package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/formancehq/go-libs/v4/collectionutils"
	"github.com/formancehq/go-libs/v4/oidc"
)

func checkScopes(service string, method string, scopes oidc.SpaceDelimitedArray) (bool, error) {
	allowed := true //nolint:ineffassign
	switch method {
	case http.MethodOptions, http.MethodGet, http.MethodHead, http.MethodTrace:
		allowed = collectionutils.Contains(scopes, service+":read") ||
			collectionutils.Contains(scopes, service+":write")
	default:
		allowed = collectionutils.Contains(scopes, service+":write")
	}

	if !allowed {
		return false, fmt.Errorf("missing access, found scopes: '%s' need %s:read|write", strings.Join(scopes, ", "), service)
	}
	return true, nil
}
