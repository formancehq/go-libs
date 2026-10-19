// Package payloadlog emits bounded, secret-redacted application payloads as
// reassemblable Debug log records.
//
// Capture is deliberately gated twice: the logger must enable Debug and the
// operator must explicitly enable capture with Configure. Transport
// adapters provide payloads lazily so disabled capture performs no buffering,
// marshalling, or redaction.
package payloadlog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"mime"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
	golibsredact "github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

const chunkBytes = 64 << 10

type config struct {
	enabled bool
	maxBody int64
}

// runtimeConfig contains an atomically replaced process-wide startup snapshot.
// Hosts configure capture before starting transports.
//
//nolint:gochecknoglobals // immutable snapshots are atomically replaced during command startup
var runtimeConfig atomic.Pointer[config]

// Configure applies host startup values before transports start. A negative
// maximum is retained so Capture can fail closed with an observable omission.
func Configure(enabled bool, maxBody int64) {
	runtimeConfig.Store(&config{enabled: enabled, maxBody: maxBody})
}

// Produce returns one complete payload already bounded by the transport adapter.
// limit is the lower of the adapter's hard ceiling and the optional operator
// ceiling. A streaming adapter should read at most limit+1 bytes so Capture can
// distinguish an exact-limit payload from an overrun without unbounded buffering.
// Capture never calls Produce unless both payload capture and Debug logging are on.
type Produce func(limit int64) ([]byte, error)

// Record describes one application payload crossing a transport seam.
type Record struct {
	Transport   string
	Direction   string
	Operation   string
	ContentType string
	// TrustedPlainText permits text/plain only for fixed, locally-produced
	// messages. Provider-controlled unstructured text stays metadata-only.
	TrustedPlainText bool
	// TrustedEmpty permits a locally-known empty body with no media type. If the
	// producer returns any bytes, capture still fails closed.
	TrustedEmpty bool
	// Limit is the transport adapter's hard ceiling. It must be positive. The
	// optional configured cap can only lower it.
	Limit   int64
	Produce Produce
	// Fields supplies caller context; the payload.* namespace is reserved.
	Fields map[string]any
}

// Enabled reports whether logger and the explicit operator gate both allow
// payload capture.
func Enabled(ctx context.Context, logger logging.Logger) bool {
	if logger == nil || !logger.Enabled(logging.DebugLevel) {
		return false
	}

	cfg := runtimeConfig.Load()

	return cfg != nil && cfg.enabled
}

// Capture emits record without affecting the transport operation. Diagnostic
// failures are represented as metadata-only Debug records and are never returned
// to the caller.
func Capture(ctx context.Context, logger logging.Logger, record Record) {
	if !Enabled(ctx, logger) {
		return
	}
	captureID, err := newCaptureID()
	if err != nil {
		omit(ctx, logger, record, "capture_id_failed", "", -1)

		return
	}

	limit, err := effectiveLimit(record.Limit)
	if err != nil {
		omit(ctx, logger, record, "invalid_limit", captureID, -1)

		return
	}
	if record.Produce == nil {
		omit(ctx, logger, record, "missing_producer", captureID, -1)

		return
	}
	kind, ok := classifyContent(record)
	if !ok {
		omit(ctx, logger, record, "unsupported_content", captureID, -1)

		return
	}

	payload, err := record.Produce(limit)
	if err != nil {
		omit(ctx, logger, record, "produce_failed", captureID, -1)

		return
	}
	if int64(len(payload)) > limit {
		omit(ctx, logger, record, "limit_exceeded", captureID, len(payload))

		return
	}
	redacted, ok := safePayload(kind, payload)
	if !ok {
		omit(ctx, logger, record, "unsupported_content", captureID, len(payload))

		return
	}

	digest := sha256.Sum256(redacted)
	encoded := base64.StdEncoding.EncodeToString(redacted)

	chunks := len(encoded) / chunkBytes
	if len(encoded)%chunkBytes != 0 {
		chunks++
	}
	chunks = max(1, chunks)
	for index := range chunks {
		start := index * chunkBytes
		end := start + min(chunkBytes, len(encoded)-start)
		chunk := ""
		if start < len(encoded) {
			chunk = encoded[start:end]
		}

		fields := baseFields(record)
		fields["payload.capture_id"] = captureID
		fields["payload.chunk_index"] = index
		fields["payload.chunk_count"] = chunks
		fields["payload.original_bytes"] = len(payload)
		fields["payload.rendered_bytes"] = len(redacted)
		fields["payload.sha256"] = hex.EncodeToString(digest[:])
		fields["payload.encoding"] = "base64"
		fields["payload.redacted"] = true
		fields["payload.chunk"] = chunk

		logger.WithContext(ctx).WithFields(fields).Debug("payload capture")
	}
}

