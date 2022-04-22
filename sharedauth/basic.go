package sharedauth

import (
	"github.com/pkg/errors"
	"net/http"
	"strings"
)

type httpBasicMethod struct {
	credentials map[string]string
}

func (h httpBasicMethod) IsMatching(c *http.Request) bool {
	return strings.HasPrefix(
		strings.ToLower(c.Header.Get("Authorization")),
		"basic",
	)
}

func (h httpBasicMethod) Check(c *http.Request) error {
	username, password, ok := c.BasicAuth()
	if !ok {
		return errors.New("malformed basic")
	}
	if username == "" {
		return errors.New("malformed basic")
	}
	if h.credentials[username] != password {
		return errors.New("invalid credentials")
	}
	return nil
}

func NewHTTPBasicMethod(credentials map[string]string) *httpBasicMethod {
	return &httpBasicMethod{
		credentials: credentials,
	}
}

var _ Method = &httpBasicMethod{}
