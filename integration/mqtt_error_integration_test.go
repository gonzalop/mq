package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/gonzalop/mq"
)

func TestMqttErrorIntegration_Subscribe_Restricted(t *testing.T) {
	t.Parallel()
	server, cleanup := startMosquitto(t, "")
	defer cleanup()

	t.Run("MQTT v5.0 Subscribe Restricted Topic", func(t *testing.T) {
		client, err := mq.Dial(server,
			mq.WithProtocolVersion(mq.ProtocolV50),
			mq.WithClientID("v5-restricted-sub"),
		)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer client.Disconnect(context.Background())

		// This topic should be prohibited by the server
		token := client.Subscribe("$SYS/broker/connection/+", mq.AtLeastOnce, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = token.Wait(ctx)
		if err != nil {
			if mErr, ok := err.(*mq.MqttError); ok {
				t.Logf("Received MqttError as expected: %v", mErr)
			} else {
				t.Logf("Received error, but not MqttError: %T %v", err, err)
			}
		} else {
			t.Log("Subscription succeeded (server is permissive)")
		}
	})
}
