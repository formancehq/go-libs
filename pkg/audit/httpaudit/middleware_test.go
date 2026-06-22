package httpaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v5/pkg/audit"
	"github.com/formancehq/go-libs/v5/pkg/messaging/publish"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func TestMiddleware_DisabledByDefault(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var downstreamHeaderValue string
	handler := Middleware(pub, topic, "test-app", nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downstreamHeaderValue = r.Header.Get(audit.HandledHeader)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	)

	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"key":"value"}`))
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())
	assert.Empty(t, downstreamHeaderValue)
	assert.Empty(t, pub.AllMessages()[topic])
}

func TestMiddleware_WithConfigFromFlagsEnablesAudit(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	audit.AddFlags(flags)
	require.NoError(t, flags.Set(audit.AuditEnabledFlag, "true"))

	pub := publish.InMemory()
	topic := "audit-events"

	cfg, err := audit.ConfigFromFlags(flags)
	require.NoError(t, err)

	handler := Middleware(pub, topic, "test-app", nil, WithConfig(cfg))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, pub.AllMessages()[topic], 1)
}

func TestMiddleware_BasicCapture(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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
	assert.False(t, payload.HTTP.Request.BodyTruncated)
	assert.False(t, payload.HTTP.Response.BodyTruncated)
}

func TestMiddleware_QueryParamsCapture(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test?limit=10&status=pending&status=posted&empty=", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Equal(t, "/api/test", payload.HTTP.Request.Path)
	assert.Equal(t, url.Values{
		"limit":  {"10"},
		"status": {"pending", "posted"},
		"empty":  {""},
	}, payload.HTTP.Request.QueryParams)
	assert.False(t, payload.HTTP.Request.QueryParamsTruncated)
}

func TestMiddleware_CapsQueryParamsAndFlagsTruncation(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	const limit = 24
	handler := Middleware(pub, topic, "test-app", nil,
		WithEnabled(true),
		WithMaxQueryParamsBytes(limit),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test?first=one&large="+strings.Repeat("x", 100)+"&last=ignored", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Equal(t, url.Values{
		"first": {"one"},
		"large": {strings.Repeat("x", 8)},
	}, payload.HTTP.Request.QueryParams)
	assert.True(t, payload.HTTP.Request.QueryParamsTruncated)
	assert.NotContains(t, string(messages[0].Payload), "last")
	assert.NotContains(t, string(messages[0].Payload), strings.Repeat("x", 20))
}

func TestMiddleware_CapsRequestBodyAndFlagsTruncation(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var received string
	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			received = string(b)
			w.WriteHeader(http.StatusOK)
		}),
	)

	prefix := strings.Repeat("a", DefaultMaxCapturedBodyBytes)
	reqBody := prefix + strings.Repeat("b", 1024)
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	// The handler still sees the full request body.
	assert.Equal(t, reqBody, received)

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Len(t, payload.HTTP.Request.Body, DefaultMaxCapturedBodyBytes)
	assert.Equal(t, prefix, payload.HTTP.Request.Body)
	assert.True(t, payload.HTTP.Request.BodyTruncated)
}

func TestMiddleware_CapsResponseBodyAndFlagsTruncation(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	prefix := strings.Repeat("r", DefaultMaxCapturedBodyBytes)
	responseBody := prefix + strings.Repeat("s", 1024)
	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Write across the cap boundary in two chunks.
			_, _ = w.Write([]byte(responseBody[:DefaultMaxCapturedBodyBytes/2]))
			_, _ = w.Write([]byte(responseBody[DefaultMaxCapturedBodyBytes/2:]))
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	// The client still receives the full response body.
	assert.Equal(t, responseBody, rr.Body.String())

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Len(t, payload.HTTP.Response.Body, DefaultMaxCapturedBodyBytes)
	assert.Equal(t, prefix, payload.HTTP.Response.Body)
	assert.True(t, payload.HTTP.Response.BodyTruncated)
}

func TestMiddleware_WithMaxBodyBytesOverridesDefault(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	const limit = 16
	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true), WithMaxBodyBytes(limit))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("z", 100)))
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, strings.Repeat("z", 100), rr.Body.String())

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Len(t, payload.HTTP.Response.Body, limit)
	assert.True(t, payload.HTTP.Response.BodyTruncated)
}

func TestMiddleware_DoesNotPoolOversizedCaptureBuffers(t *testing.T) {
	t.Parallel()

	assert.True(t, shouldPoolCaptureBuffer(bytes.NewBuffer(make([]byte, 0, DefaultMaxCapturedBodyBytes))))
	assert.False(t, shouldPoolCaptureBuffer(bytes.NewBuffer(make([]byte, 0, DefaultMaxCapturedBodyBytes+1))))
}

func auditPayloadFromMessage(t *testing.T, msg *message.Message) audit.Payload {
	t.Helper()

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(msg.Payload, &event))

	payloadBytes, err := json.Marshal(event.Payload)
	require.NoError(t, err)

	var payload audit.Payload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))
	return payload
}

func TestMiddleware_AuthorizationHeaderStripped(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

func TestMiddleware_CookieHeadersStripped(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Response-ID", "response-id")
			http.SetCookie(w, &http.Cookie{
				Name:  "session",
				Value: "response-secret",
			})
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Cookie", "session=request-secret")
	req.Header.Set("X-Request-ID", "request-id")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Contains(t, rr.Header().Get("Set-Cookie"), "response-secret")

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Empty(t, payload.HTTP.Request.Header.Get("Cookie"))
	assert.Equal(t, "request-id", payload.HTTP.Request.Header.Get("X-Request-ID"))
	assert.Empty(t, payload.HTTP.Response.Headers.Get("Set-Cookie"))
	assert.Equal(t, "response-id", payload.HTTP.Response.Headers.Get("X-Response-ID"))
	assert.NotContains(t, string(messages[0].Payload), "request-secret")
	assert.NotContains(t, string(messages[0].Payload), "response-secret")
}

func TestMiddleware_SensitivePaths(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil,
		WithEnabled(true),
		WithSensitivePaths("/api/auth/oauth/token"),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"secret"}`))
		}),
	)

	req := httptest.NewRequest("POST", "/api/auth/oauth/token?client_secret=query-secret", strings.NewReader(`grant_type=client_credentials`))
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
	assert.Empty(t, payload.HTTP.Request.Body)
	assert.Empty(t, payload.HTTP.Request.QueryParams)
	assert.False(t, payload.HTTP.Request.QueryParamsTruncated)
	assert.NotContains(t, string(messages[0].Payload), "client_credentials")
	assert.NotContains(t, string(messages[0].Payload), "access_token")
	assert.NotContains(t, string(messages[0].Payload), "query-secret")
}

