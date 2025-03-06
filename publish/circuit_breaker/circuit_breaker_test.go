package circuitbreaker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker(t *testing.T) {
	t.Run("nominal", func(t *testing.T) {
		messages := make(chan *testMessages, 10)
		publisher := newMockPublisher(messages)
		store := newMockStore()

		cb := newCircuitBreaker(
			logging.Testing(),
			publisher,
			store,
			5*time.Second,
		)
		go cb.loop()

		payload, err := json.Marshal("test")
		require.NoError(t, err)
		msg := message.NewMessage(uuid.New().String(), payload)
		require.NoError(t, cb.Publish("test", msg))

		select {
		case m := <-messages:
			require.Equal(t, "test", m.topic)
			var payload string
			require.NoError(t, json.Unmarshal(m.msg.Payload, &payload))
			require.Equal(t, "test", payload)
		case <-time.After(time.Second):
			t.Fatal("Expected message but didn't receive one")
		}

		require.NoError(t, cb.Close())
	})

	t.Run("error publisher", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		errTest := errors.New("test")
		underlyingPublisher := newMockPublisher(messages)
		store := newMockStore()
		publisher := newCircuitBreaker(
			logging.Testing(),
			underlyingPublisher,
			store,
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		expectedP1, _ := json.Marshal(&payload{Result: 1})
		m1 := message.NewMessage("1", expectedP1)
		m1.Metadata.Set("foo", "bar")
		err := publisher.Publish("test", m1)
		require.NoError(t, err)
		require.Equal(t, StateClose, publisher.GetState())

		underlyingPublisher.WithPublishError(errTest)

		expectedP2, _ := json.Marshal(&payload{Result: 2})
		m2 := message.NewMessage("2", expectedP2)
		m2.Metadata.Set("foo", "bar")
		err = publisher.Publish("test", m2)
		require.NoError(t, err)
		require.Equal(t, StateOpen, publisher.GetState())

		expectedP3, _ := json.Marshal(&payload{Result: 3})
		m3 := message.NewMessage("3", expectedP3)
		m3.Metadata.Set("foo2", "bar2")
		err = publisher.Publish("test", m3)
		require.NoError(t, err)
		require.Equal(t, StateOpen, publisher.GetState())

		require.Len(t, messages, 1)
		msg1 := <-messages
		require.Equal(t, "test", msg1.topic)
		require.Equal(t, "1", msg1.msg.UUID)
		require.Equal(t, message.Metadata(map[string]string{"foo": "bar"}), msg1.msg.Metadata)
		p1 := &payload{}
		_ = json.Unmarshal(msg1.msg.Payload, p1)
		require.Equal(t, 1, p1.Result)

		storedMessages, err := store.List(context.Background())
		require.NoError(t, err)
		require.Len(t, storedMessages, 2)

		require.Equal(t, "test", storedMessages[0].Topic)
		require.Equal(t, map[string]string{"foo": "bar"}, storedMessages[0].Metadata)
		p2 := &payload{}
		_ = json.Unmarshal(storedMessages[0].Data, p2)
		require.Equal(t, 2, p2.Result)

		require.Equal(t, "test", storedMessages[1].Topic)
		require.Equal(t, map[string]string{"foo2": "bar2"}, storedMessages[1].Metadata)
		p3 := &payload{}
		_ = json.Unmarshal(storedMessages[1].Data, p3)
		require.Equal(t, 3, p3.Result)
	})

	t.Run("error publisher and store", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		errTest := errors.New("test")
		underlyingPublisher := newMockPublisher(messages)
		store := newMockStore().WithInsertError(errTest)
		publisher := newCircuitBreaker(
			logging.Testing(),
			underlyingPublisher,
			store,
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		expectedP1, _ := json.Marshal(&payload{Result: 1})
		m1 := message.NewMessage("1", expectedP1)
		m1.Metadata.Set("foo", "bar")
		err := publisher.Publish("test", m1)
		require.NoError(t, err)
		require.Equal(t, StateClose, publisher.GetState())

		underlyingPublisher.WithPublishError(errTest)

		expectedP2, _ := json.Marshal(&payload{Result: 2})
		m2 := message.NewMessage("2", expectedP2)
		m2.Metadata.Set("foo", "bar")
		err = publisher.Publish("test", m2)
		require.ErrorIs(t, err, errTest)
		require.Equal(t, StateOpen, publisher.GetState())

		expectedP3, _ := json.Marshal(&payload{Result: 3})
		m3 := message.NewMessage("3", expectedP3)
		m3.Metadata.Set("foo2", "bar2")
		err = publisher.Publish("test", m3)
		require.ErrorIs(t, err, errTest)
		require.Equal(t, StateOpen, publisher.GetState())

		require.Len(t, messages, 1)
		msg1 := <-messages
		require.Equal(t, "test", msg1.topic)
		require.Equal(t, "1", msg1.msg.UUID)
		require.Equal(t, message.Metadata(map[string]string{"foo": "bar"}), msg1.msg.Metadata)
		p1 := &payload{}
		_ = json.Unmarshal(msg1.msg.Payload, p1)
		require.Equal(t, 1, p1.Result)

		storedMessages, err := store.List(context.Background())
		require.NoError(t, err)
		require.Len(t, storedMessages, 0)
	})

	t.Run("error publisher but recover after x seconds", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		errTest := errors.New("test")
		underlyingPublisher := newMockPublisher(messages)
		store := newMockStore()
		publisher := newCircuitBreaker(
			logging.Testing(),
			underlyingPublisher,
			store,
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		expectedP1, _ := json.Marshal(&payload{Result: 1})
		m1 := message.NewMessage("1", expectedP1)
		m1.Metadata.Set("foo", "bar")
		err := publisher.Publish("test", m1)
		require.NoError(t, err)
		require.Equal(t, StateClose, publisher.GetState())

		underlyingPublisher.WithPublishError(errTest)

		expectedP2, _ := json.Marshal(&payload{Result: 2})
		m2 := message.NewMessage("2", expectedP2)
		m2.Metadata.Set("foo", "bar")
		err = publisher.Publish("test", m2)
		require.NoError(t, err)
		require.Equal(t, StateOpen, publisher.GetState())

		expectedP3, _ := json.Marshal(&payload{Result: 3})
		m3 := message.NewMessage("3", expectedP3)
		m3.Metadata.Set("foo2", "bar2")
		err = publisher.Publish("test", m3)
		require.NoError(t, err)
		require.Equal(t, StateOpen, publisher.GetState())

		require.Len(t, messages, 1)
		msg1 := <-messages
		require.Equal(t, "test", msg1.topic)
		require.Equal(t, "1", msg1.msg.UUID)
		require.Equal(t, message.Metadata(map[string]string{"foo": "bar"}), msg1.msg.Metadata)
		p1 := &payload{}
		_ = json.Unmarshal(msg1.msg.Payload, p1)
		require.Equal(t, 1, p1.Result)

		storedMessages, err := store.List(context.Background())
		require.NoError(t, err)
		require.Len(t, storedMessages, 2)

		require.Equal(t, "test", storedMessages[0].Topic)
		require.Equal(t, map[string]string{"foo": "bar"}, storedMessages[0].Metadata)
		p2 := &payload{}
		_ = json.Unmarshal(storedMessages[0].Data, p2)
		require.Equal(t, 2, p2.Result)

		require.Equal(t, "test", storedMessages[1].Topic)
		require.Equal(t, map[string]string{"foo2": "bar2"}, storedMessages[1].Metadata)
		p3 := &payload{}
		_ = json.Unmarshal(storedMessages[1].Data, p3)
		require.Equal(t, 3, p3.Result)

		underlyingPublisher.WithPublishError(nil)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			if !assert.Equal(c, StateClose, publisher.GetState()) {
				return
			}

			// Now we must fail if state is closed and there is nothing or wrong
			// data in the messages channel

			require.Len(t, messages, 2)

			msg2 := <-messages
			require.Equal(t, "test", msg2.topic)
			require.Equal(t, message.Metadata(map[string]string{"foo": "bar"}), msg2.msg.Metadata)
			p2 := &payload{}
			_ = json.Unmarshal(msg2.msg.Payload, p2)
			require.Equal(t, 2, p2.Result)

			msg3 := <-messages
			require.Equal(t, "test", msg3.topic)
			require.Equal(t, message.Metadata(map[string]string{"foo2": "bar2"}), msg3.msg.Metadata)
			p3 := &payload{}
			_ = json.Unmarshal(msg3.msg.Payload, p3)
			require.Equal(t, 3, p3.Result)

			storedMessages, err := store.List(context.Background())
			require.NoError(t, err)
			require.Len(t, storedMessages, 0)
		}, 10*time.Second, 1*time.Second)
	})

	t.Run("context cancelled", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages),
			newMockStore(),
			5*time.Second,
		)

		// Cancel the context before starting the loop
		publisher.cancel()

		expectedP1, _ := json.Marshal(&payload{Result: 1})
		m1 := message.NewMessage("1", expectedP1)
		err := publisher.Publish("test", m1)
		require.Error(t, err)
		require.Equal(t, "circuit breaker closed", err.Error())

		// Try to close an already cancelled publisher
		err = publisher.Close()
		require.NoError(t, err)
	})

	t.Run("otel context propagation", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages),
			newMockStore(),
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		// Test with valid otel context
		metadata := map[string]string{
			otelContextKey: `{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}`,
		}
		msg := message.NewMessage("1", []byte("test"))
		for k, v := range metadata {
			msg.Metadata.Set(k, v)
		}

		err := publisher.Publish("test", msg)
		require.NoError(t, err)

		// Test with invalid otel context
		metadata = map[string]string{
			otelContextKey: `invalid json`,
		}
		msg = message.NewMessage("2", []byte("test"))
		for k, v := range metadata {
			msg.Metadata.Set(k, v)
		}

		store := newMockStore()
		publisher = newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages).WithPublishError(errors.New("publish error")),
			store,
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		err = publisher.Publish("test", msg)
		require.NoError(t, err)

		// Verify that the message was stored with the invalid context
		storedMessages, err := store.List(context.Background())
		require.NoError(t, err)
		require.Len(t, storedMessages, 1)
		require.Equal(t, metadata, storedMessages[0].Metadata)
	})

	t.Run("multiple messages in single publish", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages),
			newMockStore(),
			5*time.Second,
		)
		defer publisher.Close()

		go publisher.loop()

		// Create multiple messages
		msg1 := message.NewMessage("1", []byte("test1"))
		msg2 := message.NewMessage("2", []byte("test2"))
		msg3 := message.NewMessage("3", []byte("test3"))

		// Publish multiple messages at once
		err := publisher.Publish("test", msg1, msg2, msg3)
		require.NoError(t, err)

		// Verify all messages were published
		require.Len(t, messages, 3)
		receivedMsg1 := <-messages
		require.Equal(t, "1", receivedMsg1.msg.UUID)
		receivedMsg2 := <-messages
		require.Equal(t, "2", receivedMsg2.msg.UUID)
		receivedMsg3 := <-messages
		require.Equal(t, "3", receivedMsg3.msg.UUID)
	})

	t.Run("catchup database with invalid message", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		store := newMockStore()
		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages).WithPublishError(errors.New("publish error")),
			store,
			1*time.Millisecond, // Use a very short interval to trigger catchup quickly
		)

		// Insert an invalid message directly into the store
		err := store.Insert(context.Background(), "test", []byte("invalid json"), map[string]string{
			otelContextKey: "invalid json",
		})
		require.NoError(t, err)

		go publisher.loop()
		defer publisher.Close()

		// Wait for the catchup to happen and verify the state changes
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, StateOpen, publisher.GetState())
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("store error during catchup", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		errTest := errors.New("store error")
		store := newMockStore().WithListError(errTest)

		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages),
			store,
			1*time.Millisecond,
		)

		go publisher.loop()
		defer publisher.Close()

		// Wait for the catchup to happen and verify the state changes
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, StateOpen, publisher.GetState())
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("store delete error during catchup", func(t *testing.T) {
		messages := make(chan *testMessages, 100)
		defer close(messages)

		errTest := errors.New("delete error")
		store := newMockStore().WithDeleteError(errTest)

		// Insert a valid message
		err := store.Insert(context.Background(), "test", []byte(`{"test":"data"}`), nil)
		require.NoError(t, err)

		publisher := newCircuitBreaker(
			logging.Testing(),
			newMockPublisher(messages),
			store,
			1*time.Millisecond,
		)

		go publisher.loop()
		defer publisher.Close()

		// Wait for the catchup to happen and verify the state changes
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, StateOpen, publisher.GetState())
		}, 2*time.Second, 100*time.Millisecond)
	})
}
