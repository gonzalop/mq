package main

import (
	"context"
	"fmt"
	"log"

	"github.com/gonzalop/mq"
)

func main() {
	server := "tcp://localhost:1883"
	clientID := "filestore-example-client"
	topic := "sensors/data"

	// 1. Create a FileStore.
	// This will create a directory named after the clientID inside "./data"
	// and use it to store pending messages and subscription info.
	baseStore, err := mq.NewFileStore("./data", clientID)
	if err != nil {
		log.Fatalf("Failed to create FileStore: %v", err)
	}

	// 2. Wrap it in an AsyncStore.
	// This ensures that slow disk writes never block the library's high-speed logic loop.
	// The second parameter is the buffer size for pending disk operations.
	store := mq.NewAsyncStore(baseStore, 1000)
	defer store.Close()

	fmt.Println("Connecting with FileStore persistence...")

	// 3. Configure the client to use the store.
	client, err := mq.Dial(server,
		mq.WithClientID(clientID),
		mq.WithCleanSession(false),               // Required for the server to keep state
		mq.WithSessionExpiryInterval(0xFFFFFFFF), // Required for v5 persistence (keep forever)
		mq.WithSessionStore(store),               // Use our local disk store

		// Pro-Tip: Use WithSubscription to ensure the handler is re-attached
		// if the session is restored from the local store on startup.
		mq.WithSubscription(topic, func(c *mq.Client, msg mq.Message) {
			fmt.Printf("Received message: %s\n", string(msg.Payload))
		}),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect(context.Background())

	fmt.Printf("Connected! Client is now monitoring %s\n", topic)
	fmt.Println("Try stopping this program, publishing a message while offline, and restarting it.")

	// Keep the example running to receive messages
	select {}
}
