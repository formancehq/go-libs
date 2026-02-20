package clickhousetesting

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/formancehq/go-libs/v4/testing/deferred"
	. "github.com/formancehq/go-libs/v4/testing/docker/ginkgo"
)

func WithClickhouse(fn func(d *deferred.Deferred[*Server])) {
	Context("with clickhouse", func() {
		ret := deferred.New[*Server]()
		BeforeEach(func() {
			ret.Reset()
			ret.SetValue(CreateServer(ActualDockerPool()))
		})
		fn(ret)
	})
}

func WithNewDatabase(srv *deferred.Deferred[*Server], fn func(d *deferred.Deferred[*Database])) {
	Context("with new database", func() {
		ret := deferred.New[*Database]()
		BeforeEach(func() {
			ret.Reset()
			ret.SetValue(srv.GetValue().NewDatabase(GinkgoT()))
		})
		fn(ret)
	})
}
