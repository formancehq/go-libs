package queue

import (
	"context"
	"fmt"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/formancehq/go-libs/v3/logging"
)

type (
	CallbackFn func(ctx context.Context, metadata map[string]string, msg []byte) error
)

//go:generate mockgen -source listener.go -destination listener_generated.go -package queue . Listener
type Listener interface {
	Listen(ctx context.Context, ch <-chan *message.Message)
	Done() <-chan struct{}
}

type listener struct {
	wg         *sync.WaitGroup
	mux        *sync.Mutex
	hasStarted bool

	logger      logging.Logger
	workerCount int
	callbackFn  CallbackFn

	// channel that blocks until all workers are stopped
	done chan struct{}
}

func NewListener(
	logger logging.Logger,
	callbackFn CallbackFn,
	workerCount int,
) (Listener, error) {
	if workerCount < 1 {
		return nil, fmt.Errorf("workerCount must be bigger than 0")
	}
	if callbackFn == nil {
		return nil, fmt.Errorf("callback function cannot be nil")
	}

	return &listener{
		wg:          &sync.WaitGroup{},
		mux:         &sync.Mutex{},
		logger:      logger,
		callbackFn:  callbackFn,
		workerCount: workerCount,
		done:        make(chan struct{}),
	}, nil
}

// Start polling for messages
func (l *listener) Listen(ctx context.Context, ch <-chan *message.Message) {
	l.mux.Lock()
	l.hasStarted = true
	l.logger.WithField("workerCount", l.workerCount).Debugf("queue listener starting listen...")
	l.mux.Unlock()

	// fan out and process messages concurrently
	for i := 0; i < l.workerCount; i++ {
		l.wg.Add(1)
		go func() {
			l.startWorker(ctx, ch)
		}()
	}

	go func() {
		l.wg.Wait()
		close(l.done)
		l.logger.Infof("queue listener closed")
	}()
	return
}

// Done signals that the workers are done processing all in-flight messages
func (l *listener) Done() <-chan struct{} {
	l.mux.Lock()
	defer l.mux.Unlock()
	if !l.hasStarted {
		// listen was never called
		close(l.done)
	}
	return l.done
}

func (l *listener) startWorker(ctx context.Context, messages <-chan *message.Message) {
	defer l.wg.Done()
	for {
		select {
		case <-ctx.Done():
			l.logger.Infof("context canceled, queue listener closing...")
			return
		case msg, ok := <-messages:
			if !ok {
				l.logger.Infof("channel closed by subscriber")
				return
			}
			if msg == nil { // channel closed by subscriber
				l.logger.Errorf("received nil message from subscriber")
				continue
			}
			// workers are protected from context cancel to ensure we tell sqs to delete messages we've processed
			detachedCtx := context.WithoutCancel(ctx)
			l.handleMessage(detachedCtx, msg)
		}
	}
}

func (l *listener) handleMessage(ctx context.Context, msg *message.Message) {
	l.logger.WithField("message_uuid", msg.UUID).Debugf("queue listener handling message")
	err := l.callbackFn(ctx, msg.Metadata, msg.Payload)
	if err != nil {
		l.logger.WithField("message_uuid", msg.UUID).Errorf("queue listener failed to process message: %w", err)
		msg.Nack()
		return
	}
	// delete message from queue
	msg.Ack()
}
