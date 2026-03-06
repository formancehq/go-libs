package publish

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
)

type MSKAccessTokenProvider struct {
	Region      string
	RoleArn     string
	SessionName string
}

func (m *MSKAccessTokenProvider) Token() (*sarama.AccessToken, error) {
	token, _, err := signer.GenerateAuthTokenFromRole(context.TODO(), m.Region, m.RoleArn, m.SessionName)
	return &sarama.AccessToken{Token: token}, err
}
