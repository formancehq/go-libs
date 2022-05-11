package sharedauth

import (
	"github.com/numary/go-libs/sharedlogging"
	"net/http"
)

func Middleware(methods ...Method) func(handler http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok := false
			for _, m := range methods {
				if m.IsMatching(r) {
					agent, err := m.Check(r)
					if err != nil {
						sharedlogging.GetLogger(r.Context()).WithFields(map[string]interface{}{
							"err": err,
						}).Infof("Access denied")
						w.WriteHeader(http.StatusForbidden)
						return
					}
					r = r.WithContext(WithAgent(r.Context(), agent))
					ok = true
					break
				}
			}
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, r)
		})

	}
}

func NeedScopes(scopes ...string) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agent := AgentFromContext(r.Context())
			if agent == nil {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		l:
			for _, scope := range scopes {
				for _, agentScope := range agent.GetScopes() {
					if agentScope == scope {
						continue l
					}
				}
				// Scope not found
				w.WriteHeader(http.StatusForbidden)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}
