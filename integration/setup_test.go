package mq_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedServer  string
	sharedCleanup func()

	// Track all containers to ensure cleanup
	cleanupMu         sync.Mutex
	containerCleanups []func()
)

func TestMain(m *testing.M) {
	// Setup signal handling for graceful shutdown (Ctrl-C, HUP)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nReceived interrupt signal, cleaning up containers...")
		cleanupMu.Lock()
		for _, cleanup := range containerCleanups {
			cleanup()
		}
		cleanupMu.Unlock()
		os.Exit(1)
	}()

	var err error
	// Start the shared container with default configuration
	sharedServer, sharedCleanup, err = startContainer("")
	if err != nil {
		fmt.Printf("Failed to start shared container: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Clean up all containers (shared + isolated)
	cleanupMu.Lock()
	for _, cleanup := range containerCleanups {
		cleanup()
	}
	cleanupMu.Unlock()

	os.Exit(code)
}

// getFreePort returns a free TCP port by opening a listener on :0 and closing it.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startContainer starts a server container (Mosquitto or NanoMQ).
// If configContent is empty, default config is used.
// If fixedPort is set, it tries to use that host port (for restarts).
func startContainer(configContent string, fixedPort ...string) (string, func(), error) {
	ctx := context.Background()

	// Determine server image from env var or default to Mosquitto
	serverImage := os.Getenv("MQTT_SERVER_IMAGE")
	if serverImage == "" {
		serverImage = "eclipse-mosquitto:2"
	}
	isNano := strings.Contains(serverImage, "nanomq")
	isMochi := strings.Contains(serverImage, "mochi")

	var port string
	if len(fixedPort) > 0 && fixedPort[0] != "" {
		// Use the requested port (must be available)
		port = fixedPort[0]
	} else {
		// Manually find a free port to use with host networking.
		// This bypasses Podman bridge/nftables issues while still providing dynamic ports.
		portInt, err := getFreePort()
		if err != nil {
			return "", nil, fmt.Errorf("failed to find free port: %w", err)
		}
		port = fmt.Sprintf("%d", portInt)
	}

	req := testcontainers.ContainerRequest{
		Image: serverImage,
		// Using Host network mode bypasses the need for Podman to create a bridge
		// and manipulate nftables, which fails on some rootless setups.
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.NetworkMode = "host"
		},
		WaitingFor: wait.ForListeningPort(nat.Port(port + "/tcp")),
	}

	var tmpFileName string

	if isNano {
		// NanoMQ configuration
		// Use command flags instead of config file
		req.Cmd = []string{"start", "--url", "nmq-tcp://0.0.0.0:" + port}
	} else if isMochi {
		cmd := []string{"-tcp", "0.0.0.0:" + port}
		// Parse mosquitto config to extract limits for our patched mochi server
		lines := strings.Split(configContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "max_packet_size") {
				var size int
				fmt.Sscanf(line, "max_packet_size %d", &size)
				if size > 0 {
					cmd = append(cmd, "-max-packet-size", fmt.Sprintf("%d", size))
				}
			} else if strings.HasPrefix(line, "max_inflight_messages") {
				var inflight int
				fmt.Sscanf(line, "max_inflight_messages %d", &inflight)
				if inflight > 0 {
					cmd = append(cmd, "-max-receive", fmt.Sprintf("%d", inflight))
				}
			}
		}
		req.Cmd = cmd
	} else {
		// Mosquitto configuration
		// Always generate a config to ensure we listen on the correct port
		baseConfig := fmt.Sprintf("listener %s\nallow_anonymous true\n", port)
		finalConfig := baseConfig + configContent

		tmpfile, err := os.CreateTemp("", "mosquitto-*.conf")
		if err != nil {
			return "", nil, fmt.Errorf("failed to create temp config file: %w", err)
		}
		if _, err := tmpfile.Write([]byte(finalConfig)); err != nil {
			tmpfile.Close()
			return "", nil, fmt.Errorf("failed to write to temp config file: %w", err)
		}
		if err := tmpfile.Close(); err != nil {
			return "", nil, fmt.Errorf("failed to close temp config file: %w", err)
		}
		tmpFileName = tmpfile.Name()

		req.Files = append(req.Files, testcontainers.ContainerFile{
			HostFilePath:      tmpFileName,
			ContainerFilePath: "/mosquitto/config/mosquitto.conf",
			FileMode:          0644,
		})
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	// Clean up temp file immediately after container starts
	if tmpFileName != "" {
		defer os.Remove(tmpFileName)
	}

	if err != nil {
		return "", nil, fmt.Errorf("failed to start server container: %w", err)
	}

	server := fmt.Sprintf("tcp://localhost:%s", port)

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			if err := container.Terminate(ctx); err != nil {
				fmt.Printf("Failed to terminate container: %v\n", err)
			}
		})
	}

	// Register cleanup globally
	cleanupMu.Lock()
	containerCleanups = append(containerCleanups, cleanup)
	cleanupMu.Unlock()

	return server, cleanup, nil
}

// startMosquitto is the helper for tests.
// configContent: custom mosquitto config (or empty for default)
// opts: optional fixed port to reuse (e.g. "30001")
func startMosquitto(t *testing.T, configContent string, opts ...string) (string, func()) {
	t.Helper()

	// If default config and shared server is available AND no fixed port requested, use it.
	if configContent == "" && len(opts) == 0 && sharedServer != "" {
		return sharedServer, func() {
			// No-op cleanup for shared container
		}
	}

	// Otherwise start a new one (e.g. custom config, fixed port, or if shared failed)
	server, cleanup, err := startContainer(configContent, opts...)
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	return server, cleanup
}
