package object

import (
	"context"
	"errors"
	"testing"
)

type cancellationPageBucket struct {
	currentBucket
	calls  int
	cancel context.CancelFunc
}

func (b *cancellationPageBucket) ListPage(context.Context, ListRequest) (Page, error) {
	b.calls++
	if b.cancel != nil {
		b.cancel()
	}
	return Page{Objects: []Metadata{{Key: "item"}}}, nil
}

func (b *cancellationPageBucket) Bounds() Limits {
	bounds, _ := NormalizeLimits(Limits{})
	return bounds
}

type cancellationWholeBucket struct {
	*cancellationPageBucket
	wholeCalls int
	cancel     context.CancelFunc
}

func (b *cancellationWholeBucket) ListAll(context.Context, string, Limits) ([]Metadata, error) {
	b.wholeCalls++
	if b.cancel != nil {
		b.cancel()
	}
	return []Metadata{{Key: "item"}}, nil
}

func TestListAllCancellationPreventsListingAndSuccessfulResults(t *testing.T) {
	for _, mode := range []string{"pre-canceled paged", "pre-canceled whole", "canceled whole result", "canceled paged result"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			page := &cancellationPageBucket{}
			whole := &cancellationWholeBucket{cancellationPageBucket: page}
			var bucket Bucket = whole
			wantWholeCalls := 0
			wantPageCalls := 0
			switch mode {
			case "pre-canceled paged":
				bucket = page
				cancel()
			case "pre-canceled whole":
				cancel()
			case "canceled paged result":
				bucket = page
				page.cancel = cancel
				wantPageCalls = 1
			case "canceled whole result":
				whole.cancel = cancel
				wantWholeCalls = 1
			}
			got, err := ListAll(ctx, bucket, "", Limits{})
			if !errors.Is(err, context.Canceled) || got != nil {
				t.Fatalf("listing admitted canceled work: objects=%v error=%v", got, err)
			}
			if page.calls != wantPageCalls || whole.wholeCalls != wantWholeCalls {
				t.Fatalf("listing calls: paged=%d whole=%d, want paged=%d whole=%d", page.calls, whole.wholeCalls, wantPageCalls, wantWholeCalls)
			}
		})
	}
}
