package pgtesting

import (
	"github.com/formancehq/go-libs/v3/testing/deferred"
	. "github.com/formancehq/go-libs/v3/testing/docker/ginkgo"
	. "github.com/onsi/ginkgo/v2"
)

func WithNewPostgresServer(fn func(p *deferred.Deferred[*PostgresServer])) bool {
	return Context("With new postgres server", func() {
		ret := deferred.New[*PostgresServer]()
		BeforeEach(func() {
			ret.Reset()
			ret.SetValue(CreatePostgresServer(
				GinkgoT(),
				ActualDockerPool(),
			))
		})
		fn(ret)
	})
}

func UsePostgresDatabase(server *deferred.Deferred[*PostgresServer], options ...CreateDatabaseOption) *deferred.Deferred[*Database] {
	ret := &deferred.Deferred[*Database]{}
	BeforeEach(func() {
		ret.Reset()
		ret.SetValue(server.GetValue().NewDatabase(GinkgoT(), options...))
	})
	return ret
}

func WithNewPostgresDatabase(server *deferred.Deferred[*PostgresServer], fn func(p *deferred.Deferred[*Database])) {
	Context("With new postgres database", func() {
		fn(UsePostgresDatabase(server))
	})
}
