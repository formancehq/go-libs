package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
)

func TestNewEventMessage(t *testing.T) {
	t.Parallel()

	payload := Payload{ID: "payload-id"}

	msg := NewEventMessage(context.Background(), "test-app", payload)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(msg.Payload, &event))
	assert.Equal(t, "test-app", event.App)
	assert.Equal(t, EventVersion, event.Version)
	assert.Equal(t, EventTypeAudit, event.Type)
	assert.NotZero(t, event.Date)

	payloadBytes, err := json.Marshal(event.Payload)
	require.NoError(t, err)
	var decodedPayload Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &decodedPayload))
	assert.Equal(t, payload.ID, decodedPayload.ID)
}

func TestPublishEventWithError(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	payload := Payload{ID: "payload-id"}

	require.NoError(t, PublishEventWithError(context.Background(), pub, "audit-events", "test-app", payload))

	require.Equal(t, "audit-events", pub.topic)
	require.Len(t, pub.messages, 1)
}

func TestPublishEventWithErrorReturnsPublisherError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("publish failed")
	pub := &recordingPublisher{err: expectedErr}

	err := PublishEventWithError(context.Background(), pub, "audit-events", "test-app", Payload{})

	require.ErrorIs(t, err, expectedErr)
}

type recordingPublisher struct {
	topic    string
	messages []*message.Message
	err      error
}

func (p *recordingPublisher) Publish(topic string, messages ...*message.Message) error {
	p.topic = topic
	p.messages = append(p.messages, messages...)
	return p.err
}

func (p *recordingPublisher) Close() error {
	return nil
}
