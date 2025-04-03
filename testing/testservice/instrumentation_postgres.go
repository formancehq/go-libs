package testservice

import (
	"fmt"

	"github.com/formancehq/go-libs/v2/bun/bunconnect"
	"github.com/formancehq/go-libs/v2/testing/utils"
)

func PostgresInstrumentation(postgresConfiguration *utils.Deferred[bunconnect.ConnectionOptions]) Instrumentation {
	return InstrumentationFunc(func(cfg *RunConfiguration) {
		postgresConfiguration := postgresConfiguration.Wait()

		cfg.AppendArgs("--"+bunconnect.PostgresURIFlag, postgresConfiguration.DatabaseSourceName)
		if postgresConfiguration.MaxIdleConns != 0 {
			cfg.AppendArgs("--"+bunconnect.PostgresMaxIdleConnsFlag, fmt.Sprint(postgresConfiguration.MaxIdleConns))
		}
		if postgresConfiguration.MaxOpenConns != 0 {
			cfg.AppendArgs("--"+bunconnect.PostgresMaxOpenConnsFlag, fmt.Sprint(postgresConfiguration.MaxOpenConns))
		}
		if postgresConfiguration.ConnMaxIdleTime != 0 {
			cfg.AppendArgs("--"+bunconnect.PostgresConnMaxIdleTimeFlag, fmt.Sprint(postgresConfiguration.ConnMaxIdleTime))
		}
	})
}
