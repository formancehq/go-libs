package auth

import (
	"github.com/golang-jwt/jwt"
	"github.com/numary/go-libs/oauth2/oauth2introspect"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

func forgeToken(t *testing.T, audience string) string {
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"aud": audience,
	}).SignedString([]byte("0000000000000000"))
	assert.NoError(t, err)
	return tok
}

func TestHttpBearerWithWildcardOnAudiences(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active": true}`))
	}))
	defer srv.Close()

	i := oauth2introspect.NewIntrospecter(srv.URL)
	m := Middleware(NewHttpBearerMethod(i, true))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/foo", nil)
	req.Header.Set("Authorization", "Bearer "+forgeToken(t, "foo"))

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Result().StatusCode)
}

func TestHttpBearerWithValidAudience(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active": true}`))
	}))
	defer srv.Close()

	i := oauth2introspect.NewIntrospecter(srv.URL)
	m := Middleware(NewHttpBearerMethod(i, false, "http://example.net"))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/foo", nil)
	req.Header.Set("Authorization", "Bearer "+forgeToken(t, "http://example.net"))

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Result().StatusCode)
}

func TestHttpBearerWithInvalidToken(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := Middleware(NewHttpBearerMethod(oauth2introspect.NewIntrospecter(srv.URL), true))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/XXX", nil)
	req.Header.Set("Authorization", "Bearer XXX")

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Result().StatusCode)
}

func TestHttpBearerForbiddenWithWrongAudience(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active": true}`))
	}))
	defer srv.Close()

	m := Middleware(NewHttpBearerMethod(oauth2introspect.NewIntrospecter(srv.URL), false, "http://example.net"))
	h := m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/foo", nil)
	req.Header.Set("Authorization", "Bearer "+forgeToken(t, "http://external.net"))

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Result().StatusCode)
}
