package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestReaderMasksEscapedConfiguredPrefixAtInputCap(t *testing.T) {
	secret := `onlyalphabeticconfiguredcredential/with+symbols`
	ConfigureValues([]string{secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, test := range []struct {
		name, value, visible string
	}{
		{"unicode leading letter", `\u006fnlyalphabeticconfiguredcredential/with+symbols`, `\u006fnlyalph`},
		{"unicode embedded letter", `only\u0061lphabeticconfiguredcredential/with+symbols`, `only\u0061lph`},
		{"multiple unicode letters", `\u006fnly\u0061lphabeticconfiguredcredential/with+symbols`, `\u006fnly\u0061lph`},
		{"escaped slash", `onlyalphabeticconfiguredcredential\/with+symbols`, `onlyalphabeticconfiguredcredential\/wi`},
		{"unicode punctuation", `onlyalphabeticconfiguredcredential\u002fwith\u002bsymbols`, `onlyalphabeticconfiguredcredential\u002fwith\u002bsy`},
	} {
		t.Run(test.name, func(t *testing.T) {
			const prefix = "unknown="
			limit := len(prefix) + len(test.visible)
			reader := &cappedCredentialReader{reader: strings.NewReader(prefix + test.value + " remainder")}
			want := prefix + Marker + "...[truncated]"
			if got := Reader(reader, limit); got != want {
				t.Fatalf("escaped configured prefix was not fully masked: got %q, want %q", got, want)
			}
			if reader.read != limit+1 {
				t.Fatalf("read %d bytes, want bounded detection window %d", reader.read, limit+1)
			}
		})
	}
}

func TestReaderPreservesEscapeInterpretationAndExactWindow(t *testing.T) {
	secret := `onlyalphabeticconfiguredcredential/with+symbols`
	ConfigureValues([]string{secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	input := `unknown=\u006fnlyalphabeticconfiguredcredential\/with+symbols`
	reader := &cappedCredentialReader{reader: strings.NewReader(input)}
	if got := Reader(reader, len(input)); got != "unknown="+Marker || reader.read != len(input) {
		t.Fatalf("complete escaped credential or exact-window read changed: %q (%d)", got, reader.read)
	}
	// An escaped backslash must not become a Unicode escape on a second pass.
	input = `safe=\\u0061\nzzzzzzzzzz`
	reader = &cappedCredentialReader{reader: strings.NewReader(input)}
	const limit = len(`safe=\\u0061\nz`)
	if got := Reader(reader, limit); got != input[:limit]+"...[truncated]" || reader.read != limit+1 {
		t.Fatalf("inert-escape boundary or bounded read changed: %q (%d)", got, reader.read)
	}
}

func TestReaderConfiguredRepresentationsAtEveryEscapeCut(t *testing.T) {
	secret := `onlyAlphabetic09-._~configured/with+symbols`
	ConfigureValues([]string{secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	for variantIndex, variant := range valueVariants(secret) {
		var lower, upper strings.Builder
		for _, c := range []byte(variant) {
			if _, ok := inertEscape([]byte(fmt.Sprintf(`\u%04x`, c))); ok {
				fmt.Fprintf(&lower, `\u%04x`, c)
				fmt.Fprintf(&upper, `\u%04X`, c)
			} else {
				lower.WriteByte(c)
				upper.WriteByte(c)
			}
		}
		for spellingIndex, spelling := range []string{variant, lower.String(), upper.String(), strings.ReplaceAll(variant, "/", `\/`)} {
			for visible := 1; visible < len(spelling); visible++ {
				const prefix = "unknown="
				limit := len(prefix) + visible
				reader := &cappedCredentialReader{reader: strings.NewReader(prefix + spelling + " remainder")}
				want := prefix + Marker
				want = want[:min(len(want), limit)] + "...[truncated]"
				if got := Reader(reader, limit); got != want {
					t.Fatalf("variant %d spelling %d cut %d: got %q, want %q", variantIndex, spellingIndex, visible, got, want)
				}
				if reader.read != limit+1 {
					t.Fatalf("read %d bytes, want %d", reader.read, limit+1)
				}
			}
		}
	}
}

func TestReaderPartialEscapeUnambiguousControls(t *testing.T) {
	ConfigureValues([]string{`onlyalphabeticconfiguredcredential/with+symbols`})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, window := range []string{
		`safe=zzzz\u004`,  // No configured prefix can continue with this escape.
		`safe=only\u004`,  // The configured next character is 'a', not this code prefix.
		`safe=only\\u006`, // The backslash is escaped, not an incomplete Unicode escape.
		`safe=zzzz\x`,     // Unsupported escapes retain their existing interpretation.
	} {
		reader := &cappedCredentialReader{reader: strings.NewReader(window + " remainder")}
		limit := len(window) - 1
		if got := Reader(reader, limit); got != window[:limit]+"...[truncated]" || reader.read != limit+1 {
			t.Fatalf("unambiguous window/read changed: got %q (%d)", got, reader.read)
		}
	}
	ConfigureValues(nil)
	window := `unknown=only\u006`
	if got := Reader(strings.NewReader(window+" remainder"), len(window)-1); got != window[:len(window)-1]+"...[truncated]" {
		t.Fatalf("unconfigured diagnostic contract changed: %q", got)
	}
}

func TestReaderMasksEveryPartialUnicodeEscapeAfterConfiguredPrefix(t *testing.T) {
	ConfigureValues([]string{`onlyalphabeticconfiguredcredential/with+symbols`})
	t.Cleanup(func() { ConfigureValues(nil) })
	const prefix = "unknown="
	const observed = "only"
	const escape = `\u0061`
	for offset := 1; offset < len(escape); offset++ {
		t.Run(fmt.Sprintf("escape-bytes-%d", offset), func(t *testing.T) {
			window := prefix + observed + escape[:offset]
			limit := len(window) - 1
			reader := &cappedCredentialReader{reader: strings.NewReader(prefix + observed + escape + "lphabeticconfiguredcredential/with+symbols remainder")}
			want := prefix + Marker
			want = want[:min(len(want), limit)] + "...[truncated]"
			if got := Reader(reader, limit); got != want {
				t.Fatalf("partial Unicode escape exposed configured prefix: got %q, want %q", got, want)
			}
			if reader.read != limit+1 {
				t.Fatalf("read %d bytes, want %d", reader.read, limit+1)
			}
		})
	}
}

func TestReaderKeepsLiteralConfiguredEscapePrefixesProtected(t *testing.T) {
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, secret := range []string{
		`only\literalconfiguredcredential/with+symbols`,
		`literal\unicodeconfiguredtail`,
		`only\u-prefixliteralconfiguredcredential/with+symbols`,
		`\u-prefixliteralconfiguredcredential/with+symbols`,
	} {
		ConfigureValues([]string{secret})
		for variantIndex, variant := range valueVariants(secret) {
			for visible := 1; visible < len(variant); visible++ {
				const prefix = "unknown="
				limit := len(prefix) + visible
				reader := &cappedCredentialReader{reader: strings.NewReader(prefix + variant + " remainder")}
				want := prefix + Marker
				want = want[:min(len(want), limit)] + "...[truncated]"
				if got := Reader(reader, limit); got != want {
					t.Fatalf("literal credential variant %d cut %d: got %q, want %q", variantIndex, visible, got, want)
				}
				if reader.read != limit+1 {
					t.Fatalf("read %d bytes, want %d", reader.read, limit+1)
				}
			}
		}
	}
}

func TestDiagnosticKnownLiteralEscapesRemainProtected(t *testing.T) {
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, secret := range []string{
		`only\u0061configuredcredential/with+symbols`,
		`only\/configuredcredential/with+symbols`,
		`only\u0041configured\/credential\u002bwithsymbols`,
		`\u0061\u0062\u0063`,                           // Normalization must not lose the long raw value's boundary policy.
		`only\\u0061configuredcredential/with+symbols`, // No recursive decoding.
	} {
		ConfigureValues([]string{secret})
		const prefix = "unknown="
		for variantIndex, variant := range valueVariants(secret) {
			input := []byte(prefix + variant)
			original := bytes.Clone(input)
			t.Run(fmt.Sprintf("Bytes-%d", variantIndex), func(t *testing.T) {
				if got := Bytes(input); string(got) != prefix+Marker || !bytes.Equal(input, original) {
					t.Fatalf("variant %d complete diagnostic exposed a known value or changed input: %q", variantIndex, got)
				}
			})
			reader := &cappedCredentialReader{reader: strings.NewReader(string(input))}
			t.Run(fmt.Sprintf("Reader-%d", variantIndex), func(t *testing.T) {
				if got := Reader(reader, len(input)); got != prefix+Marker || reader.read != len(input) {
					t.Fatalf("variant %d exact-window diagnostic changed: %q (%d)", variantIndex, got, reader.read)
				}
			})
			for limit := len(prefix) + 1; limit < len(input); limit++ {
				reader := &cappedCredentialReader{reader: strings.NewReader(string(input) + " remainder")}
				want := prefix + Marker
				want = want[:min(len(want), limit)] + "...[truncated]"
				if got := Reader(reader, limit); got != want || reader.read != limit+1 {
					t.Fatalf("variant %d cap %d: known literal prefix or read bound changed: %q (%d)", variantIndex, limit, got, reader.read)
				}
			}
		}
		if got := MaskValues(prefix + secret); got != prefix+Marker {
			t.Fatalf("direct literal masking changed: %q", got)
		}
		normalized := string(decodeInertEscapes([]byte(secret)))
		if normalized != secret && MaskValues(prefix+normalized) != prefix+normalized {
			t.Fatal("diagnostic-only normalization leaked into direct masking")
		}
		for _, value := range []string{secret, normalized} {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			input := []byte(`{ "note":` + string(encoded) + `, "safe":"\u0061\/", "n":1e+09 }`)
			want := string(input)
			if value == secret {
				want = `{ "note":"[REDACTED]", "safe":"\u0061\/", "n":1e+09 }`
			}
			original := bytes.Clone(input)
			if got, ok := JSON(input); !ok || string(got) != want || !bytes.Equal(input, original) {
				t.Fatalf("structured raw-value/byte-preservation contract changed: %q (%v)", got, ok)
			}
		}
	}
}

func TestDiagnosticNormalizedBoundaryPolicy(t *testing.T) {
	t.Cleanup(func() { ConfigureValues(nil) })
	ConfigureValues([]string{`\u0061\u0062\u0063`})
	input := []byte(`prefix\u0061\u0062\u0063suffix`)
	if got := string(Bytes(input)); got != "prefix"+Marker+"suffix" {
		t.Fatalf("normalization lost original unbounded matching: %q", got)
	}
	ConfigureValues([]string{`\u0061`})
	if got := string(Bytes([]byte(`unknown=\u0061`))); got != "unknown="+Marker {
		t.Fatalf("isolated short known value was not masked: %q", got)
	}
	if got := string(Bytes([]byte(`safe=c\u0061t`))); got != "safe=cat" {
		t.Fatalf("short normalized value lost token boundaries: %q", got)
	}
	ConfigureValues([]string{`only\\u0061configuredcredential`})
	if got := string(Bytes([]byte(`safe=onlyaconfiguredcredential`))); got != "safe=onlyaconfiguredcredential" {
		t.Fatalf("diagnostic known variants were recursively normalized: %q", got)
	}
}
