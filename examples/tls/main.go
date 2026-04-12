//go:build ignore_test

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/gonzalop/mq"
)

func main() {
	var (
		server   string
		username string
		password string
		caFile   string
		certFile string
		keyFile  string
		insecure bool
		topic    string
	)

	flag.StringVar(&server, "server", "tls://localhost:8883", "MQTT server address (tls://...)")
	flag.StringVar(&username, "username", os.Getenv("MQTT_USERNAME"), "Username for authentication")
	flag.StringVar(&password, "password", os.Getenv("MQTT_PASSWORD"), "Password for authentication")
	flag.StringVar(&caFile, "ca-file", "", "Path to CA certificate file (PEM)")
	flag.StringVar(&certFile, "cert-file", "", "Path to client certificate file (PEM) for mTLS")
	flag.StringVar(&keyFile, "key-file", "", "Path to client private key file (PEM) for mTLS")
	flag.BoolVar(&insecure, "insecure", false, "Skip server certificate verification (WARNING: UNSAFE)")
	flag.StringVar(&topic, "topic", "test/tls", "Topic to subscribe and publish to")
	flag.Parse()

	// Positional arguments override flags for compatibility with old example usage
	args := flag.Args()
	if len(args) > 0 {
		server = args[0]
	}
	if len(args) > 1 {
		username = args[1]
	}
	if len(args) > 2 {
		password = args[2]
	}

	fmt.Printf("Connecting to MQTT server at %s (TLS)...\n", server)
	if username != "" {
		fmt.Printf("Using credentials: username=%s\n", username)
	}

	// Setup TLS configuration
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if insecure {
		fmt.Println("⚠️  WARNING: Skipping server certificate verification (InsecureSkipVerify=true)")
		fmt.Println("   This is UNSAFE and should only be used for local testing.")
		tlsConfig.InsecureSkipVerify = true
	}

	if caFile != "" {
		fmt.Printf("Loading CA certificate from %s...\n", caFile)
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			log.Fatalf("Error reading CA file: %v", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			log.Fatalf("Failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	if certFile != "" && keyFile != "" {
		fmt.Printf("Loading client certificate pair (%s, %s)...\n", certFile, keyFile)
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Fatalf("Error loading client key pair: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Change the log level here if you wish
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	opts := []mq.Option{
		mq.WithClientID("tlsExampleClient"),
		mq.WithKeepAlive(60 * time.Second),
		mq.WithLogger(logger),
		mq.WithTLS(tlsConfig),
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	// Connect to server
	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("✓ Connected successfully via TLS!")

	// Subscribe to test topic
	fmt.Printf("\nSubscribing to '%s'...\n", topic)
	received := make(chan mq.Message, 10)

	token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("📨 Received: topic=%s qos=%d payload=%s\n",
			msg.Topic, msg.QoS, string(msg.Payload))
		received <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := token.Wait(ctx); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	fmt.Println("✓ Subscribed successfully!")

	// Publish test message
	fmt.Println("\nPublishing secure message...")
	pubToken := client.Publish(topic, []byte("Secure Hello via TLS!"), mq.WithQoS(1))
	if err := pubToken.Wait(context.Background()); err != nil {
		log.Printf("Publish failed: %v", err)
	} else {
		fmt.Println("✓ Message published and acknowledged")
	}

	// Wait for message
	fmt.Println("\nWaiting for message...")
	select {
	case msg := <-received:
		fmt.Printf("✓ Received: %s\n", string(msg.Payload))
	case <-time.After(5 * time.Second):
		fmt.Println("⚠ Timeout waiting for message")
	}

	fmt.Println("\nTLS test completed! 🔒")
}
