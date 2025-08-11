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
	Listen(ctx context.Context, ch <-chan *message.Message, fn CallbackFn) error
	Done() <-chan struct{}
}

type listener struct {
	wg *sync.WaitGroup

	logger      logging.Logger
	workerCount int
	callbackFn  CallbackFn
}

func NewListener(
	logger logging.Logger,
	workerCount int,
) Listener {
	return &listener{
		wg:          &sync.WaitGroup{},
		logger:      logger,
		workerCount: workerCount,
	}
}

// Start polling SQS for messages
func (l *listener) Listen(ctx context.Context, ch <-chan *message.Message, fn CallbackFn) error {
	l.logger.WithField("workerCount", l.workerCount).Debugf("queue listener starting listen...")
	l.callbackFn = fn
	if l.workerCount < 1 {
		return fmt.Errorf("WorkerCount must be bigger than 0")
	}

	// fan out and process messages concurrently
	for i := 0; i < l.workerCount; i++ {
		l.wg.Add(1)
		go func() {
			l.startWorker(ctx, ch)
		}()
	}
	return nil
}

// Done signals that the workers are done processing all in-flight messages
func (l *listener) Done() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
		l.logger.Infof("queue listener closed")
	}()
	return done
}

func (l *listener) startWorker(ctx context.Context, messages <-chan *message.Message) {
	defer l.wg.Done()
	for {
		select {
		case <-ctx.Done():
			l.logger.Infof("context canceled, queue listener closing...")
			return
		case message := <-messages:
			// workers are protected from context cancel to ensure we tell sqs to delete messages we've processed
			detachedCtx := context.WithoutCancel(ctx)
			l.handleMessage(detachedCtx, message)
		}
	}
}

func (l *listener) handleMessage(ctx context.Context, msg *message.Message) {
	l.logger.WithField("message_uuid", msg.UUID).Debugf("queue listener handling message")
	err := l.callbackFn(ctx, msg.Metadata, msg.Payload)
	if err != nil {
		l.logger.WithField("message_uuid", msg.UUID).Errorf("queue listener failed to process message: %w", err)
		return
	}
	// delete message from queue
	msg.Ack()
}
