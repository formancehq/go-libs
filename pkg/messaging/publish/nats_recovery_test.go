package publish

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestRetryableNATSPublishError(t *testing.T) {
	for _, err := range []error{nats.ErrTimeout, nats.ErrDisconnected, nats.ErrNoResponders, nats.ErrNoStreamResponse, nats.ErrReconnectBufExceeded, context.DeadlineExceeded, net.ErrClosed, &net.OpError{Op: "write", Err: errors.New("broken pipe")}, &nats.APIError{Code: 409, ErrorCode: 10158}} {
		if !IsRetryableNATSPublishError(fmt.Errorf("wrapped: %w", err)) {
			t.Errorf("transient: %v", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, nats.ErrConnectionClosed, errors.New("marshal failed"), &nats.APIError{Code: 409, ErrorCode: 10071}, &nats.APIError{Code: 503, ErrorCode: 10047}} {
		if IsRetryableNATSPublishError(fmt.Errorf("wrapped: %w", err)) {
			t.Errorf("permanent: %v", err)
		}
	}
}

func TestNATSBoundedDialerValidation(t *testing.T) {
	if _, err := NewNATSBoundedDialer(net.Dialer{}, time.Second, time.Second, 2*time.Second); err == nil {
		t.Fatal("invalid stall timeout accepted")
	}
	if _, err := NewNATSBoundedDialer(net.Dialer{}, 0, time.Second, time.Second); err == nil {
		t.Fatal("unbounded dial accepted")
	}
}
