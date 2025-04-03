package testservice

import (
	"context"
	"time"

	. "github.com/formancehq/go-libs/v2/testing/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

func DeferNew(
	commandFactory func() *cobra.Command,
	options ...Option,
) *Deferred[*Service] {
	d := NewDeferred[*Service]()
	BeforeEach(func() {
		d.Reset()

		service := New(commandFactory, options...)
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
