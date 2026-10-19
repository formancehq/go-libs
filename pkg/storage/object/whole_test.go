package object

import (
	"context"
	"errors"
	"testing"
)

type wholeBucket struct {
	currentBucket
	objects []Metadata
	err     error
}

func (b *wholeBucket) ListAll(context.Context, string, Limits) ([]Metadata, error) {
	return b.objects, b.err
}

func TestWholeListingCannotBypassAggregateAdmission(t *testing.T) {
	failure := errors.New("listing failed")
	for _, tc := range []struct {
		name          string
		count         int
		failure, want error
	}{
		{"oversized success", 2, nil, ErrListingLimit},
		{"partial failure", 1, failure, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &wholeBucket{objects: make([]Metadata, tc.count), err: tc.failure}
			got, err := ListAll(t.Context(), b, "", Limits{MaxObjects: 1})
			if got != nil || !errors.Is(err, tc.want) {
				t.Fatal("whole-list implementation bypassed bounds or returned partial inventory")
			}
		})
	}
}
