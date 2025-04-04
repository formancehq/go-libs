package testservice

import (
	"context"
	. "github.com/formancehq/go-libs/v2/testing/utils"
	"net/url"

	"github.com/formancehq/go-libs/v2/httpserver"
)

func HTTPServerInstrumentation() Instrumentation {
	return InstrumentationFunc(func(cfg *RunConfiguration) {
		cfg.WrapContext(func(ctx context.Context) context.Context {
			return httpserver.ContextWithServerInfo(ctx)
		})
	})
}

func GetServerURL(service *Service) *url.URL {
	rawUrl := httpserver.URL(service.GetContext())
	if rawUrl == "" {
		return nil
	}

	url, err := url.Parse(rawUrl)
	if err != nil {
		panic(err)
	}

	return url
}

func DeferGetServerURL(service *Deferred[*Service]) *Deferred[*url.URL] {
	return MapDeferred(service, GetServerURL)
}
