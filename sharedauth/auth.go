package sharedauth

import (
	"net/http"

	_ "github.com/formancehq/go-libs/v2/sharedlogging/sharedlogginglogrus"
)

type Agent interface {
	GetScopes() []string
}

type Method interface {
	IsMatching(c *http.Request) bool
	Check(c *http.Request) (Agent, error)
}
