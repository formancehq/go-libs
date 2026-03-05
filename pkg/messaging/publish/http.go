package publish

import (
	"net/http"

	"github.com/ThreeDotsLabs/watermill"
	wHttp "github.com/ThreeDotsLabs/watermill-http/v2/pkg/http"
	"github.com/ThreeDotsLabs/watermill/message"
)

func NewHTTPPublisher(logger watermill.LoggerAdapter, config wHttp.PublisherConfig) (*wHttp.Publisher, error) {
	return wHttp.NewPublisher(config, logger)
}

func NewHTTPPublisherConfig(httpClient *http.Client, m wHttp.MarshalMessageFunc) wHttp.PublisherConfig {
	return wHttp.PublisherConfig{
		MarshalMessageFunc: m,
		Client:             httpClient,
	}
}

func DefaultHTTPMarshalMessageFunc() wHttp.MarshalMessageFunc {
	return func(url string, msg *message.Message) (*http.Request, error) {
		req, err := wHttp.DefaultMarshalMessageFunc(url, msg)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}
}
