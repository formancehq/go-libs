package testservice

import (
	"context"
	"fmt"

	"github.com/formancehq/go-libs/v2/testing/deferred"

	"github.com/formancehq/go-libs/v2/bun/bunconnect"
)

func PostgresInstrumentation(postgresConfiguration *deferred.Deferred[bunconnect.ConnectionOptions]) Instrumentation {
	return InstrumentationFunc(func(ctx context.Context, cfg *RunConfiguration) error {
		postgresConfiguration, err := postgresConfiguration.Wait(ctx)
		if err != nil {
			return err
		}

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

		return nil
	})
}
