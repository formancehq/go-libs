package redact

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestConfiguredRangeCrossesBothEscapeEdges(t *testing.T) {
	// The known raw value starts inside the first escape and ends inside the
	// last. Neither normalized matching alone nor rounding both endpoints in
	// the same direction can protect the whole original credential.
	ConfigureValues([]string{`u0061longconfiguredcredential\u00`})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, test := range []struct{ input, want string }{
		{`detail=\u0061longconfiguredcredential\u0062suffix`, `detail=[REDACTED]suffix`},
		{`\u0078\/ detail=\u0061longconfiguredcredential\u0062suffix`, `x/ detail=[REDACTED]suffix`},
		{`\\u0078\u0022 detail=\u0061longconfiguredcredential\u0062suffix\u007a`, `\\u0078\u0022 detail=[REDACTED]suffixz`},
	} {
		input := []byte(test.input)
		original := bytes.Clone(input)
		if got := string(Bytes(input)); got != test.want {
			t.Fatalf("escape-edge mapping: got %q, want %q", got, test.want)
		}
		if !bytes.Equal(input, original) {
			t.Fatal("diagnostic mapping mutated caller bytes")
		}
		if got := Reader(strings.NewReader(test.input), len(test.input)); got != test.want {
			t.Fatalf("exact-window mapping: got %q, want %q", got, test.want)
		}
	}
}

// Keep both match count and escape count proportional to input size. A mapping
// that rescans the escape list for each endpoint becomes quadratic here. Record
// throughput and allocations for comparison; timing assertions are deliberately
// avoided because compiler, race instrumentation and runners change wall time.
func BenchmarkConfiguredEscapeMapping(b *testing.B) {
	ConfigureValues([]string{"aaaaaaaaaaaaaaaa"})
	b.Cleanup(func() { ConfigureValues(nil) })
	for _, blocks := range []int{128, 512, 2048, 8192} {
		b.Run(fmt.Sprintf("blocks-%d", blocks), func(b *testing.B) {
			input := []byte(strings.Repeat(`aaaaaaaaaaaaaaaa\u0061`, blocks))
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				Bytes(input)
			}
		})
	}
}
