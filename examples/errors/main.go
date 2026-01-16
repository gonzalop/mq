//go:build ignore_test

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/gonzalop/mq"
)

func main() {
	server := "tcp://localhost:1883"
	if len(os.Args) > 1 {
		server = os.Args[1]
	}

	// Get credentials from environment or command line
	username := os.Getenv("MQTT_USERNAME")
	password := os.Getenv("MQTT_PASSWORD")

	if len(os.Args) > 2 {
		username = os.Args[2]
	}
	if len(os.Args) > 3 {
		password = os.Args[3]
	}

	fmt.Println("--- Example 1: Connection Refused (Bad Credentials) ---")

	// Attempt to connect with invalid credentials
	// Note: Mosquitto needs to be configured to require passwords for this to fail as expected,
	// or we can simulate it if the server rejects anonymous users.
	// For this example, we assume the server might reject us.
	// We intentionally mangle the credentials if provided, or use known bad ones.

	badUser := "wrong-user"
	badPass := "wrong-pass"
	if username != "" {
		badUser = username + "-invalid"
		badPass = password + "-invalid"
	}

	client, err := mq.Dial(server,
		mq.WithClientID("error-test"),
		mq.WithCredentials(badUser, badPass),
		mq.WithConnectTimeout(2000), // Short timeout
	)

	if err != nil {
		// Demonstrate error checking with errors.Is
		if errors.Is(err, mq.ErrBadUsernameOrPassword) {
			fmt.Println("✓ Caught expected error: Bad Username/Password")
		} else if errors.Is(err, mq.ErrNotAuthorized) {
			fmt.Println("✓ Caught expected error: Not Authorized")
		} else {
			fmt.Printf("Received other error: %v\n", err)
		}
	} else {
		fmt.Println("Connected unexpectedly (server allows anonymous/any creds?)")
		client.Disconnect(context.Background())
	}

	fmt.Println("\n--- Example 2: Subscription Failure ---")

	// Connect properly first
	opts := []mq.Option{mq.WithClientID("error-test-2")}
	if username != "" {
		opts = append(opts, mq.WithCredentials(username, password))
	}

	client, err = mq.Dial(server, opts...)
	if err != nil {
		log.Printf("Skipping subscription test (conn failed): %v\n", err)
		return
	}
	defer client.Disconnect(context.Background())

	// Try to subscribe to a topic we might not have permission for (or invalid)
	// Note: Standard mosquitto allows everything by default.
	// We'll simulate a check by printing the type of error you WOULD get.

	fmt.Println("Attempting to subscribe to restricted topic...")
	token := client.Subscribe("sys/admin/restricted", mq.AtLeastOnce, nil)

	if err := token.Wait(context.Background()); err != nil {
		if errors.Is(err, mq.ErrSubscriptionFailed) {
			fmt.Println("✓ Caught expected error: Subscription Failed")
		} else {
			fmt.Printf("Subscription failed with: %v\n", err)
		}
	} else {
		fmt.Println("Subscription succeeded (server didn't reject it)")
		fmt.Println("(To see failure, configure server ACLs to deny 'sys/admin/restricted')")
	}
}
