package connect

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/require"
)

func TestBuildIAMAuthToken_ProducesValidSigV4PresignedURL(t *testing.T) {
	t.Parallel()

	awsCfg := aws.Config{
		Region:      "eu-west-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIATESTACCESSKEY", "SECRETTESTKEY", ""),
	}

	token, err := BuildIAMAuthToken(context.Background(), awsCfg, "db.example.com:5432", "iam-user")
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
