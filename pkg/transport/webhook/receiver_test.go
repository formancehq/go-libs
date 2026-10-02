package webhook_test

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/transport/webhook"
)

// countingReader records how many bytes the receiver pulled from the body.
type countingReader struct {
	r    io.Reader
	read int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)

	return n, err
}

// probe records what the Verifier and Deliver were handed.
type probe struct {
	verified   [][]byte
	delivered  []webhook.Delivery
	verifyErr  error
	deliverErr error
}

func (p *probe) config(maxBody int64) webhook.Config {
	return webhook.Config{
		MaxBody: maxBody,
		Verifier: webhook.VerifierFunc(func(_ context.Context, _ *http.Request, body []byte) error {
			p.verified = append(p.verified, body)

			return p.verifyErr
		}),
		Deliver: func(_ context.Context, d webhook.Delivery) error {
			p.delivered = append(p.delivered, d)

			return p.deliverErr
		},
	}
}

func newReceiver(t *testing.T, cfg webhook.Config) http.Handler {
	t.Helper()
	h, err := webhook.NewReceiver(cfg)
	require.NoError(t, err)

	return h
}

func serve(h http.Handler, method string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/webhooks?topic=payments", body)
	req.Header.Set("X-Signature", "sig")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestNewReceiverRejectsIncompleteConfig(t *testing.T) {
	t.Parallel()

	valid := (&probe{}).config(0)

	for name, mutate := range map[string]func(*webhook.Config){
		"nil verifier":     func(c *webhook.Config) { c.Verifier = nil },
		"nil deliver":      func(c *webhook.Config) { c.Deliver = nil },
		"negative maxbody": func(c *webhook.Config) { c.MaxBody = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			mutate(&cfg)
			h, err := webhook.NewReceiver(cfg)
			require.Error(t, err)
			require.Nil(t, h)
		})
	}
}

const testMaxBody = 16

func TestReceiverAcceptsBodyAtCap(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(testMaxBody))
	payload := []byte(strings.Repeat("a", testMaxBody))

	rec := serve(h, http.MethodPost, bytes.NewReader(payload))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Body.Bytes())
	require.Equal(t, [][]byte{payload}, p.verified, "the verifier sees the exact raw bytes")
	require.Len(t, p.delivered, 1)
	d := p.delivered[0]
	require.Equal(t, payload, d.Body, "Deliver receives the bytes that were verified")
	require.Equal(t, "sig", d.Header.Get("X-Signature"))
	require.Equal(t, "payments", d.Query.Get("topic"))
}

func TestReceiverRejectsStreamedBodyPastCapWithoutDraining(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(testMaxBody))
	body := &countingReader{r: strings.NewReader(strings.Repeat("a", 1<<20))}

	// A custom reader leaves ContentLength unknown (-1), so the cap is
	// enforced while reading rather than from the declared length.
	rec := serve(h, http.MethodPost, body)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.LessOrEqual(t, body.read, int64(testMaxBody+1), "never read past MaxBody+1")
	require.Greater(t, body.read, int64(testMaxBody), "the cap is detected by reading one byte past it")
	require.Empty(t, p.verified, "an oversized body never reaches the verifier")
	require.Empty(t, p.delivered)
}

func TestReceiverRejectsDeclaredLengthPastCapUnread(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(testMaxBody))
	body := &countingReader{r: strings.NewReader(strings.Repeat("a", testMaxBody+1))}
	req := httptest.NewRequest(http.MethodPost, "/webhooks", body)
	req.ContentLength = testMaxBody + 1
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Zero(t, body.read, "a declared oversized body is rejected before any read")
	require.Empty(t, p.verified)
	require.Empty(t, p.delivered)
}