func TestMiddleware_SensitivePathsMatchPrefixAndPassRequestBodyThrough(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var receivedBody string
	handler := Middleware(pub, topic, "test-app", nil,
		WithEnabled(true),
		WithSensitivePaths("/api/auth"),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			receivedBody = string(body)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"response-secret"}`))
		}),
	)

	reqBody := `username=alice&password=request-secret`
	req := httptest.NewRequest("POST", "/api/auth/oauth/token", strings.NewReader(reqBody))
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, reqBody, receivedBody)
	require.Equal(t, `{"access_token":"response-secret"}`, rr.Body.String())

	messages := pub.AllMessages()[topic]
	require.Len(t, messages, 1)
	payload := auditPayloadFromMessage(t, messages[0])

	assert.Empty(t, payload.HTTP.Request.Body)
	assert.Empty(t, payload.HTTP.Response.Body)
	assert.False(t, payload.HTTP.Request.BodyTruncated)
	assert.False(t, payload.HTTP.Response.BodyTruncated)
	assert.NotContains(t, string(messages[0].Payload), "request-secret")
	assert.NotContains(t, string(messages[0].Payload), "response-secret")
}

func TestMiddleware_StreamRequestSkipsBodyCapture(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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
	}, WithEnabled(true))(
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

			handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

			handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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
	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

func TestMiddleware_SecretConfiguredAuditsForgedHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		headerValue string
	}{
		{name: "legacy true value", headerValue: "true"},
		{name: "arbitrary client value", headerValue: "x"},
		{name: "wrong secret", headerValue: "not-the-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pub := publish.InMemory()
			topic := "audit-events"

			var downstreamHeaderValue string
			handler := Middleware(pub, topic, "test-app", nil,
				WithEnabled(true),
				WithHandledHeaderSecret("super-secret"),
			)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					downstreamHeaderValue = r.Header.Get(audit.HandledHeader)
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set(audit.HandledHeader, tt.headerValue)
			req = req.WithContext(logging.TestingContext())

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			messages := pub.AllMessages()[topic]
			require.Len(t, messages, 1, "request with forged header must still be audited")

			// Downstream services sharing the secret must see the secret, not the forged value.
			assert.Equal(t, "super-secret", downstreamHeaderValue)

			// The secret must not leak into the audit payload.
			var event publish.EventMessage
			require.NoError(t, json.Unmarshal(messages[0].Payload, &event))

			payloadBytes, _ := json.Marshal(event.Payload)
			var payload audit.Payload
			require.NoError(t, json.Unmarshal(payloadBytes, &payload))

			assert.Empty(t, payload.HTTP.Request.Header.Get(audit.HandledHeader))
			assert.NotContains(t, string(messages[0].Payload), "super-secret")
		})
	}
}

func TestMiddleware_SecretConfiguredSkipsWhenHeaderMatches(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil,
		WithEnabled(true),
		WithHandledHeaderSecret("super-secret"),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set(audit.HandledHeader, "super-secret")
	req = req.WithContext(logging.TestingContext())

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())

	assert.Empty(t, pub.AllMessages()[topic])
}

func TestMiddleware_SecretConfiguredViaConfigFromFlags(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	audit.AddFlags(flags)
	require.NoError(t, flags.Set(audit.AuditEnabledFlag, "true"))
	require.NoError(t, flags.Set(audit.AuditHandledHeaderSecretFlag, "flag-secret"))

	cfg, err := audit.ConfigFromFlags(flags)
	require.NoError(t, err)

	pub := publish.InMemory()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithConfig(cfg))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	// A client-forged header value is still audited.
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set(audit.HandledHeader, "true")
	req = req.WithContext(logging.TestingContext())
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, pub.AllMessages()[topic], 1)

	// The configured secret skips audit.
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set(audit.HandledHeader, "flag-secret")
	req = req.WithContext(logging.TestingContext())
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, pub.AllMessages()[topic], 1)
}

func TestMiddleware_SetsHeaderForDownstream(t *testing.T) {
	t.Parallel()

	pub := publish.InMemory()
	topic := "audit-events"

	var downstreamHeaderValue string
	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
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

func TestMiddleware_DefaultPublishesSynchronously(t *testing.T) {
	pub := newBlockingPublisher()
	topic := "audit-events"

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()

	pub.waitStarted(t)

	select {
	case <-done:
		t.Fatal("handler returned before synchronous publish completed")
	case <-time.After(20 * time.Millisecond):
	}

	pub.release()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after publisher completed")
	}
}

func TestMiddleware_AsyncPublishingReturnsBeforeSlowPublisherCompletes(t *testing.T) {
	pub := newBlockingPublisher()
	topic := "audit-events"
	asyncPublisher := NewAsyncPublisher(pub, topic, "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
	)
	defer func() {
		pub.release()
		require.NoError(t, asyncPublisher.Close(context.Background()))
	}()

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true), WithAsyncPublisher(asyncPublisher))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())

	start := time.Now()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Less(t, elapsed, 50*time.Millisecond)

	pub.waitStarted(t)
}

func TestMiddleware_AsyncPublishingQueueIsBoundedAndDropsWhenFull(t *testing.T) {
	pub := newBlockingPublisher()
	topic := "audit-events"
	var dropped atomic.Uint64
	asyncPublisher := NewAsyncPublisher(pub, topic, "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
		WithAsyncPublishingDropCallback(func(context.Context, audit.Payload) {
			dropped.Add(1)
		}),
	)
	defer func() {
		pub.release()
		require.NoError(t, asyncPublisher.Close(context.Background()))
	}()

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true), WithAsyncPublisher(asyncPublisher))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	serveAuditRequest(t, handler)
	pub.waitStarted(t)

	serveAuditRequest(t, handler)
	require.Eventually(t, func() bool {
		return asyncPublisher.Stats().Enqueued == 2
	}, time.Second, time.Millisecond)

	serveAuditRequest(t, handler)
	require.Eventually(t, func() bool {
		return asyncPublisher.Stats().Dropped == 1
	}, time.Second, time.Millisecond)

	stats := asyncPublisher.Stats()
	assert.Equal(t, uint64(2), stats.Enqueued)
	assert.Equal(t, uint64(1), stats.Dropped)
	assert.Equal(t, uint64(1), dropped.Load())
}

func TestMiddleware_AsyncPublishingWorkerCountIsBounded(t *testing.T) {
	pub := newConcurrentBlockingPublisher()
	topic := "audit-events"
	asyncPublisher := NewAsyncPublisher(pub, topic, "test-app",
		WithAsyncPublishingQueueCapacity(2),
		WithAsyncPublishingWorkerCount(2),
	)
	defer func() {
		pub.release()
		require.NoError(t, asyncPublisher.Close(context.Background()))
	}()

	handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true), WithAsyncPublisher(asyncPublisher))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	serveAuditRequest(t, handler)
	serveAuditRequest(t, handler)
	pub.waitCalls(t, 2)

	serveAuditRequest(t, handler)
	serveAuditRequest(t, handler)
	require.Eventually(t, func() bool {
		return asyncPublisher.Stats().Enqueued == 4
	}, time.Second, time.Millisecond)

	serveAuditRequest(t, handler)
	require.Eventually(t, func() bool {
		return asyncPublisher.Stats().Dropped == 1
	}, time.Second, time.Millisecond)

	assert.LessOrEqual(t, pub.maxInFlight.Load(), int64(2))
}

func TestMiddleware_AsyncPublishingLogsAndCountsPublishErrors(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := logging.NewDefaultLogger(&logBuffer, true, false, false)
	errPublisher := &errorPublisher{err: errors.New("publisher failed")}
	topic := "audit-events"
	asyncPublisher := NewAsyncPublisher(errPublisher, topic, "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
	)

	handler := Middleware(errPublisher, topic, "test-app", nil, WithEnabled(true), WithAsyncPublisher(asyncPublisher))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.ContextWithLogger(context.Background(), logger))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Eventually(t, func() bool {
		return asyncPublisher.Stats().PublishErrors == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, asyncPublisher.Close(context.Background()))

	stats := asyncPublisher.Stats()
	assert.Equal(t, uint64(1), stats.Enqueued)
	assert.Equal(t, uint64(0), stats.Published)
	assert.Equal(t, uint64(1), stats.PublishErrors)
	assert.Contains(t, logBuffer.String(), "failed to publish audit message asynchronously")
	assert.Contains(t, logBuffer.String(), "publisher failed")
}

func TestMiddleware_PublishLatencyRegression(t *testing.T) {
	topic := "audit-events"
	publishDelay := 100 * time.Millisecond

	t.Run("synchronous response includes publish delay", func(t *testing.T) {
		pub := &delayedPublisher{delay: publishDelay}
		handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)

		start := time.Now()
		serveAuditRequest(t, handler)

		assert.GreaterOrEqual(t, time.Since(start), publishDelay)
		assert.Equal(t, uint64(1), pub.calls.Load())
	})

	t.Run("async response does not include publish delay", func(t *testing.T) {
		pub := &delayedPublisher{delay: publishDelay}
		asyncPublisher := NewAsyncPublisher(pub, topic, "test-app",
			WithAsyncPublishingQueueCapacity(1),
			WithAsyncPublishingWorkerCount(1),
		)
		defer func() {
			require.NoError(t, asyncPublisher.Close(context.Background()))
		}()

		handler := Middleware(pub, topic, "test-app", nil, WithEnabled(true), WithAsyncPublisher(asyncPublisher))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)

		start := time.Now()
		serveAuditRequest(t, handler)

		assert.Less(t, time.Since(start), publishDelay/2)
		require.Eventually(t, func() bool {
			return asyncPublisher.Stats().Published == 1
		}, time.Second, time.Millisecond)
		assert.Equal(t, uint64(1), pub.calls.Load())
	})
}

func TestMiddleware_WithAsyncPublishingUsesCallerManagedPublisher(t *testing.T) {
	pub := &delayedPublisher{}
	topic := "audit-events"
	asyncPublisher := NewAsyncPublisher(pub, topic, "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
	)
	defer func() {
		require.NoError(t, asyncPublisher.Close(context.Background()))
	}()

	handler := Middleware(pub, topic, "test-app", nil,
		WithEnabled(true),
		WithAsyncPublishing(asyncPublisher),
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	serveAuditRequest(t, handler)

	require.Eventually(t, func() bool {
		return pub.calls.Load() == 1
	}, time.Second, time.Millisecond)
}

func TestAsyncPublisher_CapturesEventDateBeforeQueueDelay(t *testing.T) {
	pub := newSequentialBlockingRecorderPublisher()
	asyncPublisher := NewAsyncPublisher(pub, "audit-events", "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
	)
	defer func() {
		pub.release()
		require.NoError(t, asyncPublisher.Close(context.Background()))
	}()

	asyncPublisher.Publish(logging.TestingContext(), audit.Payload{ID: "first"})
	pub.waitCalls(t, 1)

	beforeSecondEnqueue := time.Now().UTC()
	asyncPublisher.Publish(logging.TestingContext(), audit.Payload{ID: "second"})
	time.Sleep(50 * time.Millisecond)
	releaseTime := time.Now().UTC()
	pub.release()
	pub.waitCalls(t, 2)

	msgs := pub.messages()
	require.Len(t, msgs, 2)

	var event publish.EventMessage
	require.NoError(t, json.Unmarshal(msgs[1].Payload, &event))
	assert.GreaterOrEqual(t, event.Date.UnixNano(), beforeSecondEnqueue.UnixNano())
	assert.Less(t, event.Date.UnixNano(), releaseTime.UnixNano())
}

func TestAsyncPublisher_CloseRespectsContextTimeout(t *testing.T) {
	pub := newBlockingPublisher()
	asyncPublisher := NewAsyncPublisher(pub, "audit-events", "test-app",
		WithAsyncPublishingQueueCapacity(1),
		WithAsyncPublishingWorkerCount(1),
	)

	asyncPublisher.Publish(logging.TestingContext(), audit.Payload{ID: "payload-id"})
	pub.waitStarted(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, asyncPublisher.Close(ctx), context.DeadlineExceeded)

	pub.release()
	require.NoError(t, asyncPublisher.Close(context.Background()))
}

func serveAuditRequest(t *testing.T, handler http.Handler) {
	t.Helper()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req = req.WithContext(logging.TestingContext())
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

type blockingPublisher struct {
	started     chan struct{}
	released    chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
	calls       atomic.Uint64
}

func newBlockingPublisher() *blockingPublisher {
	return &blockingPublisher{
		started:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (p *blockingPublisher) Publish(string, ...*message.Message) error {
	p.calls.Add(1)
	p.startedOnce.Do(func() {
		close(p.started)
	})
	<-p.released
	return nil
}

func (p *blockingPublisher) Close() error {
	return nil
}

func (p *blockingPublisher) waitStarted(t *testing.T) {
	t.Helper()

	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("publisher was not called")
	}
}

func (p *blockingPublisher) release() {
	p.releaseOnce.Do(func() {
		close(p.released)
	})
}

type concurrentBlockingPublisher struct {
	released    chan struct{}
	releaseOnce sync.Once
	calls       atomic.Uint64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
}

func newConcurrentBlockingPublisher() *concurrentBlockingPublisher {
	return &concurrentBlockingPublisher{
		released: make(chan struct{}),
	}
}

func (p *concurrentBlockingPublisher) Publish(string, ...*message.Message) error {
	p.calls.Add(1)
	inFlight := p.inFlight.Add(1)
	for {
		maxInFlight := p.maxInFlight.Load()
		if inFlight <= maxInFlight || p.maxInFlight.CompareAndSwap(maxInFlight, inFlight) {
			break
		}
	}

	<-p.released
	p.inFlight.Add(-1)
	return nil
}

func (p *concurrentBlockingPublisher) Close() error {
	return nil
}

func (p *concurrentBlockingPublisher) waitCalls(t *testing.T, calls uint64) {
	t.Helper()

	require.Eventually(t, func() bool {
		return p.calls.Load() >= calls
	}, time.Second, time.Millisecond)
}

func (p *concurrentBlockingPublisher) release() {
	p.releaseOnce.Do(func() {
		close(p.released)
	})
}

type sequentialBlockingRecorderPublisher struct {
	mu          sync.Mutex
	released    chan struct{}
	releaseOnce sync.Once
	calls       atomic.Uint64
	messagesSet []*message.Message
}

func newSequentialBlockingRecorderPublisher() *sequentialBlockingRecorderPublisher {
	return &sequentialBlockingRecorderPublisher{
		released: make(chan struct{}),
	}
}

func (p *sequentialBlockingRecorderPublisher) Publish(_ string, messages ...*message.Message) error {
	call := p.calls.Add(1)
	p.mu.Lock()
	p.messagesSet = append(p.messagesSet, messages...)
	p.mu.Unlock()

	if call == 1 {
		<-p.released
	}
	return nil
}

func (p *sequentialBlockingRecorderPublisher) Close() error {
	return nil
}

func (p *sequentialBlockingRecorderPublisher) waitCalls(t *testing.T, calls uint64) {
	t.Helper()

	require.Eventually(t, func() bool {
		return p.calls.Load() >= calls
	}, time.Second, time.Millisecond)
}

func (p *sequentialBlockingRecorderPublisher) release() {
	p.releaseOnce.Do(func() {
		close(p.released)
	})
}

func (p *sequentialBlockingRecorderPublisher) messages() []*message.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*message.Message(nil), p.messagesSet...)
}

type delayedPublisher struct {
	delay time.Duration
	calls atomic.Uint64
}

func (p *delayedPublisher) Publish(string, ...*message.Message) error {
	p.calls.Add(1)
	time.Sleep(p.delay)
	return nil
}

func (p *delayedPublisher) Close() error {
	return nil
}

type errorPublisher struct {
	err   error
	calls atomic.Uint64
}

func (p *errorPublisher) Publish(string, ...*message.Message) error {
	p.calls.Add(1)
	return p.err
}

func (p *errorPublisher) Close() error {
	return nil
}
