package licence

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
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

// setEmbeddedKey temporarily overrides the package-level formancePublicKey for testing.
func setEmbeddedKey(t *testing.T, key string) {
	t.Helper()
	original := formancePublicKey
	formancePublicKey = key
	t.Cleanup(func() { formancePublicKey = original })
}

// generateTestRSAKeyPair creates an RSA key pair and returns the private key
// and the PEM-encoded public key. Uses RSA to match production (RS256).
func generateTestRSAKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	return privateKey, string(pubPEM)
}

func createTokenWithRSAKey(t *testing.T, claims jwt.MapClaims, privateKey *rsa.PrivateKey) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return tokenString
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
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setEmbeddedKey(t, pubPEM)

	t.Run("invalid token", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"invalid-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
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
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
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
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
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
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setEmbeddedKey(t, pubPEM)

	t.Run("invalid token format", func(t *testing.T) {
		logger := &mockLogger{}
		licence := NewLicence(
			logger,
			"invalid-token",
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.Error(t, err)
		require.Equal(t, "token is malformed: token contains an invalid number of segments", err.Error())
	})

	t.Run("invalid audience", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "invalid-service",
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
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
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid subject")
	})

	t.Run("invalid issuer", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "wrong-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid issuer")
	})

	t.Run("expired token", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "test-issuer",
			"exp": time.Now().Add(-time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "token has invalid claims: token is expired")
	})

	t.Run("wrong signing key", func(t *testing.T) {
		otherKey, _ := generateTestRSAKeyPair(t)
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, otherKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "verification error")
	})

	t.Run("valid token", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			2*time.Minute,
			"test-service",
			"test-cluster",
			"test-issuer",
		)

		err := licence.validate()
		require.NoError(t, err)
	})
}

func TestLicence_validate_with_production_key(t *testing.T) {
	// This test does NOT override formancePublicKey — it exercises the actual
	// embedded production RSA key from public_key.go to catch parsing regressions.
	// The token is signed with a random key, so validation must fail with a
	// verification error (not a key parsing error).
	privateKey, _ := generateTestRSAKeyPair(t)
	claims := jwt.MapClaims{
		"sub": "test-cluster",
		"aud": "test-service",
		"iss": "test-issuer",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tokenString := createTokenWithRSAKey(t, claims, privateKey)

	logger := &mockLogger{}
	licence := NewLicence(
		logger,
		tokenString,
		2*time.Minute,
		"test-service",
		"test-cluster",
		"test-issuer",
	)

	err := licence.validate()
	require.Error(t, err)
	// The production key must parse successfully — the error should be about
	// signature verification, not about key decoding/parsing.
	require.Contains(t, err.Error(), "verification error")
}

func TestLicence_run(t *testing.T) {
	privateKey, pubPEM := generateTestRSAKeyPair(t)
	setEmbeddedKey(t, pubPEM)

	t.Run("stop on app stop", func(t *testing.T) {
		logger := &mockLogger{}
		claims := jwt.MapClaims{
			"sub": "test-cluster",
			"aud": "test-service",
			"iss": "test-issuer",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		tokenString := createTokenWithRSAKey(t, claims, privateKey)

		licence := NewLicence(
			logger,
			tokenString,
			100*time.Millisecond,
			"test-service",
			"test-cluster",
			"test-issuer",
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