func TestReceiverAppliesDefaultMaxBody(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(0))

	atCap := serve(h, http.MethodPost, bytes.NewReader(make([]byte, webhook.DefaultMaxBody)))
	require.Equal(t, http.StatusOK, atCap.Code)
	require.Len(t, p.delivered, 1)
	require.Len(t, p.delivered[0].Body, int(webhook.DefaultMaxBody))

	past := &countingReader{r: bytes.NewReader(make([]byte, webhook.DefaultMaxBody+1024))}
	rec := serve(h, http.MethodPost, past)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.LessOrEqual(t, past.read, webhook.DefaultMaxBody+1)
	require.Len(t, p.delivered, 1, "the oversized delivery is not handed on")
}

func TestReceiverRejectsFailedVerification(t *testing.T) {
	t.Parallel()

	p := &probe{verifyErr: errors.New("expected signature deadbeef")}
	h := newReceiver(t, p.config(testMaxBody))

	rec := serve(h, http.MethodPost, strings.NewReader("payload"))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NotContains(t, rec.Body.String(), "deadbeef", "the verifier error never reaches the wire")
	require.Len(t, p.verified, 1)
	require.Empty(t, p.delivered, "an unverified delivery is never handed on")
}

func TestReceiverAsksForRedeliveryWhenDeliverFails(t *testing.T) {
	t.Parallel()

	p := &probe{deliverErr: errors.New("spool full")}
	h := newReceiver(t, p.config(testMaxBody))

	rec := serve(h, http.MethodPost, strings.NewReader("payload"))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), "spool full")
	require.Len(t, p.delivered, 1)
}

func TestReceiverRejectsUnreadableBody(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(testMaxBody))

	rec := serve(h, http.MethodPost, iotest.ErrReader(errors.New("connection reset")))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, p.verified)
	require.Empty(t, p.delivered)
}

func TestReceiverAnswersHEADWithoutReadingBody(t *testing.T) {
	t.Parallel()

	p := &probe{}
	h := newReceiver(t, p.config(testMaxBody))
	body := &countingReader{r: strings.NewReader("payload")}

	rec := serve(h, http.MethodHead, body)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Body.Bytes())
	require.Zero(t, body.read)
	require.Empty(t, p.verified)
	require.Empty(t, p.delivered)
}

func TestReceiverRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			p := &probe{}
			h := newReceiver(t, p.config(testMaxBody))
			body := &countingReader{r: strings.NewReader("payload")}

			rec := serve(h, method, body)

			require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			require.Equal(t, "HEAD, POST", rec.Header().Get("Allow"))
			require.Zero(t, body.read)
			require.Empty(t, p.verified)
			require.Empty(t, p.delivered)
		})
	}
}

// spanText flattens everything a span exports, so a secret can be searched
// for in one place.
func spanText(s sdktrace.ReadOnlySpan) string {
	var b strings.Builder
	b.WriteString(s.Name() + "\n" + s.Status().Description + "\n")
	for _, kv := range s.Attributes() {
		b.WriteString(string(kv.Key) + "=" + kv.Value.Emit() + "\n")
	}
	for _, e := range s.Events() {
		b.WriteString(e.Name + "\n")
		for _, kv := range e.Attributes {
			b.WriteString(string(kv.Key) + "=" + kv.Value.Emit() + "\n")
		}
	}

	return b.String()
}

func statusAttribute(s sdktrace.ReadOnlySpan) (int64, bool) {
	for _, kv := range s.Attributes() {
		if kv.Key == "http.response.status_code" {
			return kv.Value.AsInt64(), true
		}
	}

	return 0, false
}

