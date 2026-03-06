package ginkgo

import (
	. "github.com/onsi/ginkgo/v2"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/testing/docker"
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
