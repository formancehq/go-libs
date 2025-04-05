package topicmapper

import (
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
)

type mockPublisher struct {
	publishedTopic string
	publishedMsgs  []*message.Message
	err            error
}

func (m *mockPublisher) Publish(topic string, messages ...*message.Message) error {
	m.publishedTopic = topic
	m.publishedMsgs = messages
	return m.err
}

func (m *mockPublisher) Close() error {
	return nil
}

func TestNewPublisherDecorator(t *testing.T) {
	publisher := &mockPublisher{}
	topics := map[string]string{
		"topic1": "mapped1",
		"topic2": "mapped2",
	}
	
	decorator := NewPublisherDecorator(publisher, topics)
	
	require.NotNil(t, decorator, "Le décorateur ne devrait pas être nil")
	require.Equal(t, publisher, decorator.Publisher, "Le publisher devrait être correctement défini")
	require.Equal(t, topics, decorator.topics, "Les topics devraient être correctement définis")
}

func TestTopicMapperPublisherDecorator_Publish_MappedTopic(t *testing.T) {
	publisher := &mockPublisher{}
	topics := map[string]string{
		"topic1": "mapped1",
		"topic2": "mapped2",
	}
	
	decorator := NewPublisherDecorator(publisher, topics)
	
	msg := message.NewMessage("123", []byte("test"))
	err := decorator.Publish("topic1", msg)
	
	require.NoError(t, err, "Publish ne devrait pas échouer")
	require.Equal(t, "mapped1", publisher.publishedTopic, "Le topic devrait être mappé correctement")
	require.Len(t, publisher.publishedMsgs, 1, "Un message devrait être publié")
	require.Equal(t, msg, publisher.publishedMsgs[0], "Le message devrait être correctement transmis")
}

func TestTopicMapperPublisherDecorator_Publish_UnmappedTopic(t *testing.T) {
	publisher := &mockPublisher{}
	topics := map[string]string{
		"topic1": "mapped1",
		"topic2": "mapped2",
	}
	
	decorator := NewPublisherDecorator(publisher, topics)
	
	msg := message.NewMessage("123", []byte("test"))
	err := decorator.Publish("topic3", msg)
	
	require.NoError(t, err, "Publish ne devrait pas échouer")
	require.Equal(t, "topic3", publisher.publishedTopic, "Un topic non mappé devrait être utilisé tel quel")
	require.Len(t, publisher.publishedMsgs, 1, "Un message devrait être publié")
	require.Equal(t, msg, publisher.publishedMsgs[0], "Le message devrait être correctement transmis")
}

func TestTopicMapperPublisherDecorator_Publish_WildcardMapping(t *testing.T) {
	publisher := &mockPublisher{}
	topics := map[string]string{
		"topic1": "mapped1",
		"*":      "wildcard",
	}
	
	decorator := NewPublisherDecorator(publisher, topics)
	
	msg := message.NewMessage("123", []byte("test"))
	err := decorator.Publish("topic3", msg)
	
	require.NoError(t, err, "Publish ne devrait pas échouer")
	require.Equal(t, "wildcard", publisher.publishedTopic, "Le topic devrait être mappé au wildcard")
	require.Len(t, publisher.publishedMsgs, 1, "Un message devrait être publié")
	require.Equal(t, msg, publisher.publishedMsgs[0], "Le message devrait être correctement transmis")
}