// Not parallel: it installs a global tracer provider, as the httpserver
// middleware tests do.
func TestReceiverRecordsOneSpanPerDelivery(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	const (
		secretBody   = "body-secret-value"
		secretHeader = "header-secret-value"
		secretQuery  = "query-secret-value"
		secretVerify = "verify-secret-value"
	)
	// A Deliver error built from the delivery, quoting all three.
	deliverErr := fmt.Errorf("spool rejected %s (sig %s, token %s)", secretBody, secretHeader, secretQuery)

	for _, tc := range []struct {
		name       string
		body       string
		verifyErr  error
		deliverErr error
		wantStatus int
		wantCode   codes.Code
		wantCalls  int
	}{
		{name: "delivered", body: secretBody, wantStatus: http.StatusOK, wantCode: codes.Unset, wantCalls: 2},
		{name: "oversized", body: secretBody + strings.Repeat("x", 64), wantStatus: http.StatusRequestEntityTooLarge, wantCode: codes.Error},
		{name: "unverified", body: secretBody, verifyErr: errors.New(secretVerify), wantStatus: http.StatusUnauthorized, wantCode: codes.Error, wantCalls: 1},
		{name: "undelivered", body: secretBody, deliverErr: deliverErr, wantStatus: http.StatusServiceUnavailable, wantCode: codes.Error, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctxSpans []trace.SpanID
			h := newReceiver(t, webhook.Config{
				MaxBody: 32,
				Verifier: webhook.VerifierFunc(func(ctx context.Context, _ *http.Request, _ []byte) error {
					ctxSpans = append(ctxSpans, trace.SpanContextFromContext(ctx).SpanID())

					return tc.verifyErr
				}),
				Deliver: func(ctx context.Context, _ webhook.Delivery) error {
					ctxSpans = append(ctxSpans, trace.SpanContextFromContext(ctx).SpanID())

					return tc.deliverErr
				},
			})
			before := len(recorder.Ended())

			req := httptest.NewRequest(http.MethodPost, "/webhooks?token="+secretQuery, strings.NewReader(tc.body))
			req.Header.Set("X-Signature", secretHeader)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			spans := recorder.Ended()[before:]
			require.Len(t, spans, 1, "one span per delivery")
			span := spans[0]
			require.Equal(t, "webhook.delivery", span.Name())
			require.Equal(t, tc.wantCode, span.Status().Code)
			status, ok := statusAttribute(span)
			require.True(t, ok)
			require.Equal(t, int64(tc.wantStatus), status)
			require.Len(t, ctxSpans, tc.wantCalls)
			for _, id := range ctxSpans {
				require.Equal(t, span.SpanContext().SpanID(), id, "Verify and Deliver run under the delivery span")
			}

			text := spanText(span)
			for _, secret := range []string{secretBody, secretHeader, secretQuery, secretVerify} {
				require.NotContains(t, text, secret)
			}
			if failure := cmp.Or(tc.verifyErr, tc.deliverErr); failure != nil {
				require.NotContains(t, text, failure.Error(), "a callback's error text stays off the span")
				require.Contains(t, text, fmt.Sprintf("%T", failure), "the error's Go type is kept")
			}
		})
	}

	t.Run("probes and rejected methods get no span", func(t *testing.T) {
		h := newReceiver(t, (&probe{}).config(testMaxBody))
		before := len(recorder.Ended())

		require.Equal(t, http.StatusOK, serve(h, http.MethodHead, http.NoBody).Code)
		require.Equal(t, http.StatusMethodNotAllowed, serve(h, http.MethodGet, http.NoBody).Code)
		require.Len(t, recorder.Ended(), before)
	})
}

func TestReceiverLogsRejectionsWithoutRequestContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		probe      *probe
		wantStatus int
	}{
		{name: "unverified", probe: &probe{verifyErr: errors.New("bad signature header-secret")}, wantStatus: http.StatusUnauthorized},
		{name: "undelivered", probe: &probe{deliverErr: errors.New("spool rejected body-secret token=query-secret")}, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			logger := logging.NewDefaultLogger(&out, true, false, false)
			h := newReceiver(t, tc.probe.config(testMaxBody))

			req := httptest.NewRequest(http.MethodPost, "/webhooks?token=query-secret", strings.NewReader("body-secret"))
			req = req.WithContext(logging.ContextWithLogger(req.Context(), logger))
			req.Header.Set("X-Signature", "header-secret")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			logged := out.String()
			require.Contains(t, logged, "webhook delivery rejected")
			require.Contains(t, logged, "*errors.errorString", "the error's Go type is logged")
			for _, secret := range []string{"body-secret", "header-secret", "query-secret", "bad signature", "spool rejected"} {
				require.NotContains(t, logged, secret)
			}
		})
	}
}
