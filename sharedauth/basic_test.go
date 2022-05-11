package sharedauth

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHttpBasic(t *testing.T) {

	m := Middleware(NewHTTPBasicMethod(Credentials{
		"foo": {
			Password: "bar",
			Scopes:   []string{"scope1"},
		},
	}))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := AgentFromContext(r.Context())
		assert.NotNil(t, agent)
		assert.Equal(t, []string{"scope1"}, agent.GetScopes())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("foo", "bar")

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Result().StatusCode)
}

func TestHttpBasicForbidden(t *testing.T) {

	m := Middleware(NewHTTPBasicMethod(Credentials{
		"foo": {
			Password: "bar",
		},
	}))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("foo", "baz")

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Result().StatusCode)
}
