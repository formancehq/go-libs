package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestCaptureJSONPreservesUnrelatedBytes(t *testing.T) {
	input := []byte("{\n \"note\":\"\\u0061\\/\", \"integer\":9007199254740997, \"password\":\"hidden\", \"list\":[1e+09,null]\n}\n")
	original := bytes.Clone(input)
	got, ok := JSON(input)
	want := bytes.Replace(input, []byte(`"hidden"`), []byte(`"`+Marker+`"`), 1)
	if !ok || !bytes.Equal(got, want) || !bytes.Equal(input, original) {
		t.Fatalf("capture changed noncredential bytes or input: %q, %v", got, ok)
	}
	for _, value := range []string{`null`, `[]`, `42`, `"text"`, `{"a":1,"a":2}`} {
		got, ok := JSON([]byte(value))
		if !ok || string(got) != value {
			t.Errorf("safe document changed: %q, %v", got, ok)
		}
	}
}

func TestCaptureJSONRefusesAmbiguousCredentialFields(t *testing.T) {
	for _, value := range []string{
		`{"password":1}`, `{"password":false}`, `{"password":null}`,
		`{"password":{}}`, `{"password":[]}`, `{"pass\u0077ord":"x"}`,
		`{"password":"x","Password":"y"}`,
		`{"PASSWORD":"x","pass\u0077ord":"y"}`,
		`{"nested":[{"authorization":true}]}`, `{"sessionToken":[]}`,
		`{"password":"x"} {}`, `{"password":`, `invalid`,
	} {
		if got, ok := JSON([]byte(value)); ok || got != nil {
			t.Errorf("ambiguous capture accepted: %q", value)
		}
	}
	input := `{"first":{"password":"one"},"second":[{"Password":"two"}]}`
	got, ok := JSON([]byte(input))
	if !ok || string(got) != `{"first":{"password":"[REDACTED]"},"second":[{"Password":"[REDACTED]"}]}` {
		t.Fatalf("distinct objects should redact independently: %q", got)
	}
}

func TestConfiguredValuesAcrossEncodingsAndDiagnosticBoundaries(t *testing.T) {
	secret := "test secret/with+symbols&long"
	ConfigureValues([]string{"", Marker, secret})
	t.Cleanup(func() { ConfigureValues(nil) })
	for _, value := range []string{
		secret, url.QueryEscape(secret), url.PathEscape(secret),
		base64.StdEncoding.EncodeToString([]byte(secret)),
		base64.RawStdEncoding.EncodeToString([]byte(secret)),
		base64.URLEncoding.EncodeToString([]byte(secret)),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
	} {
		if got := MaskValues("unknown=" + value); got != "unknown="+Marker {
			t.Fatalf("representation was not masked: %q", got)
		}
		if got := string(Bytes([]byte("unknown=" + value))); strings.Contains(got, value) {
			t.Fatal("diagnostic bytes leaked configured representation")
		}
	}
	ConfigureValues([]string{"1", "abc"})
	if got := MaskValues("build1 1 abc prefixabc abc_suffix \u00e9abc abc\u00e9"); got != "build1 [REDACTED] [REDACTED] prefixabc abc_suffix \u00e9abc abc\u00e9" {
		t.Fatalf("short value boundaries changed: %q", got)
	}
	ConfigureValues(nil)
	if got := MaskValues(secret); got != secret {
		t.Fatal("cleared configuration retained values")
	}
}

func TestConfiguredJSONStringsDecodeEscapesWithoutReserializingOthers(t *testing.T) {
	for _, value := range []struct{ secret, encoded string }{
		{"example-secret-abcdef", `"\u0065xample-secret-abcdef"`},
		{"example/secret-abcdef", `"example\/secret-abcdef"`},
		{"example-😃-secret-abcdef", `"example-\ud83d\ude03-secret-abcdef"`},
	} {
		ConfigureValues([]string{value.secret})
		input := `{"unknown":` + value.encoded + `,"unchanged":"\u0061","n":9007199254740997}`
		got, ok := JSON([]byte(input))
		if !ok || string(got) != `{"unknown":"[REDACTED]","unchanged":"\u0061","n":9007199254740997}` {
			t.Fatalf("semantic string masking failed: %q, %v", got, ok)
		}
	}
	ConfigureValues([]string{"token", "client_secret"})
	t.Cleanup(func() { ConfigureValues(nil) })
	got, ok := JSON([]byte(`{"token":"unknown-secret","other":"token"}`))
	if !ok || string(got) != `{"token":"[REDACTED]","other":"[REDACTED]"}` {
		t.Fatal("value configuration hid field-name classification")
	}
	if got := string(Bytes([]byte(`{"token":"unknown-secret"}`))); strings.Contains(got, "unknown-secret") {
		t.Fatal("configured name hid diagnostic credential")
	}
}

