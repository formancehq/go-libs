package apispec

import (
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	chi "github.com/go-chi/chi/v5"
)

// implements routers.Router using chi for path matching
// OpenAPI path templates use {param} syntax which is directly compatible with chi
type Router struct {
	mux        *chi.Mux
	doc        *openapi3.T
	operations map[string]map[string]*openapi3.Operation // openapi path → METHOD → operation
}

// NewRouter builds a Router from a parsed OpenAPI document
// gives programatic access to documented info such as oauth2 scopes
func NewRouter(doc *openapi3.T) *Router {
	mux := chi.NewRouter()
	ops := map[string]map[string]*openapi3.Operation{}

	for path, item := range doc.Paths.Map() {
		ops[path] = map[string]*openapi3.Operation{}
		for method, op := range item.Operations() {
			method = strings.ToUpper(method)
			ops[path][method] = op
			mux.Method(method, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		}
	}

	return &Router{mux: mux, doc: doc, operations: ops}
}

// allMethods lists the HTTP methods registered against the chi Mux so that
// FindRoute can distinguish "path not found" from "method not allowed"
var allMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodHead,
	http.MethodOptions,
}

// FindRoute implements routers.Router interface
func (r *Router) FindRoute(req *http.Request) (*routers.Route, map[string]string, error) {
	rctx := chi.NewRouteContext()
	if !r.mux.Match(rctx, req.Method, req.URL.Path) {
		// Determine whether the path itself exists under a different method
		for _, m := range allMethods {
			if m == req.Method {
				continue
			}
			if r.mux.Match(chi.NewRouteContext(), m, req.URL.Path) {
				return nil, nil, routers.ErrMethodNotAllowed
			}
		}
		return nil, nil, routers.ErrPathNotFound
	}

	pattern := rctx.RoutePattern()
	pathOps, ok := r.operations[pattern]
	if !ok {
		return nil, nil, routers.ErrPathNotFound
	}
	op, ok := pathOps[req.Method]
	if !ok {
		return nil, nil, routers.ErrMethodNotAllowed
	}

	pathParams := make(map[string]string, len(rctx.URLParams.Keys))
	for i, key := range rctx.URLParams.Keys {
		pathParams[key] = rctx.URLParams.Values[i]
	}

	return &routers.Route{
		Spec:      r.doc,
		Path:      pattern,
		PathItem:  r.doc.Paths.Find(pattern),
		Method:    req.Method,
		Operation: op,
	}, pathParams, nil
}

func (r *Router) Oauth2Scopes() []string {
	seen := map[string]struct{}{}
	var result []string
	for _, item := range r.doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op == nil || op.Security == nil {
				continue
			}
			for _, secReq := range *op.Security {
				for schemeName, scopeNames := range secReq {
					ref, ok := r.doc.Components.SecuritySchemes[schemeName]
					if !ok || ref.Value == nil || ref.Value.Type != "oauth2" {
						continue
					}
					for _, s := range scopeNames {
						if _, ok := seen[s]; !ok {
							seen[s] = struct{}{}
							result = append(result, s)
						}
					}
				}
			}
		}
	}
	sort.Strings(result)
	return result
}

var _ routers.Router = (*Router)(nil)
