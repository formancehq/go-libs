package publish

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestNewMessageWithErrorReturnsMarshalError(t *testing.T) {
	t.Parallel()

	ctx := logging.ContextWithLogger(context.Background(), logging.NopZap())
	msg, err := NewMessageWithError(ctx, EventMessage{
		Type:    "test",
		Payload: func() {},
	})

	require.Nil(t, msg)
	require.ErrorContains(t, err, "marshal event message")
}

func TestNewMessageFallsBackWithoutPanicOnMarshalError(t *testing.T) {
	t.Parallel()

	ctx := logging.ContextWithLogger(context.Background(), logging.NopZap())
	var msgPayload *EventMessage
	var err error
	var msgID string

	require.NotPanics(t, func() {
		msg := NewMessage(ctx, EventMessage{
			Type:    "test",
			Payload: func() {},
		})
		require.NotNil(t, msg)
		msgID = msg.UUID
		_, msgPayload, err = UnmarshalMessage(msg)
	})

	require.NotEmpty(t, msgID)
	require.NoError(t, err)
	require.Equal(t, "test", msgPayload.Type)
	require.Nil(t, msgPayload.Payload)
}
