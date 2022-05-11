package sharedauth

import (
	"context"
	"github.com/golang-jwt/jwt"
	"github.com/numary/go-libs/oauth2/oauth2introspect"
	"github.com/pkg/errors"
	"net/http"
	"strings"
)

type validator interface {
	Validate(ctx context.Context, token string) error
}

type introspectionValidator struct {
	introspecter      *oauth2introspect.Introspecter
	audiences         []string
	audiencesWildcard bool
}

func (v *introspectionValidator) Validate(ctx context.Context, token string) error {
	active, err := v.introspecter.Introspect(ctx, token)
	if err != nil {
		return err
	}
	if !active {
		return errors.New("invalid token")
	}

	if !v.audiencesWildcard {
		claims := jwt.MapClaims{}
		_, _, err := (&jwt.Parser{}).ParseUnverified(token, &claims)
		if err != nil {
			return err
		}
		for _, a := range v.audiences {
			if claims.VerifyAudience(a, true) {
				return nil
			}
		}
		return errors.New("mismatch audience")
	}
	return nil
}

func NewIntrospectionValidator(introspecter *oauth2introspect.Introspecter, audiencesWildcard bool, audiences ...string) *introspectionValidator {
	return &introspectionValidator{
		introspecter:      introspecter,
		audiences:         audiences,
		audiencesWildcard: audiencesWildcard,
	}
}

type oauth2Agent struct {
	claims jwt.MapClaims
}

func (o oauth2Agent) GetScopes() []string {
	scopeClaim, ok := o.claims["scope"]
	if !ok {
		return []string{}
	}
	scopeClaimAsString, ok := scopeClaim.(string)
	if !ok {
		return []string{}
	}
	return strings.Split(scopeClaimAsString, " ")
}

type oauth2BearerMethod struct {
	validator validator
}

func (h oauth2BearerMethod) IsMatching(c *http.Request) bool {
	return strings.HasPrefix(
		strings.ToLower(c.Header.Get("Authorization")),
		"bearer",
	)
}

func (h *oauth2BearerMethod) Check(c *http.Request) (Agent, error) {
	token := c.Header.Get("Authorization")[len("bearer "):]
	err := h.validator.Validate(c.Context(), token)
	if err != nil {
		return nil, err
	}
	claims := jwt.MapClaims{}
	_, _, err = new(jwt.Parser).ParseUnverified(token, &claims)
	if err != nil {
		return nil, err
	}
	return &oauth2Agent{
		claims: claims,
	}, nil
}

var _ Method = &oauth2BearerMethod{}

func NewHttpBearerMethod(validator validator) *oauth2BearerMethod {
	return &oauth2BearerMethod{
		validator: validator,
	}
}
