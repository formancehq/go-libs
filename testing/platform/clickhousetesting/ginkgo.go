package clickhousetesting

import (
	"github.com/formancehq/go-libs/v2/testing/deferred"
	. "github.com/formancehq/go-libs/v2/testing/docker/ginkgo"
	. "github.com/onsi/ginkgo/v2"
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