func effectiveLimit(adapterLimit int64) (int64, error) {
	if adapterLimit <= 0 {
		return 0, fmt.Errorf("adapter limit must be positive")
	}

	cfg := runtimeConfig.Load()
	if cfg == nil || cfg.maxBody == 0 {
		return adapterLimit, nil
	}
	if cfg.maxBody < 0 {
		return 0, fmt.Errorf("capture maximum must be zero or a positive byte count")
	}

	return min(adapterLimit, cfg.maxBody), nil
}

type contentKind uint8

const (
	contentEmpty contentKind = iota
	contentJSON
	contentForm
	contentPlain
)

func classifyContent(record Record) (contentKind, bool) {
	if strings.TrimSpace(record.ContentType) == "" {
		return contentEmpty, record.TrustedEmpty
	}
	mediaType, _, err := mime.ParseMediaType(record.ContentType)
	if err != nil {
		return 0, false
	}
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return contentJSON, true
	case mediaType == "application/x-www-form-urlencoded":
		return contentForm, true
	case mediaType == "text/plain" && record.TrustedPlainText:
		return contentPlain, true
	default:
		return 0, false
	}
}

func safePayload(kind contentKind, payload []byte) ([]byte, bool) {
	if len(payload) == 0 {
		return []byte{}, true
	}
	if !utf8.Valid(payload) {
		return nil, false
	}
	switch kind {
	case contentJSON:
		return golibsredact.JSON(payload)
	case contentForm:
		return golibsredact.Form(payload)
	case contentPlain:
		return golibsredact.Bytes(payload), true
	case contentEmpty:
		return nil, false
	default:
		return nil, false
	}
}

func newCaptureID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate capture id: %w", err)
	}

	return hex.EncodeToString(id[:]), nil
}

func omit(ctx context.Context, logger logging.Logger, record Record, reason, captureID string, originalBytes int) {
	fields := baseFields(record)
	fields["payload.capture_id"] = captureID
	fields["payload.original_bytes"] = originalBytes
	fields["payload.omitted_reason"] = reason
	logger.WithContext(ctx).WithFields(fields).Debug("payload capture omitted")
}

func baseFields(record Record) map[string]any {
	fields := maps.Clone(record.Fields)
	if fields == nil {
		fields = make(map[string]any)
	}
	// Capture owns this namespace for both chunks and omissions. Caller
	// context survives, but stale capture claims must not cross outcomes.
	for key := range fields {
		if strings.HasPrefix(key, "payload.") {
			delete(fields, key)
		}
	}
	fields["payload.transport"] = record.Transport
	fields["payload.direction"] = record.Direction
	fields["payload.operation"] = record.Operation
	fields["payload.content_type"] = safeContentType(record.ContentType)
	return fields
}

func safeContentType(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	// Check original bytes before MIME parsing lowercases the media type and
	// destroys an exact configured-value match. This only affects metadata;
	// classifyContent still owns admission using the original ContentType.
	if golibsredact.MaskValues(value) != value {
		return "[REDACTED]"
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "invalid"
	}

	return string(golibsredact.Bytes([]byte(mediaType)))
}
