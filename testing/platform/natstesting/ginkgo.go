package natstesting

import (
	"github.com/formancehq/go-libs/v3/logging"
	"github.com/formancehq/go-libs/v3/testing/deferred"
	. "github.com/onsi/ginkgo/v2"
)

func WithNewNatsServer(logger logging.Logger, fn func(p *deferred.Deferred[*NatsServer])) bool {
	return Context("With new nats server", func() {
		ret := deferred.New[*NatsServer]()
		BeforeEach(func() {
			ret.Reset()
			ret.SetValue(CreateServer(
				GinkgoT(),
				true,
				logger,
			))
		})
		fn(ret)
	})
}
