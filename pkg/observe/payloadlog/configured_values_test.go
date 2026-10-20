package payloadlog_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	"github.com/formancehq/go-libs/v5/pkg/observe/payloadlog"
)

func TestCaptureUsesCallerSanitizationBeforeChunking(t *testing.T) {
	padding := strings.Repeat("x", 64<<10)
	for _, tc := range []struct {
		name, contentType, input, want string
		known                          []string
	}{
		{
			name: "contextual plain text escape", contentType: "text/plain",
			known: []string{"u0061longconfiguredcredential"},
			input: padding + ` detail=\u0061longconfiguredcredential;next=\u0064`,
			want:  padding + ` detail=[REDACTED];next=d`,
		},
		{
			name: "overlapping form values", contentType: "application/x-www-form-urlencoded",
			known: []string{"first-known-prefix/shared-overlap", "shared-overlap/second-known-tail"},
			input: "padding=" + padding + "&note=first-known-prefix%2Fshared-overlap%2Fsecond-known-tail&tail=retained",
			want:  "padding=" + padding + "&note=%5BREDACTED%5D&tail=retained",
		},
		{
			name: "overlapping form values reverse registration", contentType: "application/x-www-form-urlencoded",
			known: []string{"shared-overlap/second-known-tail", "first-known-prefix/shared-overlap"},
			input: "padding=" + padding + "&note=first-known-prefix%2Fshared-overlap%2Fsecond-known-tail&tail=retained",
			want:  "padding=" + padding + "&note=%5BREDACTED%5D&tail=retained",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payloadlog.Configure(true, 0)
			t.Cleanup(func() { payloadlog.Configure(false, 0) })
			payload := []byte(tc.input)
			original := bytes.Clone(payload)
			var buffer bytes.Buffer
			payloadlog.Capture(t.Context(), jsonLogger(&buffer, logging.DebugLevel), payloadlog.Record{
				Sanitize: func(b []byte) ([]byte, bool) {
					if string(b) != tc.input {
						t.Fatal("sanitizer did not receive original bounded payload")
					}
					return []byte(tc.want), true
				},
				Transport: "test", Direction: "receive", ContentType: tc.contentType,
				TrustedPlainText: true, Limit: int64(len(payload)),
				Produce: func(int64) ([]byte, error) { return payload, nil },
			})
			if !bytes.Equal(payload, original) {
				t.Fatal("capture mutated caller payload")
			}
			records := lines(t, &buffer)
			if len(records) < 2 {
				t.Fatalf("got %d records, want multiple capture chunks", len(records))
			}
			var encoded strings.Builder
			for index, record := range records {
				if record["payload.chunk_index"] != float64(index) || record["payload.chunk_count"] != float64(len(records)) {
					t.Fatal("capture chunk ordering or count changed")
				}
				if record["payload.capture_id"] != records[0]["payload.capture_id"] || record["payload.redacted"] != true || record["payload.encoding"] != "base64" {
					t.Fatal("capture identity or encoding changed")
				}
				chunk, ok := record["payload.chunk"].(string)
				if !ok {
					t.Fatal("missing capture chunk")
				}
				encoded.WriteString(chunk)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded.String())
			if err != nil {
				t.Fatalf("decode capture: %v", err)
			}
			if string(decoded) != tc.want {
				t.Error("reassembled payload exposed a configured-value fragment or changed safe content")
			}
			digest := sha256.Sum256(decoded)
			for _, record := range records {
				if record["payload.sha256"] != hex.EncodeToString(digest[:]) || record["payload.rendered_bytes"] != float64(len(decoded)) {
					t.Fatal("capture digest or rendered length does not describe reassembled payload")
				}
			}
		})
	}
}
