package pgtesting

import (
	. "github.com/formancehq/go-libs/v2/testing/docker/ginkgo"
	. "github.com/formancehq/go-libs/v2/testing/utils"
	. "github.com/onsi/ginkgo/v2"
)

func WithNewPostgresServer(fn func(p *Deferred[*PostgresServer])) bool {
	return Context("With new postgres server", func() {
		ret := NewDeferred[*PostgresServer]()
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

func UsePostgresDatabase(server *Deferred[*PostgresServer], options ...CreateDatabaseOption) *Deferred[*Database] {
	ret := &Deferred[*Database]{}
	BeforeEach(func() {
		ret.Reset()
		ret.SetValue(server.GetValue().NewDatabase(GinkgoT(), options...))
	})
	return ret
}

func WithNewPostgresDatabase(server *Deferred[*PostgresServer], fn func(p *Deferred[*Database])) {
	Context("With new postgres database", func() {
		fn(UsePostgresDatabase(server))
	})
}
