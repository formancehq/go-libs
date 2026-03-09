package publish

import (
	"sync"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

func NewGoChannel() *gochannel.GoChannel {
	return gochannel.NewGoChannel(
		gochannel.Config{
			BlockPublishUntilSubscriberAck: true,
		},
		watermill.NopLogger{},
	)
}

type noOpPublisher struct {
}

func (n noOpPublisher) Publish(topic string, messages ...*message.Message) error {
	return nil
}

func (n noOpPublisher) Close() error {
	return nil
}

var NoOpPublisher message.Publisher = &noOpPublisher{}

type memoryPublisher struct {
	sync.Mutex
	messages map[string][]*message.Message
}

func (m *memoryPublisher) Publish(topic string, messages ...*message.Message) error {
	m.Lock()
	defer m.Unlock()

	m.messages[topic] = append(m.messages[topic], messages...)
	return nil
}

func (m *memoryPublisher) Close() error {
	m.Lock()
	defer m.Unlock()

	m.messages = map[string][]*message.Message{}
	return nil
}

func (m *memoryPublisher) AllMessages() map[string][]*message.Message {
	return m.messages
}

var _ message.Publisher = (*memoryPublisher)(nil)

func InMemory() *memoryPublisher {
	return &memoryPublisher{
		messages: map[string][]*message.Message{},
	}
}
