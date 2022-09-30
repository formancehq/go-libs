package sharedauth

import (
	"net/http"

	_ "go.formance.com/go-libs/sharedlogging/sharedlogginglogrus"
)

type Agent interface {
	GetScopes() []string
}

type Method interface {
	IsMatching(c *http.Request) bool
	Check(c *http.Request) (Agent, error)
}
