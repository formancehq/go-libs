package payloadlog_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/payloadlog"
	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

func jsonLogger(buffer *bytes.Buffer, level logging.Level) logging.Logger {
	return logging.NewZapStack(buffer, logging.ToZapLevel(level), logging.ZapStackOptions{JSONFormatting: true, Correlate: true})
}

func lines(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buffer.String()), "\n") {
		if line == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("decode log: %v (%q)", err, line)
		}
		out = append(out, decoded)
	}

	return out
}

func TestDisabledCaptureDoesNotProducePayload(t *testing.T) {
	payloadlog.Configure(false, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	called := false
	payloadlog.Capture(t.Context(), jsonLogger(&bytes.Buffer{}, logging.DebugLevel), payloadlog.Record{
		Transport: "test", Direction: "receive", Limit: 1024,
		Produce: func(int64) ([]byte, error) { called = true; return []byte("payload"), nil },
	})
	if called {
		t.Fatal("producer called while explicit payload gate was off")
	}

	payloadlog.Configure(true, 0)
	payloadlog.Capture(t.Context(), jsonLogger(&bytes.Buffer{}, logging.InfoLevel), payloadlog.Record{
		Transport: "test", Direction: "receive", Limit: 1024,
		Produce: func(int64) ([]byte, error) { called = true; return []byte("payload"), nil },
	})
	if called {
		t.Fatal("producer called while Debug logging was off")
	}
}

func TestCaptureRedactsBeforeChunkingAndReassembles(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	secret := strings.Repeat("S", 96)
	padding := strings.Repeat("x", 64<<10)
	payload := []byte(`{"padding":"` + padding + `","data":{"auth":"` + secret + `"},"tail":"ok"}`)
	var buffer bytes.Buffer
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2},
	}))
	payloadlog.Capture(ctx, jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Transport: "websocket", Direction: "receive", Operation: "frame", Fields: map[string]any{"component.name": "example"},
		ContentType: "application/json", Limit: int64(len(payload)),
		Produce: func(int64) ([]byte, error) { return payload, nil },
	})

	logs := lines(t, &buffer)
	if len(logs) < 2 {
		t.Fatalf("got %d chunks, want at least 2", len(logs))
	}
	var rebuilt strings.Builder
	var captureID, digest string
	for index, line := range logs {
		if line["trace_id"] == nil || line["span_id"] == nil {
			t.Fatalf("correlation missing: %v", line)
		}
		chunkIndex, ok := line["payload.chunk_index"].(float64)
		if !ok || int(chunkIndex) != index {
			t.Fatalf("chunk index = %v, want %d", line["payload.chunk_index"], index)
		}
		if index == 0 {
			captureID, _ = line["payload.capture_id"].(string)
			digest, _ = line["payload.sha256"].(string)
		} else if line["payload.capture_id"] != captureID || line["payload.sha256"] != digest {
			t.Fatalf("chunk identity drift: %v", line)
		}
		chunk, ok := line["payload.chunk"].(string)
		if !ok {
			t.Fatalf("chunk is %T, want string", line["payload.chunk"])
		}
		rebuilt.WriteString(chunk)
	}
	gotBytes, err := base64.StdEncoding.DecodeString(rebuilt.String())
	if err != nil {
		t.Fatalf("decode reconstructed payload: %v", err)
	}
	got := string(gotBytes)
	if strings.Contains(got, secret) || !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, `"tail":"ok"`) {
		t.Fatalf("unsafe or incomplete reconstruction")
	}
	recomputed := sha256.Sum256(gotBytes)
	if hex.EncodeToString(recomputed[:]) != digest {
		t.Fatalf("payload digest = %q, want %q", digest, hex.EncodeToString(recomputed[:]))
	}
}

func TestCaptureFailsClosedForUnsupportedOrOversizedContent(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	for name, record := range map[string]payloadlog.Record{
		"binary": {
			Transport: "http", Direction: "receive", ContentType: "application/octet-stream", Limit: 16,
			Produce: func(int64) ([]byte, error) { return []byte{0xff, 0x00}, nil },
		},
		"xml containing credentials": {
			Transport: "http", Direction: "receive", ContentType: "application/xml", Limit: 128,
			Produce: func(int64) ([]byte, error) { return []byte(`<root><password>secret</password></root>`), nil },
		},
		"untrusted plain text": {
			Transport: "webhook", Direction: "receive", ContentType: "text/plain", Limit: 128,
			Produce: func(int64) ([]byte, error) { return []byte("password=secret"), nil },
		},
		"unknown media type": {
			Transport: "http", Direction: "receive", ContentType: "", Limit: 128,
			Produce: func(int64) ([]byte, error) { return []byte(`{"password":"secret"}`), nil },
		},
		"non-string JSON credential": {
			Transport: "http", Direction: "receive", ContentType: "application/json", Limit: 128,
			Produce: func(int64) ([]byte, error) { return []byte(`{"password":{"value":"secret"}}`), nil },
		},
		"over limit": {
			Transport: "http", Direction: "receive", ContentType: "application/json", Limit: 2,
			Produce: func(int64) ([]byte, error) { return []byte("{}\n"), nil },
		},
	} {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), record)
			logs := lines(t, &buffer)
			if len(logs) != 1 || logs[0]["payload.omitted_reason"] == nil || logs[0]["payload.chunk"] != nil {
				t.Fatalf("expected one metadata-only omission: %v", logs)
			}
			if logs[0]["payload.capture_id"] == "" || logs[0]["payload.original_bytes"] == nil {
				t.Fatalf("omission identity or size missing: %v", logs)
			}
			if strings.Contains(buffer.String(), "secret") {
				t.Fatalf("omission leaked payload: %s", buffer.String())
			}
		})
	}
}

