package circuitbreaker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/publish/circuit_breaker/storage"
)

type State string

const (
	// StateOpen is the state when the circuit breaker is open and does not allow
	// requests to pass through. Instead, it writes the message in the database
	// and waits for the "openInterval" to pass before switching to the
	// "half-open" state.
	StateOpen State = "open"
	// StateClosed is the default state. It allows requests to pass through.
	StateClose State = "close"
)

type CircuitBreaker struct {
	logger logging.Logger

	publisher message.Publisher
	store     storage.Store

	stateMu sync.RWMutex
	state   State

	// openInterval is the time interval for the "open" state, before switching
	// to the "half-open" state.
	openInterval      time.Duration
	openIntervalTimer *time.Timer

	sendChan    chan *internalMessage
	stopChannel chan chan struct{}
	stopped     chan struct{}
}

type internalMessage struct {
	topic string
	msg   *message.Message

	errChan chan error
}

func newCircuitBreaker(
	logger logging.Logger,
	publisher message.Publisher,
	store storage.Store,
	openIntervalDuration time.Duration,
) *CircuitBreaker {
	return &CircuitBreaker{
		stopChannel: make(chan chan struct{}),
		stopped:     make(chan struct{}),
		logger:      logger,
		publisher:   publisher,
		store:       store,

		state: StateClose,

		openInterval:      openIntervalDuration,
		openIntervalTimer: time.NewTimer(openIntervalDuration),

		// no capacity, we want to block the loop if the sendChan is not consumed
		sendChan: make(chan *internalMessage, 1),
	}
}

func (cb *CircuitBreaker) GetState() State {
	cb.stateMu.RLock()
	defer cb.stateMu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) setState(state State) {
	cb.stateMu.Lock()
	cb.state = state
	cb.stateMu.Unlock()
}

func (cb *CircuitBreaker) OpenState() {
	cb.setState(StateOpen)
	cb.openIntervalTimer.Reset(cb.openInterval)

	cb.logger.Info("Circuit breaker switched to the open state")
}

func (cb *CircuitBreaker) HalfOpenState() {
	cb.openIntervalTimer.Stop()
	cb.logger.Info("Circuit breaker switched to the half open state")
}

func (cb *CircuitBreaker) CloseState() {
	cb.setState(StateClose)
	cb.openIntervalTimer.Stop()

	cb.logger.Info("Circuit breaker switched to the close state")
}

func (cb *CircuitBreaker) loop(ctx context.Context) {
	defer close(cb.stopped)
	// Start in the half open state to fetch the messages from the database
	cb.HalfOpenState()
	if err := cb.catchUpDatabase(ctx); err != nil {
		cb.OpenState()
		// Don't switch to closed state if there was an error
	} else {
		// Only switch to closed state if catchup was successful
		cb.CloseState()
	}

	for {
		select {
		case ch := <-cb.stopChannel:
			close(ch)
			// context cancelled
			return

		case <-cb.openIntervalTimer.C:
			// openInterval passed, let's switch to the half-open state

			cb.HalfOpenState()

			if err := cb.catchUpDatabase(ctx); err != nil {
				cb.OpenState()
				continue
			}

			// we successfully published the messages, let's switch to the closed state
			cb.CloseState()

		case msg, ok := <-cb.sendChan:
			if !ok {
				// sendChan closed
				return
			}

			switch cb.state {
			case StateClose:
				// We are in the closed state, send the message to the publisher

				cb.logger.Info("Circular breaker is in the closed state, sending the message to the publisher")
				err := cb.publisher.Publish(msg.topic, msg.msg)
				if err != nil {
					// error publishing the message, let's switch to the open state
					cb.OpenState()

					cb.logger.Info("Failed to publish the message, switching to the open state")
					// write the message in the database
					err = cb.store.Insert(ctx, msg.topic, msg.msg.Payload, msg.msg.Metadata)
					if err != nil {
						select {
						case msg.errChan <- err:
						case ch := <-cb.stopChannel:
							close(ch)
							return
						}
						continue
					}
				}
			case StateOpen:
				// We are in the open state, write the message in the database
				cb.logger.Info("Circuit breaker is in the open state, writing the message in the database")

				err := cb.store.Insert(ctx, msg.topic, msg.msg.Payload, msg.msg.Metadata)
				if err != nil {
					select {
					case msg.errChan <- err:
					case ch := <-cb.stopChannel:
						close(ch)
						return
					}
					continue
				}
			}

			select {
			case msg.errChan <- nil:
			case ch := <-cb.stopChannel:
				close(ch)
				return
			}
		}
	}
}

func (cb *CircuitBreaker) catchUpDatabase(ctx context.Context) error {
	for {
		// fetch the oldest messages from the database
		messages, err := cb.store.List(ctx)
		if err != nil {
			// error fetching messages, let's switch back to the open state
			return err
		}

		if len(messages) == 0 {
			return nil
		}

		messagesToDelete := make([]uint64, 0)
		var publishError error
		for _, msg := range messages {
			// We need to publish the messages one by one in order to know
			// which one failed.

			message, err := newMessage(ctx, msg.Data, msg.Metadata)
			if err != nil {
				publishError = err
				break
			}

			err = cb.publisher.Publish(msg.Topic, message)
			if err != nil {
				publishError = err
				break
			}

			messagesToDelete = append(messagesToDelete, msg.ID)
		}

		err = cb.store.Delete(ctx, messagesToDelete)
		if err != nil {
			// error deleting messages, let's switch back to the open state
			return err
		}

		if publishError != nil {
			// we failed to publish all the messages
			return publishError
		}
	}
}

func (cb *CircuitBreaker) Publish(topic string, messages ...*message.Message) error {
	for _, msg := range messages {
		errChan := make(chan error, 1)

		internalMessage := &internalMessage{
			topic:   topic,
			msg:     msg,
			errChan: errChan,
		}

		select {
		case cb.sendChan <- internalMessage:
		case <-cb.stopped:
			return errors.New("circuit breaker closed")
		}

		select {
		case err := <-errChan:
			if err != nil {
				return err
			}
		case <-cb.stopped:
			return errors.New("circuit breaker closed")
		}
	}

	return nil
}

func (cb *CircuitBreaker) Close() error {
	ch := make(chan struct{})
	cb.stopChannel <- ch
	<-ch

	return cb.publisher.Close()
}

const (
	otelContextKey = "otel-context"
)

func newMessage(ctx context.Context, data []byte, metadata map[string]string) (*message.Message, error) {
	if metadata == nil {
		metadata = make(map[string]string)
	}

	msg := message.NewMessage(uuid.NewString(), data)

	otelContext, ok := metadata[otelContextKey]
	if ok {
		carrier := propagation.MapCarrier{}
		err := json.Unmarshal([]byte(otelContext), &carrier)
		if err != nil {
			return nil, err
		}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
	}

	msg.SetContext(ctx)
	for k, v := range metadata {
		msg.Metadata.Set(k, v)
	}

	return msg, nil
}
