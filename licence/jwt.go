package licence

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

func (l *Licence) getKeyFromEmbeddedPublicKey() (interface{}, error) {
	block, _ := pem.Decode([]byte(formancePublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode embedded Formance public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse embedded Formance public key")
	}

	return pub, nil
}

func (l *Licence) validate() error {
	parser := jwt.NewParser(
		jwt.WithAudience(l.serviceName),
		jwt.WithExpirationRequired(),
		jwt.WithSubject(l.clusterID),
		jwt.WithIssuer(l.expectedIssuer),
	)

	token, err := parser.Parse(l.jwtToken, func(token *jwt.Token) (interface{}, error) {
		return l.getKeyFromEmbeddedPublicKey()
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return fmt.Errorf("token has invalid claims: token is expired")
		}
		return err
	}

	if !token.Valid {
		return fmt.Errorf("token is not valid")
	}

	return nil
}
