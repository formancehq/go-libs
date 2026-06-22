package connect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jackc/pgx/v5"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestObfuscateDSN(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		{
			name:     "with user and password",
			dsn:      "postgres://user:secret@localhost:5432/mydb",
			expected: "postgres://user:%2A%2A%2A%2A@localhost:5432/mydb",
		},
		{
			name:     "with user only",
			dsn:      "postgres://user@localhost:5432/mydb",
			expected: "postgres://user:%2A%2A%2A%2A@localhost:5432/mydb",
		},
		{
			name:     "without credentials",
			dsn:      "postgres://localhost:5432/mydb",
			expected: "postgres://localhost:5432/mydb",
		},
		{
			name:     "with query params",
			dsn:      "postgres://user:secret@localhost:5432/mydb?sslmode=disable",
			expected: "postgres://user:%2A%2A%2A%2A@localhost:5432/mydb?sslmode=disable",
		},
		{
			name:     "with password in query params",
			dsn:      "postgres://localhost:5432/mydb?password=secret&sslmode=disable",
			expected: "postgres://localhost:5432/mydb?password=%2A%2A%2A%2A&sslmode=disable",
		},
		{
			name:     "with both userinfo and query param password",
			dsn:      "postgres://user:secret@localhost:5432/mydb?password=secret2",
			expected: "postgres://user:%2A%2A%2A%2A@localhost:5432/mydb?password=%2A%2A%2A%2A",
		},
		{
			name:     "invalid dsn",
			dsn:      "://invalid",
			expected: "***",
		},
		{
			name:     "empty string",
			dsn:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := obfuscateDSN(tt.dsn)
			if got != tt.expected {
				t.Errorf("obfuscateDSN(%q) = %q, want %q", tt.dsn, got, tt.expected)
			}
		})
	}
}

func TestBuildPGXConnectorDefaultsToReadWriteTargetSessionAttrs(t *testing.T) {
	config, err := pgx.ParseConfig("postgres://localhost:5432/mydb?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if config.ValidateConnect != nil {
		t.Fatal("expected parsed config without target_session_attrs to have no ValidateConnect")
	}

	_ = buildPGXConnector(config)

	if config.ValidateConnect == nil {
		t.Fatal("expected connector builder to set ValidateConnect")
	}
}

func TestIAMConnectorReturnsBuildAuthTokenError(t *testing.T) {
	expectedErr := errors.New("retrieve aws credentials")
	connector := &iamConnector{
		dsn: "postgres://db-user@localhost:5432/mydb?sslmode=disable",
		driver: &iamDriver{
			awsConfig: aws.Config{
				Region: "us-east-1",
				Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{}, expectedErr
				}),
			},
		},
		logger: logging.Testing(),
	}

	_, err := connector.Connect(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected BuildAuthToken error %q, got %v", expectedErr, err)
	}
}

func TestIAMConnectorParseErrorDoesNotLeakDSN(t *testing.T) {
	dsn := "postgres://db-user:super-secret@%gh/mydb?sslmode=disable"
	connector := &iamConnector{
		dsn: dsn,
		driver: &iamDriver{
			awsConfig: aws.Config{Region: "us-east-1"},
		},
		logger: logging.Testing(),
	}

	_, err := connector.Connect(context.Background())
	if err == nil {
		t.Fatal("expected parse error")
	}
	if got := err.Error(); got != "parsing dsn" {
		t.Fatalf("unexpected parse error: got %q", got)
	}
	for _, leaked := range []string{dsn, "super-secret"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("parse error leaked %q in %q", leaked, err.Error())
		}
	}
}
