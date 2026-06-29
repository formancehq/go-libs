package connect

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"

	otlp "github.com/formancehq/go-libs/v5/pkg/observe"
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

// iamTokenMinter abstracts token generation so tests can inject a stub without
// reaching to AWS. Production code uses buildIAMAuthToken via WithPgxPoolIAMAuth.
type iamTokenMinter func(ctx context.Context, awsConfig aws.Config, endpoint, user string) (string, error)

// WithPgxPoolIAMAuth installs a BeforeConnect hook that mints a fresh AWS RDS
// IAM authentication token for every new connection acquired by the pool. RDS
// IAM tokens are short-lived (15 min), so the per-connect refresh matches the
// AWS-recommended usage pattern.
func WithPgxPoolIAMAuth(awsConfig aws.Config) PgxPoolOption {
	return withPgxPoolIAMAuthMinter(awsConfig, buildIAMAuthToken)
}

func withPgxPoolIAMAuthMinter(awsConfig aws.Config, mint iamTokenMinter) PgxPoolOption {
	return WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
		endpoint := fmt.Sprintf("%s:%d", cc.Host, cc.Port)
		token, err := mint(ctx, awsConfig, endpoint, cc.User)
		if err != nil {
			return errors.Wrap(err, "building aws auth token")
		}
		cc.Password = token
		return nil
	})
}

// BuildPgxPoolConfig parses dsn into a *pgxpool.Config wired with this
// package's pgx tracer, then applies opts in order. ValidateConnect is set to
// the same read-write probe the database/sql connector uses, so an endpoint
// that resolves to a read replica/hot standby is rejected at connect time
// (instead of letting a read-only connection through and failing later on the
// first write). The returned config can be passed to pgxpool.NewWithConfig.
func BuildPgxPoolConfig(ctx context.Context, dsn string, opts ...PgxPoolOption) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dsn: %w", err)
	}
	cfg.ConnConfig.Tracer = newPgxTracer()
	if cfg.ConnConfig.ValidateConnect == nil {
		cfg.ConnConfig.ValidateConnect = validateConnectTargetSessionAttrsReadWrite
	}
	for _, opt := range opts {
		opt(cfg)
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
