package auth

import (
	"github.com/golang-jwt/jwt"
	"github.com/numary/go-libs/oauth2/oauth2introspect"
	"github.com/pkg/errors"
	"net/http"
	"strings"
)

type oauth2BearerMethod struct {
	introspecter      *oauth2introspect.Introspecter
	audiences         []string
	audiencesWildcard bool
}

func (h oauth2BearerMethod) IsMatching(c *http.Request) bool {
	return strings.HasPrefix(
		strings.ToLower(c.Header.Get("Authorization")),
		"bearer",
	)
}

func (h *oauth2BearerMethod) Check(c *http.Request) error {
	token := c.Header.Get("Authorization")[len("bearer "):]
	active, err := h.introspecter.Introspect(c.Context(), token)
	if err != nil {
		return err
	}
	if !active {
		return errors.New("invalid token")
	}

	if !h.audiencesWildcard {
		claims := jwt.MapClaims{}
		_, _, err := (&jwt.Parser{}).ParseUnverified(token, &claims)
		if err != nil {
			return err
		}
		for _, a := range h.audiences {
			if claims.VerifyAudience(a, true) {
				return nil
			}
		}
		return errors.New("mismatch audience")
	}

	return nil
}

var _ Method = &oauth2BearerMethod{}

func NewHttpBearerMethod(introspecter *oauth2introspect.Introspecter, audiencesWildcard bool, audiences ...string) *oauth2BearerMethod {
	return &oauth2BearerMethod{
		introspecter:      introspecter,
		audiences:         audiences,
		audiencesWildcard: audiencesWildcard,
	}
}
