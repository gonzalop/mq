package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gonzalop/mq"
	"nhooyr.io/websocket"
)

func main() {
	// Get credentials from environment or command line
	username := os.Getenv("MQTT_USERNAME")
	password := os.Getenv("MQTT_PASSWORD")

	if len(os.Args) > 2 {
		username = os.Args[2]
	}
	if len(os.Args) > 3 {
		password = os.Args[3]
	}

	// 1. Create a custom dialer for WebSockets
	wsDialer := mq.DialFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Note: 'addr' here is the full URL string like "ws://server.hivemq.com:8000/mqtt"
		// If you passed "wss://..." it would be encrypted.

		// Connect using nhooyr.io/websocket
		c, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{
			Subprotocols: []string{"mqtt"}, // Crucial: MQTT over Websocket subprotocol
		})
		if err != nil {
			return nil, err
		}

		// Wrap the websocket connection to look like a net.Conn
		return websocket.NetConn(ctx, c, websocket.MessageBinary), nil
	})

	// 2. Connect using the custom dialer
	// Local Mosquitto with WebSockets enabled on port 9001
	server := "ws://localhost:9001/"
	if len(os.Args) > 1 {
		server = os.Args[1]
	}
	clientID := fmt.Sprintf("go-mq-ws-example-%d", time.Now().UnixNano())

	fmt.Printf("Connecting to %s via WebSockets...\n", server)

	opts := []mq.Option{
		mq.WithClientID(clientID),
		mq.WithDialer(wsDialer), // Inject our custom dialer
		mq.WithCleanSession(true),
	}

	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	client, err := mq.Dial(server, opts...)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Println("Connected!")

	// 3. Subscribe
	topic := "mq-test/websocket"
	subReady := make(chan struct{})

	token := client.Subscribe(topic, 1, func(c *mq.Client, msg mq.Message) {
		fmt.Printf("Received: %s on %s\n", string(msg.Payload), msg.Topic)
		subReady <- struct{}{}
	})
	subCtx, subCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer subCancel()
	if err := token.Wait(subCtx); err != nil {
		log.Fatalf("Subscribe failed: %v", err)
	}
	fmt.Printf("Subscribed to %s\n", topic)

	// 4. Publish
	fmt.Println("Publishing message...")
	pubToken := client.Publish(topic, []byte("Hello from WebSockets!"), mq.WithQoS(1))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pubToken.Wait(ctx); err != nil {
		log.Fatalf("Publish failed: %v", err)
	}

	// Wait for message
	select {
	case <-subReady:
		fmt.Println("Message received successfully!")
	case <-time.After(5 * time.Second):
		fmt.Println("Timeout waiting for message")
	}

	// Keep running until interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

	fmt.Println("Press Ctrl+C to exit")
	<-sigChan
}
