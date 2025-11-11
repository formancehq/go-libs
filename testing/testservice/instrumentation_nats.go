package testservice

import (
	"context"
	"fmt"

	"github.com/formancehq/go-libs/v3/publish"
	"github.com/formancehq/go-libs/v3/testing/deferred"
)

func NatsInstrumentation(url *deferred.Deferred[string]) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, runConfiguration *RunConfiguration) error {
		url, err := url.Wait(ctx)
		if err != nil {
			return err
		}
		runConfiguration.AppendArgs(
			"--"+publish.PublisherNatsEnabledFlag,
			"--"+publish.PublisherNatsURLFlag, url,
			"--"+publish.PublisherTopicMappingFlag, fmt.Sprintf("*:%s", runConfiguration.GetID()),
		)

		return nil
	})
}
