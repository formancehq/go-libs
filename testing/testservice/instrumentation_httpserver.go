package testservice

import (
	"context"
	"net/url"

	"github.com/formancehq/go-libs/v2/httpserver"
)

func HTTPServerInstrumentation() Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		cfg.WrapContext(func(ctx context.Context) context.Context {
			return httpserver.ContextWithServerInfo(ctx)
		})
		return nil
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
