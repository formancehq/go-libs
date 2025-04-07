package ginkgo

import (
	"context"
	"time"

	"github.com/formancehq/go-libs/v3/testing/testservice"

	"github.com/formancehq/go-libs/v3/testing/deferred"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

func DeferNew(
	commandFactory func() *cobra.Command,
	options ...testservice.Option,
) *deferred.Deferred[*testservice.Service] {
	d := deferred.New[*testservice.Service]()
	BeforeEach(func() {
		d.Reset()

		service := testservice.New(commandFactory, options...)
		go func() {
			defer GinkgoRecover()

			Expect(service.Start(context.Background())).To(Succeed())
			d.SetValue(service)
		}()

		DeferCleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			Expect(service.Stop(ctx)).To(Succeed())
		})
	})
	return d
}
