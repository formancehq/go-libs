package publish

import (
	"context"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
)

type TestMessage struct {
	Value string
}

func TestNewMessage(t *testing.T) {
	event := EventMessage{
		IdempotencyKey: "test-key",
		Date:           time.Now(),
		App:            "test-app",
		Version:        "1.0.0",
		Type:           "test-type",
		Payload:        map[string]interface{}{"key": "value"},
	}
	
	msg := NewMessage(context.Background(), event)

	require.NotNil(t, msg, "Le message ne devrait pas être nil")
	require.NotEmpty(t, msg.UUID, "L'UUID du message ne devrait pas être vide")
	require.NotEmpty(t, msg.Payload, "Le contenu du message ne devrait pas être vide")
	require.NotEmpty(t, msg.Metadata, "Les métadonnées du message ne devraient pas être vides")
}

func TestNoOpPublisher(t *testing.T) {
	publisher := NoOpPublisher
	
	err := publisher.Publish("test-topic", message.NewMessage("1", []byte("test")))
	require.NoError(t, err, "La publication ne devrait pas échouer")
	
	err = publisher.Close()
	require.NoError(t, err, "La fermeture ne devrait pas échouer")
}

func TestInMemoryPublisher(t *testing.T) {
	publisher := InMemory()
	require.NotNil(t, publisher, "Le publisher ne devrait pas être nil")
	require.Empty(t, publisher.AllMessages(), "Le publisher devrait être vide initialement")
	
	msg1 := message.NewMessage("1", []byte("test1"))
	msg2 := message.NewMessage("2", []byte("test2"))
	
	err := publisher.Publish("topic1", msg1)
	require.NoError(t, err, "La publication ne devrait pas échouer")
	
	err = publisher.Publish("topic2", msg2)
	require.NoError(t, err, "La publication ne devrait pas échouer")
	
	messages := publisher.AllMessages()
	require.Len(t, messages["topic1"], 1, "Il devrait y avoir un message dans topic1")
	require.Len(t, messages["topic2"], 1, "Il devrait y avoir un message dans topic2")
	require.Equal(t, msg1, messages["topic1"][0], "Le message dans topic1 devrait être correct")
	require.Equal(t, msg2, messages["topic2"][0], "Le message dans topic2 devrait être correct")
	
	err = publisher.Close()
	require.NoError(t, err, "La fermeture ne devrait pas échouer")
	require.Empty(t, publisher.AllMessages(), "Le publisher devrait être vide après fermeture")
}
