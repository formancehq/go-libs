package testservice

import (
	"fmt"

	"github.com/formancehq/go-libs/v2/publish"
	"github.com/formancehq/go-libs/v2/testing/utils"
)

func NatsInstrumentation(url *utils.Deferred[string]) Instrumentation {
	return InstrumentationFunc(func(runConfiguration *RunConfiguration) {
		runConfiguration.AppendArgs(
			"--"+publish.PublisherNatsEnabledFlag,
			"--"+publish.PublisherNatsURLFlag, url.Wait(),
			"--"+publish.PublisherTopicMappingFlag, fmt.Sprintf("*:%s", runConfiguration.GetID()),
		)
	})
}
