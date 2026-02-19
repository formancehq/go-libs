package ginkgo

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/formancehq/go-libs/v4/testing/deferred"
	"github.com/formancehq/go-libs/v4/testing/testservice"
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

			err := service.Start(context.Background())
			if err != nil {
				d.SetErr(err)
				Fail(err.Error())
			}
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