func TestConfiguredLimitCanOnlyLowerAdapterLimit(t *testing.T) {
	payloadlog.Configure(true, 4)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	var buffer bytes.Buffer
	payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Transport: "http", Direction: "send", ContentType: "application/json", Limit: 1024,
		Produce: func(limit int64) ([]byte, error) {
			if limit != 4 {
				t.Fatalf("producer limit = %d, want configured lower limit 4", limit)
			}

			return []byte(`{"x":1}`), nil
		},
	})
	logs := lines(t, &buffer)
	if len(logs) != 1 || logs[0]["payload.omitted_reason"] != "limit_exceeded" {
		t.Fatalf("configured lower ceiling not enforced: %v", logs)
	}
}

func TestCaptureDoesNotLogProducerErrorText(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	var buffer bytes.Buffer
	payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Transport: "http", Direction: "receive", ContentType: "application/json", Limit: 1024,
		Produce: func(int64) ([]byte, error) { return nil, errors.New("Authorization: Bearer secret") },
	})
	if strings.Contains(buffer.String(), "secret") {
		t.Fatalf("producer error leaked into log: %s", buffer.String())
	}
	logs := lines(t, &buffer)
	if len(logs) != 1 || logs[0]["payload.omitted_reason"] != "produce_failed" {
		t.Fatalf("unexpected omission: %v", logs)
	}
}

func TestCapturePreservesMultibyteJSONAcrossChunks(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	payload := []byte(`{"message":"` + strings.Repeat("euro€", 20_000) + `"}`)
	var buffer bytes.Buffer
	payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Transport: "websocket", Direction: "receive", ContentType: "application/json", Limit: int64(len(payload)),
		Produce: func(int64) ([]byte, error) { return payload, nil },
	})

	var encoded strings.Builder
	for _, line := range lines(t, &buffer) {
		chunk, ok := line["payload.chunk"].(string)
		if !ok {
			t.Fatalf("chunk is %T, want string", line["payload.chunk"])
		}
		encoded.WriteString(chunk)
	}
	rebuilt, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !bytes.Equal(rebuilt, payload) {
		t.Fatal("multibyte payload changed during chunking")
	}
}

func TestCaptureRedactsFormEncodedPayloadWithoutReordering(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	var buffer bytes.Buffer
	payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
		Transport: "http", Direction: "send", ContentType: "application/x-www-form-urlencoded", Limit: 1024,
		Produce: func(int64) ([]byte, error) {
			return []byte("grant_type=client_credentials&client_secret=secret&scope=read+write"), nil
		},
	})
	logs := lines(t, &buffer)
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
	chunk, _ := logs[0]["payload.chunk"].(string)
	decoded, err := base64.StdEncoding.DecodeString(chunk)
	if err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	got := string(decoded)
	if strings.Contains(got, "client_secret=secret") || got != "grant_type=client_credentials&client_secret=%5BREDACTED%5D&scope=read+write" {
		t.Fatalf("unsafe or reordered form: %q", got)
	}
}

func TestCaptureRedactsPlainTextAndAcceptsEmptyPayload(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	for name, payload := range map[string][]byte{
		"plain text": []byte("Authorization: Bearer secret\nunauthorized\n"),
		"empty":      {},
	} {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
				Transport: "webhook", Direction: "send", ContentType: "text/plain; charset=utf-8", Limit: 1024,
				TrustedPlainText: true,
				Produce:          func(int64) ([]byte, error) { return payload, nil },
			})
			logs := lines(t, &buffer)
			if len(logs) != 1 || logs[0]["payload.omitted_reason"] != nil {
				t.Fatalf("payload not captured: %v", logs)
			}
			decoded, err := base64.StdEncoding.DecodeString(logs[0]["payload.chunk"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if name == "empty" {
				if len(decoded) != 0 {
					t.Fatal("empty payload changed")
				}
			} else if strings.Contains(string(decoded), "secret") || !strings.Contains(string(decoded), redact.Marker) {
				t.Fatal("decoded plain-text payload did not redact the credential")
			}
		})
	}
}

