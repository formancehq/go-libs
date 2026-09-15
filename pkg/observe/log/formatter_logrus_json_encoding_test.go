package logging

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// roundTrip encodes s with the hand-rolled writer and decodes it with
// encoding/json. The bytes deliberately differ from json.Marshal's -- this
// writer does not HTML-escape, matching zap rather than encoding/json -- so
// the contract asserted here is that the value survives, not that the encoding
// is identical.
func roundTrip(t *testing.T, s string) string {
	t.Helper()

	var buf bytes.Buffer
	writeJSONString(&buf, s)

	var got string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("writeJSONString(%q) produced undecodable JSON %s: %v", s, buf.String(), err)
	}

	return got
}

func TestWriteJSONStringRoundTrips(t *testing.T) {
	for _, s := range []string{
		"",
		"plain",
		`with "quotes"`,
		`back\slash`,
		"new\nline",
		"carriage\rreturn",
		"tab\there",
		"bell\x07and\x00nul",
		"unicode: é à ü 日本語",
		"emoji: 🚀",
		"html: <script> & </script>",
		`{"nested":"json"}`,
		"https://example.test/a?b=c&d=e",
		strings.Repeat("long", 500),
	} {
		if got := roundTrip(t, s); got != s {
			t.Fatalf("round trip changed the value:\n in: %q\nout: %q", s, got)
		}
	}
}

// Invalid UTF-8 would otherwise produce a string no decoder accepts.
func TestWriteJSONStringReplacesInvalidUTF8(t *testing.T) {
	got := roundTrip(t, "bad\xff\xfebytes")

	if !strings.Contains(got, "�") {
		t.Fatalf("invalid UTF-8 must become the replacement character, got %q", got)
	}
	if !strings.HasPrefix(got, "bad") || !strings.HasSuffix(got, "bytes") {
		t.Fatalf("surrounding text must survive, got %q", got)
	}
}

func FuzzWriteJSONString(f *testing.F) {
	for _, s := range []string{"", "a", `"`, "\\", "\n", "é", "🚀", "<&>", "\x00"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		var buf bytes.Buffer
		writeJSONString(&buf, s)

		var got string
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("undecodable JSON for %q: %s (%v)", s, buf.String(), err)
		}

		// Valid input must survive byte for byte; invalid UTF-8 is allowed to
		// change, since it cannot be represented in a JSON string at all.
		if utf8Valid(s) && got != s {
			t.Fatalf("round trip changed a valid string:\n in: %q\nout: %q", s, got)
		}
	})
}

func utf8Valid(s string) bool {
	return utf8.ValidString(s)
}

// The scalar fast paths must agree with encoding/json on the value.
func TestWriteJSONValueMatchesEncodingJSON(t *testing.T) {
	for _, v := range []any{
		nil, true, false,
		"string", int(42), int8(-8), int16(-16), int32(-32), int64(-64),
		uint(7), uint8(8), uint16(16), uint32(32), uint64(64),
		3.5, float32(1.25),
		map[string]any{"k": "v"}, []int{1, 2, 3},
	} {
		var buf bytes.Buffer
		if err := writeJSONValue(&buf, v); err != nil {
			t.Fatalf("writeJSONValue(%#v): %v", v, err)
		}

		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal(%#v): %v", v, err)
		}

		var gotVal, wantVal any
		if err := json.Unmarshal(buf.Bytes(), &gotVal); err != nil {
			t.Fatalf("undecodable output for %#v: %s (%v)", v, buf.String(), err)
		}
		if err := json.Unmarshal(want, &wantVal); err != nil {
			t.Fatal(err)
		}

		if !jsonEqual(gotVal, wantVal) {
			t.Fatalf("value %#v encoded as %v, encoding/json gives %v", v, gotVal, wantVal)
		}
	}
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)

	return bytes.Equal(ab, bb)
}

// A duration is an int64 of nanoseconds in both stacks; encoding/json would
// agree, but the fast path has to not turn it into a string.
func TestWriteJSONValueEncodesDurationsAsNanoseconds(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONValue(&buf, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	if buf.String() != "250000000" {
		t.Fatalf("duration encoded as %s", buf.String())
	}
}

func TestWriteJSONValueDegradesUnmarshalableValues(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONValue(&buf, math.Inf(1)); err != nil {
		t.Fatalf("an unmarshalable value must not fail the record: %v", err)
	}

	var got string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("fallback must still be valid JSON: %s", buf.String())
	}
}

// Format returns a slice logrus keeps, so it must not alias the pooled buffer.
func TestFormatIsSafeForConcurrentUseAndDoesNotAliasThePool(t *testing.T) {
	f := &sharedJSONFormatter{}

	const writers = 16
	results := make([][]byte, writers)

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		go func() {
			defer wg.Done()

			entry := benchEntry(map[string]any{"worker": i})
			entry.Message = strings.Repeat("m", i+1)

			out, err := f.Format(entry)
			if err != nil {
				t.Error(err)

				return
			}
			results[i] = out
		}()
	}
	wg.Wait()

	for i, out := range results {
		var record map[string]any
		if err := json.Unmarshal(out, &record); err != nil {
			t.Fatalf("record %d is not valid JSON: %s", i, out)
		}
		if record["msg"] != strings.Repeat("m", i+1) {
			t.Fatalf("record %d was overwritten by another goroutine: %v", i, record)
		}
		if int(record["worker"].(float64)) != i {
			t.Fatalf("record %d carries another goroutine's field: %v", i, record)
		}
	}
}
