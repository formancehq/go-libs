package payloadlog_test

import (
	"bytes"
	"testing"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/payloadlog"
)

func TestStructuredCaptureRequiresSafeCallerPolicy(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })
	for _, tc := range []struct {
		name     string
		sanitize func([]byte) ([]byte, bool)
	}{
		{name: "missing"},
		{name: "refused", sanitize: func([]byte) ([]byte, bool) { return nil, false }},
		{name: "invalid JSON", sanitize: func([]byte) ([]byte, bool) { return []byte(`{`), true }},
		{name: "expanded beyond cap", sanitize: func([]byte) ([]byte, bool) { return []byte(`{"long":"expanded"}`), true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&out, logging.DebugLevel), payloadlog.Record{
				ContentType: "application/json", Limit: 2, Sanitize: tc.sanitize,
				Produce: func(int64) ([]byte, error) { return []byte(`{}`), nil },
			})
			got := lines(t, &out)
			if len(got) != 1 || got[0]["payload.omitted_reason"] == nil || got[0]["payload.chunk"] != nil {
				t.Fatalf("unsafe capture: %v", got)
			}
		})
	}
}

func TestSanitizerCannotMutateCallerPayload(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })
	original := []byte(`{"private":1}`)
	before := bytes.Clone(original)
	var out bytes.Buffer
	payloadlog.Capture(t.Context(), jsonLogger(&out, logging.DebugLevel), payloadlog.Record{
		ContentType: "application/json", Limit: 100,
		Produce:  func(int64) ([]byte, error) { return original, nil },
		Sanitize: func(b []byte) ([]byte, bool) { clear(b); return []byte(`{}`), true },
	})
	if !bytes.Equal(original, before) {
		t.Fatal("sanitizer mutated producer bytes")
	}
}

func TestEmptyStructuredCaptureStillRequiresCallerPolicy(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })
	for _, contentType := range []string{"application/json", "application/x-www-form-urlencoded"} {
		for _, policy := range []string{"missing", "rejecting", "identity"} {
			t.Run(contentType+"/"+policy, func(t *testing.T) {
				var out bytes.Buffer
				called := false
				var sanitize func([]byte) ([]byte, bool)
				if policy != "missing" {
					sanitize = func(b []byte) ([]byte, bool) { called = true; return b, policy == "identity" }
				}
				payloadlog.Capture(t.Context(), jsonLogger(&out, logging.DebugLevel), payloadlog.Record{
					ContentType: contentType, Limit: 100, Sanitize: sanitize,
					Produce: func(int64) ([]byte, error) { return nil, nil },
				})
				got := lines(t, &out)
				omitted := policy != "identity" || contentType == "application/json"
				if len(got) != 1 || (got[0]["payload.omitted_reason"] != nil) != omitted {
					t.Fatalf("unexpected admission: %v", got)
				}
				if called != (policy != "missing") {
					t.Fatal("caller policy was bypassed")
				}
			})
		}
	}
}
