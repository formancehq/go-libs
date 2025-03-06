package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/formancehq/go-libs/v2/publish/circuit_breaker/storage"
)

type payload struct {
	Result int `json:"result"`
}

type testMessages struct {
	topic string
	msg   *message.Message
}

type mockPublisher struct {
	err error

	messages chan *testMessages
}

func newMockPublisher(messages chan *testMessages) *mockPublisher {
	return &mockPublisher{
		messages: messages,
	}
}

func (p *mockPublisher) WithPublishError(err error) *mockPublisher {
	p.err = err
	return p
}

func (p *mockPublisher) Publish(topic string, messages ...*message.Message) error {
	if p.err != nil {
		return p.err
	}

	for _, msg := range messages {
		p.messages <- &testMessages{
			topic: topic,
			msg:   msg,
		}
	}

	return nil
}

func (p *mockPublisher) Close() error {
	return nil
}

type MockStore struct {
	insertErr error

	mu             sync.RWMutex
	messagesToSend []*storage.CircuitBreakerModel
}

func newMockStore() *MockStore {
	return &MockStore{
		messagesToSend: make([]*storage.CircuitBreakerModel, 0),
	}
}

func (s *MockStore) WithInsertError(err error) *MockStore {
	s.insertErr = err
	return s
}

func (s *MockStore) Insert(ctx context.Context, topic string, data []byte, metadata map[string]string) error {
	if s.insertErr != nil {
		return s.insertErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.messagesToSend = append(s.messagesToSend, &storage.CircuitBreakerModel{
		CreatedAt: time.Now().UTC(),
		Topic:     topic,
		Data:      data,
		Metadata:  metadata,
	})

	return nil
}

func (s *MockStore) List(ctx context.Context) ([]*storage.CircuitBreakerModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*storage.CircuitBreakerModel, len(s.messagesToSend))
	copy(result, s.messagesToSend)
	return result, nil
}

func (s *MockStore) Delete(ctx context.Context, ids []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		for i, msg := range s.messagesToSend {
			if msg.ID == id {
				s.messagesToSend = append(s.messagesToSend[:i], s.messagesToSend[i+1:]...)
				break
			}
		}
	}

	return nil
}
