package bunconnect

import (
	"testing"
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
