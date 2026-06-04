package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type (
	CallbackFn func(ctx context.Context, metadata map[string]string, msg []byte) error
)

const defaultWorkerCount = 1

var ErrMessageCallbackTimeout = errors.New("message callback function took longer than configured listener deadline")

//go:generate mockgen -source listener.go -destination listener_generated.go -package queue . Listener
type Listener interface {
	Listen(ctx context.Context, ch <-chan *message.Message)
	Done() <-chan struct{}
}

type listener struct {
	wg         *sync.WaitGroup
	mux        *sync.Mutex
	hasStarted bool

	logger logging.Logger

	name             string
	workerCount      int
	callbackDeadline time.Duration // timeout for a single message, 0 == no deadline
	callbackFn       CallbackFn

	// channel that blocks until all workers are stopped
	done chan struct{}
}

func NewListener(
	logger logging.Logger,
	callbackFn CallbackFn,
	optFns ...func(*ListenerOptions),
) (Listener, error) {
	opts := ListenerOptions{
		WorkerCount: defaultWorkerCount,
	}
	for _, fn := range optFns {
		if fn == nil {
			continue
		}
		fn(&opts)
	}
	if opts.WorkerCount < 0 {
		return nil, fmt.Errorf("workerCount must be bigger than 0")
	}
	if callbackFn == nil {
		return nil, fmt.Errorf("callback function cannot be nil")
	}

	return &listener{
		wg:               &sync.WaitGroup{},
		mux:              &sync.Mutex{},
		logger:           logger,
		callbackFn:       callbackFn,
		name:             opts.Name,
		workerCount:      opts.WorkerCount,
		callbackDeadline: opts.CallbackDeadline,
		done:             make(chan struct{}),
	}, nil
}

// Start polling for messages
func (l *listener) Listen(ctx context.Context, ch <-chan *message.Message) {
	l.mux.Lock()
	l.hasStarted = true
	l.logger.WithField("listenerName", l.name).WithField("workerCount", l.workerCount).Debugf("queue listener starting listen...")
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
		l.logger.WithField("listenerName", l.name).Infof("queue listener closed")
		close(l.done)
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
			l.logger.WithField("listenerName", l.name).Infof("context canceled, queue listener closing...")
			return
		case msg, ok := <-messages:
			if !ok {
				l.logger.WithField("listenerName", l.name).Infof("channel closed by subscriber")
				return
			}
			if msg == nil { // channel closed by subscriber
				l.logger.WithField("listenerName", l.name).Errorf("received nil message from subscriber")
				continue
			}
			// workers are protected from context cancel to ensure we tell sqs to delete messages we've processed
			detachedCtx := context.WithoutCancel(ctx)
			l.handleMessage(detachedCtx, msg)
		}
	}
}

func (l *listener) handleMessage(ctx context.Context, msg *message.Message) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(msg.Metadata))
	logger := l.logger.WithContext(ctx)
	ctx = logging.ContextWithLogger(ctx, logger)

	if l.callbackDeadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, l.callbackDeadline, ErrMessageCallbackTimeout)
		defer cancel()
	}

	logger.WithField("messageUuid", msg.UUID).
		WithField("listenerName", l.name).
		WithField("callbackDeadline", l.callbackDeadline.String()).
		Debugf("queue listener handling message")
	err := l.callbackFn(ctx, msg.Metadata, msg.Payload)
	if err != nil {
		logger.WithField("messageUuid", msg.UUID).
			WithField("listenerName", l.name).
			WithField("err", err.Error()).
			Errorf("queue listener failed to process message")
		msg.Nack()
		return
	}
	// delete message from queue
	msg.Ack()
}
