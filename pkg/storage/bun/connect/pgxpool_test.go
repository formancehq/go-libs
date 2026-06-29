package connect

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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

func TestBuildIAMAuthToken_ProducesValidSigV4PresignedURL(t *testing.T) {
	t.Parallel()

	awsCfg := aws.Config{
		Region:      "eu-west-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIATESTACCESSKEY", "SECRETTESTKEY", ""),
	}

	token, err := buildIAMAuthToken(context.Background(), awsCfg, "db.example.com:5432", "iam-user")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	// auth.BuildAuthToken returns "host:port/?query" — no scheme. Prepend a
	// dummy one so net/url can parse it.
	parsed, err := url.Parse("rds://" + token)
	require.NoError(t, err, "token must parse as a URL")

	require.Equal(t, "db.example.com:5432", parsed.Host, "token must carry the RDS endpoint")

	q := parsed.Query()
	require.Equal(t, "connect", q.Get("Action"), "Action must be 'connect' (rds-db connect verb)")
	require.Equal(t, "iam-user", q.Get("DBUser"), "DBUser must match the connect user")
	require.Equal(t, "AWS4-HMAC-SHA256", q.Get("X-Amz-Algorithm"), "must be SigV4-signed")
	require.NotEmpty(t, q.Get("X-Amz-Date"), "must carry the signing timestamp")
	require.NotEmpty(t, q.Get("X-Amz-Signature"), "must be signed")
	require.True(t, regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(q.Get("X-Amz-Signature")),
		"signature must be a 64-hex-char HMAC-SHA256")

	credential := q.Get("X-Amz-Credential")
	require.Contains(t, credential, "AKIATESTACCESSKEY/", "credential scope must start with the access key id")
	require.Contains(t, credential, "/eu-west-1/rds-db/aws4_request",
		"credential scope must target rds-db in the configured region")

	require.NotEmpty(t, q.Get("X-Amz-Expires"), "must carry a lifetime")
	expires, err := strconv.Atoi(q.Get("X-Amz-Expires"))
	require.NoError(t, err)
	require.LessOrEqual(t, expires, 900,
		"RDS IAM tokens are documented as 15-minute (900s) lifetime — anything longer indicates a misconfigured signer")
	require.Greater(t, expires, 0)
}

func TestWithPgxPoolIAMAuthMinter_PropagatesEndpointAndUser(t *testing.T) {
	t.Parallel()

	var gotEndpoint, gotUser string
	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://iam-user@db.example.com:6432/app?sslmode=require",
		withPgxPoolIAMAuthMinter(aws.Config{}, func(_ context.Context, _ aws.Config, endpoint, user string) (string, error) {
			gotEndpoint = endpoint
			gotUser = user
			return "stub-token", nil
		}),
	)
	require.NoError(t, err)
	require.NoError(t, cfg.BeforeConnect(context.Background(), cfg.ConnConfig))

	require.Equal(t, "db.example.com:6432", gotEndpoint, "minter must receive the parsed host:port")
	require.Equal(t, "iam-user", gotUser, "minter must receive the parsed user")
	require.Equal(t, "stub-token", cfg.ConnConfig.Password, "minter return value must land on ConnConfig.Password")
}

func TestWithPgxPoolIAMAuthMinter_WrapsMintError(t *testing.T) {
	t.Parallel()

	cfg, err := BuildPgxPoolConfig(context.Background(),
		"postgres://iam-user@db.example.com:5432/app",
		withPgxPoolIAMAuthMinter(aws.Config{}, func(_ context.Context, _ aws.Config, _, _ string) (string, error) {
			return "", errFirstHook
		}),
	)
	require.NoError(t, err)

	err = cfg.BeforeConnect(context.Background(), cfg.ConnConfig)
	require.Error(t, err)
	require.Contains(t, err.Error(), "building aws auth token", "minter error must be wrapped for diagnostics")
}

