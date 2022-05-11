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
					_, err := m.Check(r)
					if err != nil {
						sharedlogging.GetLogger(r.Context()).WithFields(map[string]interface{}{
							"err": err,
						}).Infof("Access denied")
						w.WriteHeader(http.StatusForbidden)
						return
					}
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
