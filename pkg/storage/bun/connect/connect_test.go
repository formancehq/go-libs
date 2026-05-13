package connect

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	got := reflect.ValueOf(config.ValidateConnect).Pointer()
	want := reflect.ValueOf(pgconn.ValidateConnectTargetSessionAttrsReadWrite).Pointer()
	if got != want {
		t.Fatalf("unexpected ValidateConnect func pointer: got %x, want %x", got, want)
	}
}
