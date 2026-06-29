package connect

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"

	otlp "github.com/formancehq/go-libs/v5/pkg/observe"
)

// BuildIAMAuthToken returns a fresh AWS RDS IAM authentication token that
// can be used as the password on a PostgreSQL connection to an
// IAM-authenticated RDS instance. The returned token is a SigV4-presigned URL
// with a 15-minute lifetime, so callers that hold a pool of connections must
// mint a new token per fresh connection (e.g. via pgxpool.Config.BeforeConnect).
//
// endpoint is the "host:port" the RDS instance answers on; user is the IAM
// database user. Ambient AWS credentials must be supplied via awsConfig (which
// is normally produced by aws-sdk-go-v2/config.LoadDefaultConfig and pulls
// from IRSA, instance profile, env vars, or profile).
//
// Errors from auth.BuildAuthToken are recorded on the active OTel span and
// returned unchanged so the caller can wrap them with their own context.
func BuildIAMAuthToken(ctx context.Context, awsConfig aws.Config, endpoint, user string) (string, error) {
	ctx, span := tracer.Start(ctx, "iam.build-auth-token")
	defer span.End()

	token, err := auth.BuildAuthToken(ctx, endpoint, awsConfig.Region, user, awsConfig.Credentials)
	if err != nil {
		otlp.RecordError(ctx, err)
		return "", err
	}
	return token, nil
}
