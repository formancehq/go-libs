package ginkgo

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/testing/docker"
)

var pool = new(docker.Pool)

func ActualDockerPool() *docker.Pool {
	return pool
}

func WithNewDockerPool(logger logging.Logger, fn func()) bool {
	return Context("With docker pool", func() {
		BeforeEach(func() {
			*pool = *docker.NewPool(GinkgoT(), logger)
		})
		fn()
	})
}
