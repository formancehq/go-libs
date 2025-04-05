package ginkgo

import (
	"context"

	"github.com/formancehq/go-libs/v2/testing/deferred"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func DeferMap[From, To any](d *deferred.Deferred[From], mapper func(From) To) *deferred.Deferred[To] {
	ret := deferred.New[To]()
	BeforeEach(func(specContext SpecContext) {
		ret.Reset()
		ret.LoadAsync(func() (To, error) {
			return deferred.WaitAndMap(d, mapper)
		})
	})
	return ret
}

func Wait[V any](ctx context.Context, d *deferred.Deferred[V]) V {
	v, err := d.Wait(ctx)
	Expect(err).To(BeNil())

	return v
}