func TestUnsupportedContentDoesNotInvokeProducer(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	for _, contentType := range []string{"", "application/xml", "application/octet-stream; token=provider-secret", "text/plain", "malformed; token=provider-secret"} {
		called := false
		var buffer bytes.Buffer
		payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
			Transport: "http", Direction: "receive", ContentType: contentType, Limit: 32 << 20,
			Produce: func(int64) ([]byte, error) {
				called = true

				return []byte("provider-secret"), nil
			},
		})
		if called {
			t.Errorf("producer called for unsupported content type %q", contentType)
		}
		logs := lines(t, &buffer)
		if len(logs) != 1 || logs[0]["payload.omitted_reason"] != "unsupported_content" || logs[0]["payload.original_bytes"] != float64(-1) {
			t.Errorf("unsupported content metadata for %q = %v", contentType, logs)
		}
		if strings.Contains(buffer.String(), "provider-secret") {
			t.Errorf("content-type parameter leaked for %q: %s", contentType, buffer.String())
		}
	}
}

func TestTrustedEmptyContentCapturesOnlyAnEmptyBody(t *testing.T) {
	payloadlog.Configure(true, 0)
	t.Cleanup(func() { payloadlog.Configure(false, 0) })

	for name, payload := range map[string][]byte{"empty": {}, "unexpected bytes": []byte(`{"value":1}`)} {
		t.Run(name, func(t *testing.T) {
			var buffer bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
				Transport: "webhook", Direction: "send", ContentType: "", TrustedEmpty: true, Limit: 1024,
				Produce: func(int64) ([]byte, error) { return payload, nil },
			})
			logs := lines(t, &buffer)
			if len(logs) != 1 {
				t.Fatalf("logs = %v", logs)
			}
			if len(payload) == 0 && logs[0]["payload.chunk"] == nil {
				t.Fatalf("trusted empty body was not captured: %v", logs)
			}
			if len(payload) > 0 && (logs[0]["payload.omitted_reason"] != "unsupported_content" || logs[0]["payload.chunk"] != nil) {
				t.Fatalf("unexpected untyped bytes did not fail closed: %v", logs)
			}
		})
	}
}

func TestCaptureAdmissionAndFieldIsolation(t *testing.T) {
	for _, test := range []struct {
		name           string
		maxBody, limit int64
		produce        payloadlog.Produce
		reason         string
	}{
		{name: "invalid adapter limit", limit: 0, reason: "invalid_limit"},
		{name: "negative operator limit", maxBody: -1, limit: 128, reason: "invalid_limit"},
		{name: "missing producer", limit: 128, reason: "missing_producer"},
		{name: "invalid UTF8", limit: 128, produce: func(int64) ([]byte, error) { return []byte{0xff}, nil }, reason: "unsupported_content"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payloadlog.Configure(true, test.maxBody)
			t.Cleanup(func() { payloadlog.Configure(false, 0) })
			fields := map[string]any{"payload.capture_id": "caller", "retained": "evidence"}
			var buffer bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
				ContentType: "application/json", Limit: test.limit, Produce: test.produce, Fields: fields,
			})
			logs := lines(t, &buffer)
			if len(logs) != 1 || logs[0]["payload.omitted_reason"] != test.reason || logs[0]["retained"] != "evidence" {
				t.Fatalf("unexpected admission result: %v", logs)
			}
			if len(fields) != 2 || fields["payload.capture_id"] != "caller" {
				t.Fatal("capture mutated caller metadata")
			}
		})
	}
}

func TestCaptureMasksConfiguredContentTypeAndReservesMetadata(t *testing.T) {
	payloadlog.Configure(true, 0)
	redact.ConfigureValues([]string{"configured-private-credential"})
	t.Cleanup(func() { payloadlog.Configure(false, 0); redact.ConfigureValues(nil) })
	for _, contentType := range []string{"application/configured-private-credential", "application/configured-private-credential+json"} {
		t.Run(contentType, func(t *testing.T) {
			fields := map[string]any{"payload.chunk": "stale-content", "payload.redacted": true, "payload.omitted_reason": "stale-reason", "payload.sha256": "stale-hash", "payload.chunk_count": 99, "caller": "retained"}
			var out bytes.Buffer
			produced := false
			payloadlog.Capture(t.Context(), jsonLogger(&out, logging.DebugLevel), payloadlog.Record{ContentType: contentType, Limit: 1024, Fields: fields, Produce: func(int64) ([]byte, error) { produced = true; return []byte(`{}`), nil }})
			records := lines(t, &out)
			if len(records) != 1 {
				t.Fatalf("records=%v", records)
			}
			got := records[0]
			if strings.Contains(out.String(), "configured-private-credential") {
				t.Fatal("content type credential leaked")
			}
			if got["caller"] != "retained" || fields["payload.chunk"] != "stale-content" || fields["payload.omitted_reason"] != "stale-reason" {
				t.Fatal("caller field ownership changed")
			}
			if strings.HasSuffix(contentType, "+json") {
				if !produced || got["payload.chunk"] != "e30=" || got["payload.omitted_reason"] != nil {
					t.Fatalf("capture metadata collision: %v", got)
				}
			} else if produced || got["payload.chunk"] != nil || got["payload.redacted"] != nil || got["payload.sha256"] != nil || got["payload.chunk_count"] != nil || got["payload.omitted_reason"] != "unsupported_content" {
				t.Fatalf("omission metadata collision: %v", got)
			}
		})
	}
}
