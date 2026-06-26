package connect

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v5/pkg/cloud/aws/iam"
	otlp "github.com/formancehq/go-libs/v5/pkg/observe"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// PgxPoolOption customizes a *pgxpool.Config built by this package.
type PgxPoolOption func(*pgxpool.Config)

// WithPgxPoolBeforeConnect chains a BeforeConnect hook onto the pool. Hooks
// installed by previous options run first; the new hook only fires if they
// all return nil.
func WithPgxPoolBeforeConnect(fn func(context.Context, *pgx.ConnConfig) error) PgxPoolOption {
	return func(cfg *pgxpool.Config) {
		prev := cfg.BeforeConnect
		cfg.BeforeConnect = func(ctx context.Context, cc *pgx.ConnConfig) error {
			if prev != nil {
				if err := prev(ctx, cc); err != nil {
					return err
				}
			}
			return fn(ctx, cc)
		}
	}
}

// WithPgxPoolIAMAuth installs a BeforeConnect hook that mints a fresh AWS RDS
// IAM authentication token for every new connection acquired by the pool. RDS
// IAM tokens are short-lived (15 min), so the per-connect refresh matches the
// AWS-recommended usage pattern.
func WithPgxPoolIAMAuth(awsConfig aws.Config) PgxPoolOption {
	return WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
		endpoint := fmt.Sprintf("%s:%d", cc.Host, cc.Port)
		token, err := buildIAMAuthToken(ctx, awsConfig, endpoint, cc.User)
		if err != nil {
			return errors.Wrap(err, "building aws auth token")
		}
		cc.Password = token
		return nil
	})
}

// BuildPgxPoolConfig parses dsn into a *pgxpool.Config wired with this
// package's pgx tracer, then applies opts in order. The returned config can be
// passed to pgxpool.NewWithConfig.
func BuildPgxPoolConfig(ctx context.Context, dsn string, opts ...PgxPoolOption) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dsn: %w", err)
	}
	cfg.ConnConfig.Tracer = newPgxTracer()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg, nil
}

// PgxPoolConfigFromFlags builds a *pgxpool.Config from the standard Postgres
// flags (see AddFlags) plus the AWS IAM flags (see iam.AddFlags). When
// PostgresAWSEnableIAMFlag is set, AWS IAM authentication is wired in via a
// BeforeConnect hook. PostgresMaxIdleConnsFlag has no equivalent on pgxpool
// and is ignored.
func PgxPoolConfigFromFlags(flags *pflag.FlagSet, ctx context.Context, opts ...PgxPoolOption) (*pgxpool.Config, error) {
	dsn, _ := flags.GetString(PostgresURIFlag)
	if dsn == "" {
		return nil, errors.New("missing postgres uri")
	}

	if awsEnable, _ := flags.GetBool(PostgresAWSEnableIAMFlag); awsEnable {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, iam.LoadOptionFromFlags(flags))
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithPgxPoolIAMAuth(awsCfg))
		logging.FromContext(ctx).Debugf("pgxpool: AWS IAM authentication enabled")
	}

	cfg, err := BuildPgxPoolConfig(ctx, dsn, opts...)
	if err != nil {
		return nil, err
	}

	if v, _ := flags.GetInt(PostgresMaxOpenConnsFlag); v > 0 {
		cfg.MaxConns = int32(v)
	}
	if v, _ := flags.GetDuration(PostgresConnMaxIdleTimeFlag); v > 0 {
		cfg.MaxConnIdleTime = v
	}
	if v, _ := flags.GetDuration(PostgresConnMaxLifetimeFlag); v > 0 {
		cfg.MaxConnLifetime = v
	}
	return cfg, nil
}

// buildIAMAuthToken wraps auth.BuildAuthToken with the package tracer and
// OTLP error recording. Shared between the database/sql iamConnector and the
// pgxpool BeforeConnect hook.
func buildIAMAuthToken(ctx context.Context, awsConfig aws.Config, endpoint, user string) (string, error) {
	ctx, span := tracer.Start(ctx, "iam.build-auth-token")
	defer span.End()

	token, err := auth.BuildAuthToken(ctx, endpoint, awsConfig.Region, user, awsConfig.Credentials)
	if err != nil {
		otlp.RecordError(ctx, err)
		return "", err
	}
	return token, nil
}
