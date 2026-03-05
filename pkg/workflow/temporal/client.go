package temporal

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type SearchAttributes struct {
	SearchAttributes map[string]enums.IndexedValueType
}

type ClientConfig struct {
	Address           string
	Namespace         string
	TLSCertPEM        string
	TLSKeyPEM         string
	EncryptionEnabled bool
	EncryptionKey     string
}

func NewClientOptions(
	cfg ClientConfig,
	tracer trace.Tracer,
	logger logging.Logger,
	meterProvider metric.MeterProvider,
) (client.Options, error) {
	var cert *tls.Certificate
	if cfg.TLSKeyPEM != "" && cfg.TLSCertPEM != "" {
		clientCert, err := tls.X509KeyPair([]byte(cfg.TLSCertPEM), []byte(cfg.TLSKeyPEM))
		if err != nil {
			return client.Options{}, err
		}
		cert = &clientCert
	}

	tracingInterceptor, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{
		Tracer: tracer,
	})
	if err != nil {
		return client.Options{}, err
	}

	options := client.Options{
		Namespace:    cfg.Namespace,
		HostPort:     cfg.Address,
		Interceptors: []interceptor.ClientInterceptor{tracingInterceptor},
		Logger:       newLogger(logger),
	}

	if cfg.EncryptionEnabled {
		converter, err := NewEncryptionDataConverter([]byte(cfg.EncryptionKey))
		if err != nil {
			return client.Options{}, err
		}
		options.DataConverter = converter
	}

	if cert != nil {
		options.ConnectionOptions = client.ConnectionOptions{
			TLS: &tls.Config{Certificates: []tls.Certificate{*cert}},
		}
	}

	if meterProvider != nil {
		logger.Info("temporal sdk metrics handler initiated")
		metricsHandler := opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{
			Meter: meterProvider.Meter(fmt.Sprintf("go-temporal-sdk-%s", cfg.Namespace)),
		})
		options.MetricsHandler = metricsHandler
	}

	return options, nil
}

func CreateSearchAttributes(ctx context.Context, c client.Client, namespace string, searchAttributes map[string]enums.IndexedValueType) error {
	_, err := c.OperatorService().AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{
		SearchAttributes: searchAttributes,
		Namespace:        namespace,
	})
	if err != nil {
		if _, ok := err.(*serviceerror.AlreadyExists); !ok {
			return err
		}
	}
	for {
		ret, err := c.OperatorService().ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{
			Namespace: namespace,
		})
		if err != nil {
			panic(err)
		}

		done := true
		for key := range searchAttributes {
			if ret.CustomAttributes[key] == enums.INDEXED_VALUE_TYPE_UNSPECIFIED {
				done = false
				break
			}
		}

		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
