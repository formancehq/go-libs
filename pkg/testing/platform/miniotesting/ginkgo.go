package miniotesting

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/formancehq/go-libs/v5/pkg/testing/deferred"
	. "github.com/formancehq/go-libs/v5/pkg/testing/docker/ginkgo"
)

func WithNewMinioServer(fn func(p *deferred.Deferred[*MinioServer]), opts ...Option) bool {
	return Context("With new minio server", func() {
		ret := deferred.New[*MinioServer]()
		BeforeEach(func() {
			ret.Reset()
			ret.SetValue(CreateMinioServer(
				GinkgoT(),
				ActualDockerPool(),
				opts...,
			))
		})
		fn(ret)
	})
}
