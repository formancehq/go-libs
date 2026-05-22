package httpaudit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/audit"
	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestMiddleware_BasicCapture(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}),
	)

	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, `{"status":"ok"}`, rr.Body.String())

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	assert.Equal(t, "test-app", event.App)
	assert.Equal(t, audit.EventVersion, event.Version)
	assert.Equal(t, audit.EventTypeAudit, event.Type)

	payloadBytes, err := json.Marshal(event.Payload)
	require.NoError(t, err)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.NotEmpty(t, payload.ID)
	assert.Equal(t, "POST", payload.HTTP.Request.Method)
	assert.Equal(t, "/api/test", payload.HTTP.Request.Path)
	assert.Equal(t, `{"key":"value"}`, payload.HTTP.Request.Body)
	assert.Equal(t, http.StatusOK, payload.HTTP.Response.StatusCode)
	assert.Equal(t, `{"status":"ok"}`, payload.HTTP.Response.Body)
}

func TestMiddleware_AuthorizationHeaderStripped(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Empty(t, payload.HTTP.Request.Header.Get("Authorization"))
}

func TestMiddleware_SensitivePaths(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil,
		WithSensitivePaths("/api/auth/oauth/token"),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"secret"}`))
		}),
	)

	req := httptest.NewRequest("POST", "/api/auth/oauth/token", strings.NewReader(`grant_type=client_credentials`))
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, `{"access_token":"secret"}`, rr.Body.String())

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Empty(t, payload.HTTP.Response.Body)
}

func TestMiddleware_StreamRequestSkipsBodyCapture(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("stream-data"))
		}),
	)

	req := httptest.NewRequest("POST", "/api/ledger/v2/logs", strings.NewReader("stream-body"))
	req.Header.Set("Content-Type", "application/vnd.formance.ledger-stream")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Empty(t, payload.HTTP.Request.Body)
}

func TestMiddleware_OctetStreamResponseNotCaptured(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("binary-data"))
		}),
	)

	req := httptest.NewRequest("GET", "/api/download", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Empty(t, payload.HTTP.Response.Body)
}

func TestMiddleware_OrganizationAndStackID(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", []audit.Option{
		audit.WithOrganizationID("org-123"),
		audit.WithStackID("stack-456"),
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Equal(t, "org-123", payload.Actor.OrganizationID)
	assert.Equal(t, "stack-456", payload.Actor.StackID)
}

func TestMiddleware_IPAddressExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expectedIP string
	}{
		{
			name:       "X-Forwarded-For",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
			remoteAddr: "9.9.9.9:1234",
			expectedIP: "1.2.3.4",
		},
		{
			name:       "X-Real-IP",
			headers:    map[string]string{"X-Real-IP": "10.0.0.1"},
			remoteAddr: "9.9.9.9:1234",
			expectedIP: "10.0.0.1",
		},
		{
			name:       "RemoteAddr fallback",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.1:5678",
			expectedIP: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pub := publish.InMemory()
			topic := "audit-events"

			handler := Middleware(pub, topic, "test-app", nil)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			messages := pub.AllMessages()[topic]
			require.Len(t, messages, 1)

			var event publish.EventMessage
			require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

			payloadBytes, _ := json.Marshal(event.Payload)
			var payload audit.Payload
			require.NoError(t, json.Unmarshal(payloadBytes, &payload))

			assert.Equal(t, tt.expectedIP, payload.Actor.IPAddress)
		})
	}
}

func TestMiddleware_StatusCodeCapture(t *testing.T) {
	t.Parallel()

	codes := []int{http.StatusCreated, http.StatusNotFound, http.StatusInternalServerError}

	for _, code := range codes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			pub := publish.InMemory()
			topic := "audit-events"

			handler := Middleware(pub, topic, "test-app", nil)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(code)
				}),
			)

			req := httptest.NewRequest("GET", "/api/test", nil)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			messages := pub.AllMessages()[topic]
			require.Len(t, messages, 1)

			var event publish.EventMessage
			require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

			payloadBytes, _ := json.Marshal(event.Payload)
			var payload audit.Payload
			require.NoError(t, json.Unmarshal(payloadBytes, &payload))

			assert.Equal(t, code, payload.HTTP.Response.StatusCode)
		})
	}
}

func TestMiddleware_RequestBodyPassedThrough(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var receivedBody string
	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			receivedBody = string(body)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, `{"hello":"world"}`, receivedBody)
}

func TestMiddleware_NoAuthByDefault(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Nil(t, payload.Actor.Claims)
	assert.Empty(t, payload.Actor.TokenValidationError)
}

func TestMiddleware_SkipsWhenHeaderPresent(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set(audit.HandledHeader, "true")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())

	messages := pub.AllMessages()[topic]
	assert.Empty(t, messages)
}

func TestMiddleware_SetsHeaderForDownstream(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var downstreamHeaderValue string
	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downstreamHeaderValue = r.Header.Get(audit.HandledHeader)
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "true", downstreamHeaderValue)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
}

func TestMiddleware_HeaderNotInAuditPayload(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

	payloadBytes, _ := json.Marshal(event.Payload)
	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))

	assert.Empty(t, payload.HTTP.Request.Header.Get(audit.HandledHeader))
}
