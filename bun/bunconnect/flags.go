package bunconnect

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v4/aws/iam"
	"github.com/formancehq/go-libs/v4/logging"
)

const (
	PostgresURIFlag             = "postgres-uri"
	PostgresAWSEnableIAMFlag    = "postgres-aws-enable-iam"
	PostgresMaxIdleConnsFlag    = "postgres-max-idle-conns"
	PostgresMaxOpenConnsFlag    = "postgres-max-open-conns"
	PostgresConnMaxIdleTimeFlag = "postgres-conn-max-idle-time"
	PostgresConnMaxLifetimeFlag = "postgres-conn-max-lifetime"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(PostgresURIFlag, "", "Postgres URI")
	flags.Bool(PostgresAWSEnableIAMFlag, false, "Enable AWS IAM authentication")
	flags.Int(PostgresMaxIdleConnsFlag, 0, "Max Idle connections")
	flags.Duration(PostgresConnMaxIdleTimeFlag, time.Minute, "Max Idle time for connections")
	flags.Duration(PostgresConnMaxLifetimeFlag, 0, "Max lifetime for connections")
	flags.Int(PostgresMaxOpenConnsFlag, 20, "Max opened connections")
}

type Option func(options *pgx.ConnConfig)

func WithRuntimeParams(params map[string]string) Option {
	return func(options *pgx.ConnConfig) {
		for k, v := range params {
			options.RuntimeParams[k] = v
		}
	}
}

func GetAWSIAMAuthConnector(cmd *cobra.Command, opts ...Option) (func(s string) (driver.Connector, error), error) {
	var cfg aws.Config
	var err error
	var ctx context.Context

	if cmd != nil {
		ctx = cmd.Context()
		cfg, err = config.LoadDefaultConfig(ctx, iam.LoadOptionFromCommand(cmd))
	} else {
		ctx = context.Background()
		cfg, err = config.LoadDefaultConfig(ctx)
	}

	if err != nil {
		return nil, err
	}

	connector := func(s string) (driver.Connector, error) {
		return &iamConnector{
			dsn: s,
			driver: &iamDriver{
				awsConfig: cfg,
			},
			options: opts,
			logger:  logging.FromContext(ctx),
		}, nil
	}

	return connector, nil
}

func ConnectionOptionsFromFlags(cmd *cobra.Command, opts ...Option) (*ConnectionOptions, error) {
	var err error
	var connector func(string) (driver.Connector, error)

	awsEnable, _ := cmd.Flags().GetBool(PostgresAWSEnableIAMFlag)
	if awsEnable {
		connector, err = GetAWSIAMAuthConnector(cmd, opts...)
		if err != nil {
			return nil, err
		}
	} else {
		connector = func(dsn string) (driver.Connector, error) {
			parseConfig, err := pgx.ParseConfig(dsn)
			if err != nil {
				return nil, fmt.Errorf("failed to parse dsn: %w", err)
			}

			for _, opt := range opts {
				opt(parseConfig)
			}

			parseConfig.Tracer = newPgxTracer()

			return stdlib.GetConnector(*parseConfig), nil
		}
	}

	postgresUri, _ := cmd.Flags().GetString(PostgresURIFlag)
	if postgresUri == "" {
		return nil, errors.New("missing postgres uri")
	}
	maxIdleConns, _ := cmd.Flags().GetInt(PostgresMaxIdleConnsFlag)
	connMaxIdleConns, _ := cmd.Flags().GetDuration(PostgresConnMaxIdleTimeFlag)
	connMaxLifetime, _ := cmd.Flags().GetDuration(PostgresConnMaxLifetimeFlag)
	maxOpenConns, _ := cmd.Flags().GetInt(PostgresMaxOpenConnsFlag)

	return &ConnectionOptions{
		DatabaseSourceName: postgresUri,
		MaxIdleConns:       maxIdleConns,
		ConnMaxIdleTime:    connMaxIdleConns,
		ConnMaxLifetime:    connMaxLifetime,
		MaxOpenConns:       maxOpenConns,
		Connector:          connector,
	}, nil
}
