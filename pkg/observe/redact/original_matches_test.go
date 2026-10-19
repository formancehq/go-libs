package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestJSONCaptureMasksEscapedBearerSemantics(t *testing.T) {
	ConfigureValues(nil)
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, value := range []string{
		`Bearer abc/secret+tail=`,
		`\u0042earer\u0020abc\/secret+tail=`,
		`\u0042\u0065arer\tabc\/secret+tail=`,
	} {
		input := []byte(`{ "note":"` + value + `", "untouched":"\u0061\/", "number":1e+09 }`)
		original := bytes.Clone(input)
		got, ok := JSON(input)
		if !ok || !json.Valid(got) || bytes.Contains(got, []byte("secret")) || !bytes.Contains(got, []byte(Marker)) {
			t.Fatalf("semantic bearer capture was not masked: %q, %v", got, ok)
		}
		if !bytes.Contains(got, []byte(`, "untouched":"\u0061\/", "number":1e+09 }`)) || !bytes.Equal(input, original) {
			t.Fatalf("capture changed unrelated bytes or input: %q", got)
		}
	}
}

func TestConfiguredMatchesSurviveDiagnosticOverlap(t *testing.T) {
	secret := "Bearer abc configured-private-tail"
	ConfigureValues([]string{secret, "password"})
	t.Cleanup(func() { ConfigureValues(nil) })
	t.Run("diagnostic bytes", func(t *testing.T) {
		input := []byte("unknown=" + secret + "\npassword=unregistered-value\nsafe=retained")
		original := bytes.Clone(input)
		got := Bytes(input)
		if bytes.Contains(got, []byte("configured-private-tail")) || bytes.Contains(got, []byte("unregistered-value")) || !bytes.Contains(got, []byte("safe=retained")) {
			t.Fatalf("overlapping policy exposed a credential or lost safe text: %q", got)
		}
		if !bytes.Equal(input, original) {
			t.Fatal("diagnostics mutated caller bytes")
		}
	})
	t.Run("structured capture", func(t *testing.T) {
		input := []byte(`{ "password":"unregistered-value", "unknown":"` + secret + `", "safe":"\u0061\/", "n":1e+09 }`)
		original := bytes.Clone(input)
		got, ok := JSON(input)
		want := `{ "password":"[REDACTED]", "unknown":"[REDACTED]", "safe":"\u0061\/", "n":1e+09 }`
		if !ok || string(got) != want || !bytes.Equal(input, original) {
			t.Fatalf("capture overlap or original-key classification failed: %q, %v", got, ok)
		}
	})
}

type cappedCredentialReader struct {
	reader io.Reader
	read   int
}

func (r *cappedCredentialReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func TestReaderMasksConfiguredCredentialAtInputCap(t *testing.T) {
	secret := `onlyalphabeticconfiguredcredential/with+symbols`
	ConfigureValues([]string{secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, variant := range valueVariants(secret) {
		for _, visible := range []int{4, len(variant) / 2, len(variant) - 2} {
			const prefix = "unknown="
			limit := len(prefix) + visible
			reader := &cappedCredentialReader{reader: strings.NewReader(prefix + variant + " remainder")}
			got := Reader(reader, limit)
			if strings.Contains(got, variant[:visible]) || !strings.HasPrefix(got, prefix+"[") || !strings.HasSuffix(got, "...[truncated]") {
				t.Fatalf("input cap exposed configured prefix: variant=%q cap=%d output=%q", variant, limit, got)
			}
			if reader.read != limit+1 {
				t.Fatalf("read %d bytes, want bounded detection window %d", reader.read, limit+1)
			}
		}
	}
}

func TestReaderRetainsCompleteAndUnambiguousWindows(t *testing.T) {
	secret := "onlyalphabeticconfiguredcredential"
	ConfigureValues([]string{secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	input := "unknown=" + secret
	reader := &cappedCredentialReader{reader: strings.NewReader(input)}
	if got := Reader(reader, len(input)); got != "unknown="+Marker || reader.read != len(input) {
		t.Fatalf("exact-limit credential or read changed: %q (%d)", got, reader.read)
	}
	reader = &cappedCredentialReader{reader: strings.NewReader("safe=zzzzzzzzzzzz")}
	if got := Reader(reader, 9); got != "safe=zzzz...[truncated]" || reader.read != 10 {
		t.Fatalf("unambiguous truncated window changed: %q (%d)", got, reader.read)
	}
}
