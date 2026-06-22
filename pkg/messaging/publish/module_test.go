package publish_test

import (
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
)

func TestMemoryPublisherAllMessagesReturnsSnapshot(t *testing.T) {
	t.Parallel()

	publisher := publish.InMemory()
	msg := message.NewMessage("id", nil)
	require.NoError(t, publisher.Publish("topic", msg))

	snapshot := publisher.AllMessages()
	snapshot["topic"][0] = message.NewMessage("other", nil)
	snapshot["other"] = []*message.Message{message.NewMessage("other", nil)}

	fresh := publisher.AllMessages()
	require.Same(t, msg, fresh["topic"][0])
	require.NotContains(t, fresh, "other")
}
