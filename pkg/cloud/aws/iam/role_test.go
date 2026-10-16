package iam

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
)

type roleTransport func(*http.Request) (*http.Response, error)

func (f roleTransport) Do(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the real SDK credential providers; the STS wire is synthetic and never
// reaches AWS. Loading configuration alone does not prove either role can mint.
func TestLoadConfigRetainsWorkloadAndProfileRoleSelection(t *testing.T) {
	for _, kind := range []string{"workload", "profile"} {
		t.Run(kind, func(t *testing.T) {
			for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN", "AWS_ROLE_SESSION_NAME"} {
				t.Setenv(key, "")
			}
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			const role = "arn:aws:iam::000000000000:role/synthetic"
			options := Options{Region: "us-east-1"}
			files, creds := []string{}, []string{}
			action := "AssumeRoleWithWebIdentity"
			if kind == "workload" {
				tokenFile := filepath.Join(t.TempDir(), "identity")
				if err := os.WriteFile(tokenFile, []byte("synthetic-identity"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("AWS_ROLE_ARN", role)
				t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", tokenFile)
			} else {
				options.Profile = "selected-role"
				action = "AssumeRole"
				file := filepath.Join(t.TempDir(), "config")
				if err := os.WriteFile(file, []byte("[profile selected-role]\nrole_arn="+role+"\nsource_profile=base\n[profile base]\naws_access_key_id=synthetic-base\naws_secret_access_key=synthetic-base-secret\n"), 0600); err != nil {
					t.Fatal(err)
				}
				files = []string{file}
			}
			calls := 0
			transport := roleTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				values, err := url.ParseQuery(string(raw))
				if err != nil {
					return nil, err
				}
				if values.Get("Action") != action || values.Get("RoleArn") != role {
					return nil, fmt.Errorf("incorrect STS role selection")
				}
				if kind == "workload" && values.Get("WebIdentityToken") != "synthetic-identity" {
					return nil, fmt.Errorf("identity file was not used")
				}
				if kind == "profile" && !strings.Contains(r.Header.Get("Authorization"), "Credential=synthetic-base/") {
					return nil, fmt.Errorf("source profile was not used")
				}
				body := fmt.Sprintf(`<%sResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><%sResult><Credentials><AccessKeyId>synthetic-role</AccessKeyId><SecretAccessKey>synthetic-secret</SecretAccessKey><SessionToken>synthetic-session</SessionToken><Expiration>2100-01-01T00:00:00Z</Expiration></Credentials></%sResult><ResponseMetadata><RequestId>synthetic</RequestId></ResponseMetadata></%sResponse>`, action, action, action, action)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			cfg, err := LoadConfig(t.Context(), options, config.WithHTTPClient(transport), config.WithSharedConfigFiles(files), config.WithSharedCredentialsFiles(creds))
			if err != nil {
				t.Fatal(err)
			}
			minted, err := cfg.Credentials.Retrieve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || minted.AccessKeyID != "synthetic-role" || minted.SessionToken != "synthetic-session" {
				t.Fatal("role credentials were not retrieved")
			}
		})
	}
}
