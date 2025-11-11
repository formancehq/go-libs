package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/formancehq/go-libs/v3/publish/circuit_breaker/storage"
)

type payload struct {
	Result int `json:"result"`
}

type testMessages struct {
	topic string
	msg   *message.Message
}

type mockPublisher struct {
	mu       sync.RWMutex
	err      error
	messages chan *testMessages
}

func newMockPublisher(messages chan *testMessages) *mockPublisher {
	return &mockPublisher{
		messages: messages,
	}
}

func (p *mockPublisher) WithPublishError(err error) *mockPublisher {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	return p
}

func (p *mockPublisher) Publish(topic string, messages ...*message.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.err != nil {
		return p.err
	}

	for _, msg := range messages {
		select {
		case p.messages <- &testMessages{
			topic: topic,
			msg:   msg,
		}:
		default:
			// Channel is full or closed
			return nil
		}
	}

	return nil
}

func (p *mockPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil
}

type MockStore struct {
	insertErr error
	listErr   error
	deleteErr error

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

func (s *MockStore) WithListError(err error) *MockStore {
	s.listErr = err
	return s
}

func (s *MockStore) WithDeleteError(err error) *MockStore {
	s.deleteErr = err
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
	if s.listErr != nil {
		return nil, s.listErr
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*storage.CircuitBreakerModel, len(s.messagesToSend))
	copy(result, s.messagesToSend)
	return result, nil
}

func (s *MockStore) Delete(ctx context.Context, ids []uint64) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

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
