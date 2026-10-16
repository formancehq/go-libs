package iam

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/pflag"
)

func TestLoadConfigRejectsPartialCredentials(t *testing.T) {
	for _, opts := range []Options{{AccessKeyID: "key"}, {SecretAccessKey: "secret"}, {SessionToken: "session"}, {AccessKeyID: " ", SecretAccessKey: "secret"}} {
		called := false
		_, err := LoadConfig(t.Context(), opts, func(*config.LoadOptions) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("partial credentials reached SDK configuration")
		}
	}
}
func TestLoadConfigUsesExplicitStaticCredentials(t *testing.T) {
	cfg, err := LoadConfig(t.Context(), Options{Region: " region ", AccessKeyID: " key ", SecretAccessKey: " secret ", SessionToken: "session"}, config.WithSharedConfigFiles([]string{}), config.WithSharedCredentialsFiles([]string{}))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := cfg.Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "region" || credentials.AccessKeyID != "key" || credentials.SecretAccessKey != "secret" || credentials.SessionToken != "session" {
		t.Fatal("explicit configuration changed")
	}
}

func TestLoadConfigRetainsDefaultEnvironmentChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	cfg, err := LoadConfig(t.Context(), Options{}, config.WithSharedConfigFiles([]string{}), config.WithSharedCredentialsFiles([]string{}))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := cfg.Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKeyID != "synthetic-key" || credentials.SecretAccessKey != "synthetic-secret" {
		t.Fatal("default chain was replaced")
	}
}

func TestLoadConfigNamedProfileAndOptionFailure(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("[test-profile]\naws_access_key_id=synthetic-profile\naws_secret_access_key=synthetic-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(t.Context(), Options{Profile: " test-profile "}, config.WithSharedConfigFiles([]string{}), config.WithSharedCredentialsFiles([]string{path}))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := cfg.Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if credentials.AccessKeyID != "synthetic-profile" {
		t.Fatal("named profile ignored")
	}
	sentinel := errors.New("option failure")
	if _, err := LoadConfig(t.Context(), Options{}, func(*config.LoadOptions) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("option error not preserved")
	}
}
func TestExistingFlagOptionsRemainIndependent(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFlags(flags)
	if err := flags.Parse([]string{"--aws-region=test-region", "--aws-profile=test-profile", "--aws-access-key-id=synthetic-key", "--aws-secret-access-key=synthetic-secret", "--aws-session-token=synthetic-session"}); err != nil {
		t.Fatal(err)
	}
	var options config.LoadOptions
	if err := LoadOptionFromFlags(flags)(&options); err != nil {
		t.Fatal(err)
	}
	credentials, err := options.Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if options.Region != "test-region" || options.SharedConfigProfile != "test-profile" || credentials.SessionToken != "synthetic-session" {
		t.Fatal("legacy flags changed")
	}
}
