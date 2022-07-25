package sharedpublish

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/numary/go-libs/sharedlogging"
	"github.com/pborman/uuid"
	"go.uber.org/fx"
)

type Publisher interface {
	Publish(ctx context.Context, topic string, ev interface{}) error
}

// TODO: Inject OpenTracing context
func newMessage(ctx context.Context, m interface{}) *message.Message {
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	msg := message.NewMessage(uuid.New(), data)
	msg.SetContext(ctx)
	return msg
}

type TopicMapperPublisher struct {
	publisher message.Publisher
	topics    map[string]string
}

func (l *TopicMapperPublisher) publish(ctx context.Context, topic string, ev interface{}) error {
	err := l.publisher.Publish(topic, newMessage(ctx, ev))
	if err != nil {
		sharedlogging.GetLogger(ctx).Errorf("Publishing message: %s", err)
		return err
	}
	return nil
}

func (l *TopicMapperPublisher) Publish(ctx context.Context, topic string, ev interface{}) error {
	mappedTopic, ok := l.topics[topic]
	if ok {
		return l.publish(ctx, mappedTopic, ev)
	}
	mappedTopic, ok = l.topics["*"]
	if ok {
		return l.publish(ctx, mappedTopic, ev)
	}

	return l.publish(ctx, topic, ev)
}

func NewTopicMapperPublisher(publisher message.Publisher, topics map[string]string) *TopicMapperPublisher {
	return &TopicMapperPublisher{
		publisher: publisher,
		topics:    topics,
	}
}

var _ Publisher = &TopicMapperPublisher{}

func TopicMapperPublisherModule(topics map[string]string) fx.Option {
	return fx.Provide(func(p message.Publisher) *TopicMapperPublisher {
		return NewTopicMapperPublisher(p, topics)
	})
}
