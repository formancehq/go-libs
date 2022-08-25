package sharedpublish

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/pborman/uuid"
	"go.uber.org/fx"
)

type Publisher interface {
	Publish(ctx context.Context, topic string, ev any) error
}

type TopicMapperPublisher struct {
	publisher message.Publisher
	topics    map[string]string
}

var _ Publisher = &TopicMapperPublisher{}

func NewTopicMapperPublisher(publisher message.Publisher, topics map[string]string) *TopicMapperPublisher {
	return &TopicMapperPublisher{
		publisher: publisher,
		topics:    topics,
	}
}

func (l *TopicMapperPublisher) Publish(ctx context.Context, topic string, ev any) error {
	if mappedTopic, ok := l.topics[topic]; ok {
		if err := l.publisher.Publish(mappedTopic, newMessage(ctx, ev)); err != nil {
			return err
		}
		return nil
	} else if mappedTopic, ok = l.topics["*"]; ok {
		if err := l.publisher.Publish(mappedTopic, newMessage(ctx, ev)); err != nil {
			return err
		}
		return nil
	}
	if err := l.publisher.Publish(topic, newMessage(ctx, ev)); err != nil {
		return err
	}
	return nil
}

// TODO: Inject OpenTracing context
func newMessage(ctx context.Context, m any) *message.Message {
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	msg := message.NewMessage(uuid.New(), data)
	msg.SetContext(ctx)
	return msg
}

func TopicMapperPublisherModule(topics map[string]string) fx.Option {
	return fx.Provide(func(p message.Publisher) *TopicMapperPublisher {
		return NewTopicMapperPublisher(p, topics)
	})
}
