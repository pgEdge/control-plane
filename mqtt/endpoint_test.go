package mqtt_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/mqtt"
	"github.com/pgEdge/control-plane/mqtt/mqtttest"
)

func TestConnectAndDisconnect(t *testing.T) {
	ctx, broker := setupTestBroker(t)

	endpoint, err := mqtt.New(mqtt.Config{URL: broker.URL()})
	require.Nil(t, err)
	require.Nil(t, endpoint.Connect(ctx))
	require.Nil(t, endpoint.Disconnect(ctx))
}

func TestPublishAndReceive(t *testing.T) {
	ctx, broker := setupTestBroker(t)

	receiveChan := make(chan *mqtt.Message)

	receiver, err := mqtt.New(mqtt.Config{
		URL:           broker.URL(),
		AutoSubscribe: true,
		MessageHandlers: map[string]mqtt.MessageHandler{
			"data/#": func(ctx context.Context, msg *mqtt.Message) {
				receiveChan <- msg
			},
		},
	})
	require.Nil(t, err)
	require.Nil(t, receiver.Connect(ctx))
	defer receiver.Disconnect(ctx)

	sender, err := mqtt.New(mqtt.Config{URL: broker.URL()})
	require.Nil(t, err)
	require.Nil(t, sender.Connect(ctx))
	defer sender.Disconnect(ctx)

	err = sender.Publish(ctx, &mqtt.Message{
		Topic:   "data/test",
		Payload: []byte("HELLO"),
		QoS:     2,
	})
	require.Nil(t, err)

	select {
	case msg := <-receiveChan:
		require.Equal(t, "data/test", msg.Topic)
		require.Equal(t, "HELLO", string(msg.Payload))
		require.Equal(t, 1, msg.QoS) // subscription QoS is 1
	case <-time.After(time.Second * 10):
		t.Fatal("timed out waiting for message")
	}
}

func TestCall(t *testing.T) {
	ctx, broker := setupTestBroker(t)

	// Create a service that responds to commands
	service, err := mqtt.New(mqtt.Config{
		Username:      "service",
		URL:           broker.URL(),
		AutoSubscribe: true,
		RequestHandlers: map[string]mqtt.RequestHandler{
			"cmd/echo": func(ctx context.Context, msg *mqtt.Message) (interface{}, error) {
				return msg.Payload, nil
			},
			"cmd/ping": func(ctx context.Context, msg *mqtt.Message) (interface{}, error) {
				return "pong", nil
			},
			"cmd/error": func(ctx context.Context, msg *mqtt.Message) (interface{}, error) {
				return nil, errors.New("that's really unfortunate")
			},
		},
	})
	require.Nil(t, err)
	require.Nil(t, service.Connect(ctx))
	defer service.Disconnect(ctx)

	// Create a client that calls the service
	client, err := mqtt.New(mqtt.Config{
		Username: "client",
		URL:      broker.URL(),
	})
	require.Nil(t, err)
	require.Nil(t, client.Connect(ctx))
	defer client.Disconnect(ctx)

	// cmd/echo should yield a response with the request value
	var responseMap map[string]interface{}
	err = client.Call(ctx, &mqtt.Call{
		Topic:    "cmd/echo",
		Request:  map[string]interface{}{"poem": "jabberwocky"},
		Response: &responseMap,
	})
	require.Nil(t, err)
	require.Equal(t, map[string]interface{}{"poem": "jabberwocky"}, responseMap)

	// cmd/ping should yield a "pong" response
	var responseStr string
	err = client.Call(ctx, &mqtt.Call{
		Topic:    "cmd/ping",
		Response: &responseStr,
	})
	require.Nil(t, err)
	require.Equal(t, "pong", responseStr)

	// cmd/error should return an error
	err = client.Call(ctx, &mqtt.Call{
		Topic: "cmd/error",
	})
	require.NotNil(t, err)
	require.Equal(t, "that's really unfortunate", err.Error())
}

func TestUnsupportedOperation(t *testing.T) {
	ctx, broker := setupTestBroker(t)

	service, err := mqtt.New(mqtt.Config{
		Username: "service",
		URL:      broker.URL(),
		// This is similar to how node service and tricorder are configured.
		Subscriptions:   []string{"cmd/#"},
		RequestHandlers: map[string]mqtt.RequestHandler{},
	})
	require.Nil(t, err)
	require.Nil(t, service.Connect(ctx))
	defer service.Disconnect(ctx)

	// Create a client that calls the service
	client, err := mqtt.New(mqtt.Config{
		Username: "client",
		URL:      broker.URL(),
	})
	require.Nil(t, err)
	require.Nil(t, client.Connect(ctx))
	defer client.Disconnect(ctx)

	// cmd/unsupported should return an "unsupported operation" error
	err = client.Call(ctx, &mqtt.Call{
		Topic: "cmd/unsupported",
	})
	require.NotNil(t, err)
	require.ErrorIs(t, err, mqtt.ErrUnsupported)
}

func setupTestBroker(t testing.TB) (context.Context, *mqtttest.EMQX) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	t.Cleanup(func() {
		cancel()
	})

	broker, err := mqtttest.NewEMQX(ctx)
	if err != nil {
		t.Fatalf("failed to initialize test broker: %v", err)
	}
	t.Cleanup(func() {
		broker.Terminate(ctx)
	})

	return ctx, broker
}
