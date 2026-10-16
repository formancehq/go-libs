package object

import (
	"context"
	"errors"
	"testing"
	"time"
)

type currentBucket struct {
	meta                 Metadata
	result               ReadResult
	statErr, readErr     error
	afterStat, afterRead func()
	request              ReadRequest
}

func (b *currentBucket) ListPage(context.Context, ListRequest) (Page, error) {
	panic("exact metadata must not list")
}
func (b *currentBucket) Stat(context.Context, string) (Metadata, error) {
	if b.afterStat != nil {
		b.afterStat()
	}
	return b.meta, b.statErr
}
func (b *currentBucket) Read(_ context.Context, r ReadRequest) (ReadResult, error) {
	b.request = r
	if b.afterRead != nil {
		b.afterRead()
	}
	return b.result, b.readErr
}

func TestCurrentReadIdentityBoundsCancellationAndRevisionlessMetadata(t *testing.T) {
	for _, name := range []string{"revisionless", "revision", "different key", "negative size", "missing time", "metadata bound", "body bound", "short body", "incomplete", "stat error", "read error", "cancel stat", "cancel read"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			b := &currentBucket{meta: Metadata{Key: "item", Size: 3, LastModified: time.Unix(1, 0)}, result: ReadResult{Data: []byte("abc"), Complete: true}}
			var want error
			switch name {
			case "revision":
				b.meta.Revision = `"v1"`
			case "different key":
				b.meta.Key = "other"
				want = ErrInvalid
			case "negative size":
				b.meta.Size = -1
				want = ErrInvalid
			case "missing time":
				b.meta.LastModified = time.Time{}
				want = ErrInvalid
			case "metadata bound":
				b.meta.Size = 4
				want = ErrTooLarge
			case "body bound":
				b.result.Data = []byte("abcd")
				want = ErrTooLarge
			case "short body":
				b.result.Data = []byte("ab")
				want = ErrRevisionChanged
			case "incomplete":
				b.result.Complete = false
				want = ErrRevisionChanged
			case "stat error":
				b.statErr = errors.ErrUnsupported
				want = b.statErr
			case "read error":
				b.readErr = ErrRevisionChanged
				want = b.readErr
			case "cancel stat":
				b.afterStat = cancel
				want = context.Canceled
			case "cancel read":
				b.afterRead = cancel
				want = context.Canceled
			}
			meta, data, err := ReadCurrent(ctx, b, "item", 3)
			if want != nil {
				if !errors.Is(err, want) || meta != (Metadata{}) || data != nil {
					t.Fatalf("returned usable data: %+v %q %v, want %v", meta, data, err, want)
				}
				return
			}
			if err != nil || meta != b.meta || string(data) != "abc" || b.request.Revision != b.meta.Revision || b.request.MaxBytes != 3 {
				t.Fatalf("%+v %q %v request=%+v", meta, data, err, b.request)
			}
		})
	}
}
