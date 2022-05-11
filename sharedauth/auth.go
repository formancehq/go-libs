package sharedauth

import (
	_ "github.com/numary/go-libs/sharedlogging/sharedlogginglogrus"
	"net/http"
)

type Agent interface {
	GetScopes() []string
}

type Method interface {
	IsMatching(c *http.Request) bool
	Check(c *http.Request) (Agent, error)
}
