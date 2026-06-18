package connect

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/xo/dburl"
)

func TestBuildIAMAuthDSNEscapesAuthTokenAndQueryValues(t *testing.T) {
	databaseURL, err := dburl.Parse("postgres://iam%20user:old%20password@db.example.com:5432/service%20db?sslmode=require&application_name=worker%20one%2Bblue%26green%3Dtrue&search_path=tenant%3Done%2Cpublic")
	if err != nil {
		t.Fatal(err)
	}
	token := "db.example.com:5432/?Action=connect&DBUser=iam user&X-Amz-Credential=AKIA/20260614/eu-west-1/rds-db/aws4_request&X-Amz-Signature=a+b=c=="

	dsn := buildIAMAuthDSN(&databaseURL.URL, token)
	if strings.Contains(dsn, token) {
		t.Fatalf("expected auth token to be URL-escaped, got raw token in %q", dsn)
	}

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("failed to parse generated IAM DSN: %v", err)
	}

	if config.User != "iam user" {
		t.Fatalf("unexpected user: got %q", config.User)
	}
	if config.Password != token {
		t.Fatalf("unexpected password token: got %q, want %q", config.Password, token)
	}
	if config.Database != "service db" {
		t.Fatalf("unexpected database: got %q", config.Database)
	}
	if got := config.RuntimeParams["application_name"]; got != "worker one+blue&green=true" {
		t.Fatalf("unexpected application_name: got %q", got)
	}
	if got := config.RuntimeParams["search_path"]; got != "tenant=one,public" {
		t.Fatalf("unexpected search_path: got %q", got)
	}
}

func TestBuildIAMAuthDSNObfuscatesAuthToken(t *testing.T) {
	databaseURL, err := dburl.Parse("postgres://iam-user@db.example.com:5432/app?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	token := "db.example.com:5432/?Action=connect&DBUser=iam-user&X-Amz-Credential=AKIA/20260614/eu-west-1/rds-db/aws4_request&X-Amz-Signature=a+b=c=="

	loggedDSN := obfuscateDSN(buildIAMAuthDSN(&databaseURL.URL, token))
	if loggedDSN != "postgres://iam-user:%2A%2A%2A%2A@db.example.com:5432/app?sslmode=disable" {
		t.Fatalf("unexpected obfuscated DSN: got %q", loggedDSN)
	}

	for _, sensitive := range []string{token, "X-Amz-Credential", "X-Amz-Signature", "AKIA"} {
		if strings.Contains(loggedDSN, sensitive) {
			t.Fatalf("obfuscated DSN leaked %q in %q", sensitive, loggedDSN)
		}
	}
}
