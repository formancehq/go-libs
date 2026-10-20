package payloadlog_test

import (
	"bytes"
	"testing"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/payloadlog"
)

func TestCaptureOmitsJSONWhenCallerRejectsSanitization(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	payload := []byte(`{ "Bearer alpha":1, "Bearer beta":2, "safe":"\\u0061", "n":1e+09 }`)
	original := bytes.Clone(payload)
	var buffer bytes.Buffer
	produced := false
	payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Sanitize:  func([]byte) ([]byte, bool) { return nil, false },
		Transport: "http", Direction: "receive", ContentType: "application/json",
		Limit: int64(len(payload)), Fields: map[string]any{"caller": "retained"},
		Produce: func(int64) ([]byte, error) { produced = true; return payload, nil },
	})
	if !produced {
		t.Fatal("JSON payload was not examined")
	}
	if !bytes.Equal(payload, original) {
		t.Error("capture mutated caller JSON bytes")
	}
	records := lines(t, &buffer)
	if len(records) != 1 {
		t.Fatalf("got %d records, want one metadata-only omission", len(records))
	}
	record := records[0]
	if record["payload.omitted_reason"] != "unsupported_content" {
		t.Error("JSON with colliding redacted keys was not refused")
	}
	captureID, ok := record["payload.capture_id"].(string)
	if !ok || captureID == "" || record["payload.original_bytes"] != float64(len(original)) || record["caller"] != "retained" {
		t.Error("omission lost identity, original size or caller context")
	}
	for _, key := range []string{"payload.chunk", "payload.chunk_index", "payload.chunk_count", "payload.encoding", "payload.sha256", "payload.redacted", "payload.rendered_bytes"} {
		if _, exists := record[key]; exists {
			t.Errorf("unsafe JSON produced capture field %s instead of metadata-only omission", key)
		}
	}
}
