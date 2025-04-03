package testservice

import (
	"context"
	"time"

	. "github.com/formancehq/go-libs/v2/testing/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

func DeferNew[Cfg SpecializedConfiguration](
	commandFactory func() *cobra.Command,
	configurationProvider func() Configuration[Cfg],
	options ...Option,
) *Deferred[*Service[Cfg]] {
	d := NewDeferred[*Service[Cfg]]()
	BeforeEach(func() {
		d.Reset()

		service := New[Cfg](
			commandFactory,
			configurationProvider(),
			options...,
		)
		Expect(service.Start(context.Background())).To(Succeed())

		DeferCleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			Expect(service.Stop(ctx)).To(Succeed())
		})

		d.SetValue(service)
	})
	return d
}
