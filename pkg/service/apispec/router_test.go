package apispec_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/formancehq/go-libs/v5/pkg/service/apispec"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDoc(t *testing.T, spec []byte) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	require.NoError(t, err)
	return doc
}

var testSpec = []byte(`
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
    post:
      operationId: createItem
      responses:
        "201":
          description: Created
  /items/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    get:
      operationId: getItem
      responses:
        "200":
          description: OK
    delete:
      operationId: deleteItem
      responses:
        "204":
          description: No Content
  /items/{id}/tags/{tag}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
      - name: tag
        in: path
        required: true
        schema:
          type: string
    get:
      operationId: getItemTag
      responses:
        "200":
          description: OK
`)

var scopeSpec = []byte(`
openapi: "3.0.0"
info:
  title: Scope Test API
  version: "1.0"
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          scopes: {}
    apikey:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /no-security:
    get:
      operationId: noSecurity
      responses:
        "200":
          description: OK
  /single-scope:
    get:
      operationId: singleScope
      security:
        - oauth2: [read:items]
      responses:
        "200":
          description: OK
  /multi-scope:
    get:
      operationId: multiScope
      security:
        - oauth2: [write:items, admin]
      responses:
        "200":
          description: OK
    post:
      operationId: postMultiScope
      security:
        - oauth2: [read:items, write:items]
      responses:
        "201":
          description: Created
  /multi-requirement:
    get:
      operationId: multiRequirement
      security:
        - oauth2: [read:items]
        - apikey: [superuser]
      responses:
        "200":
          description: OK
`)

func TestRouter_Oauth2Scopes(t *testing.T) {
	t.Parallel()

	t.Run("no security on any operation returns empty", func(t *testing.T) {
		t.Parallel()
		doc := newTestDoc(t, testSpec)
		router := apispec.NewRouter(doc)
		assert.Empty(t, router.Oauth2Scopes())
	})

	t.Run("single oauth2 scope", func(t *testing.T) {
		t.Parallel()
		spec := []byte(`
openapi: "3.0.0"
info:
  title: T
  version: "1.0"
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          scopes: {}
paths:
  /a:
    get:
      operationId: a
      security:
        - oauth2: [read:items]
      responses:
        "200":
          description: OK
`)
		doc := newTestDoc(t, spec)
		router := apispec.NewRouter(doc)
		assert.Equal(t, []string{"read:items"}, router.Oauth2Scopes())
	})

	t.Run("oauth2 scopes deduplicated and sorted, non-oauth2 excluded", func(t *testing.T) {
		t.Parallel()
		doc := newTestDoc(t, scopeSpec)
		router := apispec.NewRouter(doc)
		// superuser comes from apikey scheme and must not appear
		got := router.Oauth2Scopes()
		assert.Equal(t, []string{"admin", "read:items", "write:items"}, got)
	})

	t.Run("operation with nil security is skipped", func(t *testing.T) {
		t.Parallel()
		doc := newTestDoc(t, scopeSpec)
		router := apispec.NewRouter(doc)
		scopes := router.Oauth2Scopes()
		assert.NotContains(t, scopes, "")
	})

	t.Run("non-oauth2 security requirements excluded", func(t *testing.T) {
		t.Parallel()
		spec := []byte(`
openapi: "3.0.0"
info:
  title: T
  version: "1.0"
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          scopes: {}
    apikey:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /a:
    get:
      operationId: a
      security:
        - oauth2: [scopeA]
        - apikey: [scopeB]
      responses:
        "200":
          description: OK
`)
		doc := newTestDoc(t, spec)
		router := apispec.NewRouter(doc)
		// only scopeA from the oauth2 scheme should be returned
		assert.Equal(t, []string{"scopeA"}, router.Oauth2Scopes())
	})
}

func TestRouter_FindRoute(t *testing.T) {
	t.Parallel()

	doc := newTestDoc(t, testSpec)
	router := apispec.NewRouter(doc)

	t.Run("static path matched", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/items", nil)
		route, pathParams, err := router.FindRoute(req)
		require.NoError(t, err)
		assert.Equal(t, "/items", route.Path)
		assert.Equal(t, http.MethodGet, route.Method)
		assert.Equal(t, "listItems", route.Operation.OperationID)
		assert.Empty(t, pathParams)
	})

	t.Run("static path different method", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/items", nil)
		route, _, err := router.FindRoute(req)
		require.NoError(t, err)
		assert.Equal(t, "createItem", route.Operation.OperationID)
	})

	t.Run("path with parameter matched", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/items/42", nil)
		route, pathParams, err := router.FindRoute(req)
		require.NoError(t, err)
		assert.Equal(t, "/items/{id}", route.Path)
		assert.Equal(t, "getItem", route.Operation.OperationID)
		assert.Equal(t, map[string]string{"id": "42"}, pathParams)
	})

	t.Run("path with multiple parameters matched", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/items/42/tags/foo", nil)
		route, pathParams, err := router.FindRoute(req)
		require.NoError(t, err)
		assert.Equal(t, "/items/{id}/tags/{tag}", route.Path)
		assert.Equal(t, map[string]string{"id": "42", "tag": "foo"}, pathParams)
	})

	t.Run("path not found", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
		route, pathParams, err := router.FindRoute(req)
		assert.ErrorIs(t, err, routers.ErrPathNotFound)
		assert.Nil(t, route)
		assert.Nil(t, pathParams)
	})

	t.Run("method not allowed", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/items/42", nil)
		route, pathParams, err := router.FindRoute(req)
		assert.ErrorIs(t, err, routers.ErrMethodNotAllowed)
		assert.Nil(t, route)
		assert.Nil(t, pathParams)
	})

	t.Run("route carries correct spec and path item", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodDelete, "/items/99", nil)
		route, _, err := router.FindRoute(req)
		require.NoError(t, err)
		assert.Same(t, doc, route.Spec)
		assert.Same(t, doc.Paths.Find("/items/{id}"), route.PathItem)
		assert.Equal(t, "deleteItem", route.Operation.OperationID)
	})
}