func TestCaptureFormPreservesPairOrderAndEncoding(t *testing.T) {
	ConfigureValues([]string{"example/secret-abcdef"})
	t.Cleanup(func() { ConfigureValues(nil) })
	got, ok := Form([]byte("&&same=%41&detail=%65xample%2fsecret-abcdef&client%5fsecret=hidden&same=B&empty=&"))
	if !ok || string(got) != "&&same=%41&detail=%5BREDACTED%5D&client%5fsecret=%5BREDACTED%5D&same=B&empty=&" {
		t.Fatalf("form changed noncredential bytes: %q, %v", got, ok)
	}
	for _, input := range []string{"secret=%xx", "%xx=x", "x=y;z=q"} {
		if got, ok := Form([]byte(input)); ok || got != nil {
			t.Fatalf("malformed form accepted: %q", input)
		}
	}
}

func TestConfiguredValuesApplyToHTTPDiagnostics(t *testing.T) {
	secret := "example-host-secret"
	ConfigureValues([]string{secret, "1"})
	t.Cleanup(func() { ConfigureValues(nil) })
	u, err := url.Parse("https://user:password@" + secret + ".test/v1/1?cursor=1&version=v1")
	if err != nil {
		t.Fatal(err)
	}
	got := URL(u)
	if got != "https://[REDACTED].test/v1/%5BREDACTED%5D?cursor=%5BREDACTED%5D&version=v1" {
		t.Fatalf("configured URL masking: %q", got)
	}
	h := http.Header{"X-Detail": {secret}, "Authorization": {"credential"}}
	if got := Headers(h); strings.Contains(got, secret) || strings.Contains(got, "credential") {
		t.Fatal("header diagnostic leaked a credential")
	}
	if h.Get("X-Detail") != secret {
		t.Fatal("header was mutated")
	}
	if got := Reader(strings.NewReader(`{"password":"`+strings.Repeat("x", 100)+`"}`), 32); strings.Contains(got, "xxxx") || !strings.Contains(got, Marker) {
		t.Fatalf("bounded reader leaked partial credential: %q", got)
	}
}

func TestConfiguredJSONEscapingAndAtomicReplacement(t *testing.T) {
	secret := "quote\"slash\\<&long-secret"
	ConfigureValues([]string{secret})
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if got := MaskValues(string(encoded[1 : len(encoded)-1])); got != Marker {
		t.Fatal("JSON representation not masked")
	}
	ConfigureValues(nil)
	first, second := "atomic-first-secret-value", "atomic-second-secret-value"
	input := first + " " + second
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 1000 {
			ConfigureValues([]string{first, second})
			ConfigureValues(nil)
		}
	})
	wg.Go(func() {
		for range 1000 {
			got := MaskValues(input)
			if got != input && got != Marker+" "+Marker {
				t.Error("observed partial configuration")
			}
		}
	})
	wg.Wait()
	ConfigureValues(nil)
}

func TestUnconfiguredHeaderValuesRetainExistingSemantics(t *testing.T) {
	ConfigureValues(nil)
	if HeaderValues("X-Detail", nil) != nil {
		t.Fatal("nil values changed")
	}
	values := []string{"diagnostic"}
	got := HeaderValues("X-Detail", values)
	if &got[0] != &values[0] {
		t.Fatal("unconfigured passthrough changed")
	}
	ConfigureValues([]string{"diagnostic"})
	t.Cleanup(func() { ConfigureValues(nil) })
	masked := HeaderValues("X-Detail", values)
	if masked[0] != Marker || values[0] != "diagnostic" {
		t.Fatal("configured masking mutated header")
	}
}
