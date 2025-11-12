package licence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/logging"
)

// Mock logger
type mockLogger struct {
	mu          sync.Mutex
	messages    []string
	lastMessage string
	lastError   error
}

func (m *mockLogger) Info(args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = args[0].(string)
	m.messages = append(m.messages, m.lastMessage)
}

func (m *mockLogger) Infof(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = fmt.Sprintf(format, args...)
	m.messages = append(m.messages, m.lastMessage)
}

func (m *mockLogger) Error(args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = args[0].(string)
	if len(args) > 1 {
		m.lastError = args[1].(error)
	}
	m.messages = append(m.messages, m.lastMessage)
}

func (m *mockLogger) Errorf(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMessage = fmt.Sprintf(format, args...)
	m.messages = append(m.messages, m.lastMessage)
}

func (m *mockLogger) WithFields(fields map[string]any) logging.Logger { return m }
func (m *mockLogger) WithField(key string, value any) logging.Logger  { return m }
func (m *mockLogger) WithContext(ctx context.Context) logging.Logger  { return m }
func (m *mockLogger) Writer() io.Writer                               { return io.Discard }
func (m *mockLogger) Warn(args ...any)                                {}
func (m *mockLogger) Warnf(format string, args ...any)                {}
func (m *mockLogger) Debug(args ...any)                               {}
func (m *mockLogger) Debugf(format string, args ...any)               {}

func (m *mockLogger) getMessage() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMessage
}

// Helper function to create a JWT token
func createToken(t *testing.T, claims jwt.MapClaims, kid string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	tokenString, err := token.SignedString([]byte("test-key"))
	require.NoError(t, err)
	return tokenString
}

// Helper function to create a test JWK server
func createTestJWKServer(t *testing.T) *httptest.Server {
	key := jwk.NewSymmetricKey()
	err := key.FromRaw([]byte("test-key"))
	require.NoError(t, err)

	err = key.Set(jwk.KeyIDKey, "test-kid")
	require.NoError(t, err)
	err = key.Set(jwk.AlgorithmKey, jwa.HS256)
	require.NoError(t, err)

	set := jwk.NewSet()
	set.Add(key)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(set)
	}))

	return server
}

func TestNewLicence(t *testing.T) {
	logger := &mockLogger{}
	licence := NewLicence(
		logger,
		"test-token",
		2*time.Minute,
		"test-service",
		"test-cluster",
		"test-issuer",
	)

	require.NotNil(t, licence)
	require.Equal(t, "test-token", licence.jwtToken)
	require.Equal(t, 2*time.Minute, licence.licenceValidateTick)
	require.Equal(t, "test-service", licence.serviceName)
	require.Equal(t, "test-cluster", licence.clusterID)
	require.Equal(t, "test-issuer", licence.expectedIssuer)
	require.NotNil(t, licence.appStoped)
}

func TestLicence_Start(t *testing.T) {
	server := createTestJWKServer(t)
	defer server.Close()

	t.Run("invalid token", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"invalid-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		licenceError := make(chan error, 1)
		err := licence.Start(licenceError)
		require.Error(t, err)
		require.Equal(t, "Licence check failed token is malformed: token contains an invalid number of segments", logger.getMessage())
	})

	t.Run("successful start", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		licenceError := make(chan error, 1)
		err := licence.Start(licenceError)
		require.NoError(t, err)
		require.Equal(t, "Licence check passed", logger.getMessage())

		licence.Stop()
	})

	t.Run("stop licence check", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		licenceError := make(chan error, 1)
		err := licence.Start(licenceError)
		require.NoError(t, err)
		require.Equal(t, "Licence check passed", logger.getMessage())

		licence.Stop()
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, "Licence check stopped, app stopped", logger.getMessage())
	})
}

func TestLicence_validate(t *testing.T) {
	server := createTestJWKServer(t)
	defer server.Close()

	t.Run("invalid token format", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"invalid-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Equal(t, "token is malformed: token contains an invalid number of segments", err.Error())
	})

	t.Run("missing kid", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("test-key"))
		require.NoError(t, err)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err = licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing kid")
	})

	t.Run("missing issuer", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing issuer")
	})

	t.Run("invalid issuer", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "invalid-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to fetch remote JWK")
	})

	t.Run("invalid key", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		token.Header["kid"] = "test-kid"
		tokenString, err := token.SignedString([]byte("wrong-key"))
		require.NoError(t, err)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err = licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "signature is invalid")
	})

	t.Run("invalid audience", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "invalid-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid audience")
	})

	t.Run("invalid subject", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "invalid-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid subject")
	})

	t.Run("expired token", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(-time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid claims: token is expired")
	})

	t.Run("valid token", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		err := licence.validate()
		require.NoError(t, err)
	})
}

func TestLicence_getKey(t *testing.T) {
	server := createTestJWKServer(t)
	defer server.Close()

	t.Run("invalid issuer", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"test-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		key, err := licence.getKey("invalid-issuer", "test-kid")
		require.Error(t, err)
		require.Nil(t, key)
		require.Contains(t, err.Error(), "failed to fetch remote JWK")
	})

	t.Run("missing key", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"test-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		key, err := licence.getKey(server.URL, "missing-kid")
		require.Error(t, err)
		require.Nil(t, key)
		require.Contains(t, err.Error(), "key not found")
	})

	t.Run("valid key", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"test-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			server.URL,
		)

		key, err := licence.getKey(server.URL, "test-kid")
		require.NoError(t, err)
		require.NotNil(t, key)
	})
}

func TestLicence_run(t *testing.T) {
	server := createTestJWKServer(t)
	defer server.Close()

	t.Run("stop on app stop", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": server.URL,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createToken(t, claims, "test-kid")

		licence := NewLicence(
			logger,
			tokenString,
			100*time.Millisecond,
			"test-service",
			"test-cluster",
			server.URL,
		)

		licenceError := make(chan error, 1)
		err := licence.Start(licenceError)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)
		licence.Stop()
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, "Licence check stopped, app stopped", logger.getMessage())
	})
}
