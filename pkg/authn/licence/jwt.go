package licence

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

type validateTokenOptions struct {
	audience string
	subject  string
}

// ValidateTokenOption configures optional licence JWT claim checks.
type ValidateTokenOption func(*validateTokenOptions)

// WithAudience requires the licence JWT audience to contain audience.
func WithAudience(audience string) ValidateTokenOption {
	return func(options *validateTokenOptions) {
		options.audience = audience
	}
}

// WithSubject requires the licence JWT subject to equal subject.
func WithSubject(subject string) ValidateTokenOption {
	return func(options *validateTokenOptions) {
		options.subject = subject
	}
}

func getKeyFromEmbeddedPublicKey() (interface{}, error) {
	block, _ := pem.Decode([]byte(formancePublicKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode embedded Formance public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse embedded Formance public key")
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("embedded Formance public key is not RSA")
	}

	return rsaPub, nil
}

// ValidateToken validates a licence JWT against the embedded Formance public key.
//
// By default it verifies the signing method, signature, issuer, and expiration.
// Audience and subject checks can be enabled with WithAudience and WithSubject.
func ValidateToken(jwtToken string, expectedIssuer string, opts ...ValidateTokenOption) error {
	options := validateTokenOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(expectedIssuer),
	}
	if options.audience != "" {
		parserOptions = append(parserOptions, jwt.WithAudience(options.audience))
	}
	if options.subject != "" {
		parserOptions = append(parserOptions, jwt.WithSubject(options.subject))
	}

	parser := jwt.NewParser(parserOptions...)

	token, err := parser.Parse(jwtToken, func(token *jwt.Token) (interface{}, error) {
		return getKeyFromEmbeddedPublicKey()
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

func (l *Licence) validate() error {
	return ValidateToken(
		l.jwtToken,
		l.expectedIssuer,
		WithAudience(l.serviceName),
		WithSubject(l.clusterID),
	)
}
