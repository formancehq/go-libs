package connect

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
)

func TestBuildPgxPoolConfigParsesDSNAndSetsTracer(t *testing.T) {
	cfg, err := BuildPgxPoolConfig(context.Background(), "postgres://u:p@db.example.com:5432/app?sslmode=disable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ConnConfig.Host != "db.example.com" {
		t.Fatalf("unexpected host: %q", cfg.ConnConfig.Host)
	}
	if cfg.ConnConfig.Port != 5432 {
		t.Fatalf("unexpected port: %d", cfg.ConnConfig.Port)
	}
	if cfg.ConnConfig.Tracer == nil {
		t.Fatal("expected tracer to be set")
	}
	if cfg.ConnConfig.ValidateConnect == nil {
		t.Fatal("expected ValidateConnect to default to the read-write probe, matching the database/sql connector")
	}
}

func TestBuildPgxPoolConfigPreservesUserValidateConnect(t *testing.T) {
	called := false
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://u:p@db.example.com:5432/app",
		func(c *pgxpool.Config) {
			c.ConnConfig.ValidateConnect = func(ctx context.Context, _ *pgconn.PgConn) error {
				called = true
				return nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.ValidateConnect == nil {
		t.Fatal("expected ValidateConnect to remain set")
	}
	if err := cfg.ConnConfig.ValidateConnect(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected the user-provided ValidateConnect to be invoked, not the default")
	}
}

func TestBuildPgxPoolConfigInvalidDSN(t *testing.T) {
	_, err := BuildPgxPoolConfig(context.Background(), "not-a-dsn://")
	if err == nil {
		t.Fatal("expected error for invalid dsn")
	}
}

func TestWithPgxPoolBeforeConnectChainsHooks(t *testing.T) {
	var order []string
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://u:p@db.example.com:5432/app",
		WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
			order = append(order, "first")
			return nil
		}),
		WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
			order = append(order, "second")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BeforeConnect == nil {
		t.Fatal("expected BeforeConnect to be set")
	}
	if err := cfg.BeforeConnect(context.Background(), cfg.ConnConfig); err != nil {
		t.Fatalf("BeforeConnect failed: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("unexpected hook order: %v", order)
	}
}

func TestWithPgxPoolBeforeConnectStopsOnError(t *testing.T) {
	secondCalled := false
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://u:p@db.example.com:5432/app",
		WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
			return errFirstHook
		}),
		WithPgxPoolBeforeConnect(func(ctx context.Context, cc *pgx.ConnConfig) error {
			secondCalled = true
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.BeforeConnect(context.Background(), cfg.ConnConfig); err == nil {
		t.Fatal("expected first hook error to propagate")
	}
	if secondCalled {
		t.Fatal("expected second hook to be skipped after first hook errored")
	}
}

func TestWithPgxPoolIAMAuthSetsTokenAsPassword(t *testing.T) {
	awsCfg := aws.Config{
		Region:      "eu-west-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIATEST", "SECRETTEST", ""),
	}
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://iam-user@db.example.com:5432/app?sslmode=require",
		WithPgxPoolIAMAuth(awsCfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BeforeConnect == nil {
		t.Fatal("expected BeforeConnect to be set by WithPgxPoolIAMAuth")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cfg.BeforeConnect(ctx, cfg.ConnConfig); err != nil {
		t.Fatalf("BeforeConnect failed: %v", err)
	}

	pw := cfg.ConnConfig.Password
	if pw == "" {
		t.Fatal("expected password to be set to IAM auth token")
	}
	if !strings.Contains(pw, "db.example.com") {
		t.Fatalf("expected IAM auth token to embed host, got %q", pw)
	}
	if !strings.Contains(pw, "X-Amz-Signature") {
		t.Fatalf("expected IAM auth token to be sigv4-signed, got %q", pw)
	}
	if !strings.Contains(pw, "DBUser=iam-user") {
		t.Fatalf("expected IAM auth token to encode the DB user, got %q", pw)
	}
}

func TestWithPgxPoolIAMAuthRefreshesTokenOnEachInvocation(t *testing.T) {
	awsCfg := aws.Config{
		Region:      "eu-west-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIATEST", "SECRETTEST", ""),
	}
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://iam-user@db.example.com:5432/app?sslmode=require",
		WithPgxPoolIAMAuth(awsCfg),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.BeforeConnect(context.Background(), cfg.ConnConfig); err != nil {
		t.Fatal(err)
	}
	first := cfg.ConnConfig.Password

	// AWS auth tokens embed a timestamp (X-Amz-Date), so two invocations a
	// second apart MUST produce distinct tokens. This protects against a
	// future refactor that would cache the token across acquires.
	time.Sleep(1100 * time.Millisecond)

	if err := cfg.BeforeConnect(context.Background(), cfg.ConnConfig); err != nil {
		t.Fatal(err)
	}
	second := cfg.ConnConfig.Password

	if first == second {
		t.Fatalf("expected token to be refreshed per Connect, got identical tokens: %q", first)
	}
}

var errFirstHook = stringErr("first hook failed")

type stringErr string

func (e stringErr) Error() string { return string(e) }

func TestPgxPoolConfigFromFlagsAppliesZeroDurations(t *testing.T) {
	// Flags default ConnMaxLifetime to 0; the pgxpool helper MUST honor that
	// rather than letting pgxpool's 1-hour default win, so the behavior
	// matches database/sql.SetConnMaxLifetime(0) = "no recycling".
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	if err := flags.Parse([]string{
		"--postgres-uri=postgres://u:p@db.example.com:5432/app?sslmode=disable",
		"--postgres-conn-max-lifetime=0",
		"--postgres-conn-max-idle-time=0",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := PgxPoolConfigFromFlags(flags, context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MaxConnLifetime != 0 {
		t.Fatalf("expected MaxConnLifetime to be 0 (no recycling), got %s", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 0 {
		t.Fatalf("expected MaxConnIdleTime to be 0 (no idle recycling), got %s", cfg.MaxConnIdleTime)
	}
}
