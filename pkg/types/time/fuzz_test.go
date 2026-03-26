package time

import (
	"testing"
	stdtime "time"
)

func FuzzParseTime(f *testing.F) {
	f.Add("2021-01-01T00:00:00Z")
	f.Add("2021-01-01T00:00:00.000000000Z")
	f.Add("2021-12-31T23:59:59.999999999Z")
	f.Add("2000-01-01T00:00:00+01:00")
	f.Add("1970-01-01T00:00:00Z")

	// Edge cases
	f.Add("")
	f.Add("not-a-date")
	f.Add("2021-13-01T00:00:00Z")
	f.Add("2021-01-32T00:00:00Z")
	f.Add("2021-01-01")
	f.Add("2021-01-01T25:00:00Z")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic
		parsed, err := ParseTime(input)
		if err != nil {
			return
		}

		// Round-trip: marshal and reparse
		formatted := parsed.Format(DateFormat)
		reparsed, err := ParseTime(formatted)
		if err != nil {
			t.Fatalf("round-trip failed: %q -> %q -> %v", input, formatted, err)
		}

		if !parsed.Equal(reparsed) {
			t.Errorf("round-trip mismatch: %v vs %v", parsed, reparsed)
		}
	})
}

func FuzzTimeUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`"2021-01-01T00:00:00Z"`))
	f.Add([]byte(`"2021-01-01T00:00:00.000000000Z"`))
	f.Add([]byte(`""`))
	f.Add([]byte(`"not-a-date"`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`42`))
	f.Add([]byte(`"2021-01-01"`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var ts Time
		// Must not panic
		_ = ts.UnmarshalJSON(data)
	})
}

func FuzzTimeScan(f *testing.F) {
	f.Add("2021-01-01T00:00:00Z")
	f.Add("2021-01-01T00:00:00.000000000Z")
	f.Add("")
	f.Add("not-a-date")
	f.Add("2021-13-01T00:00:00Z")

	f.Fuzz(func(t *testing.T, input string) {
		var ts Time

		// Scan from string — must not panic
		errStr := ts.Scan(input)

		// Scan from []byte — must not panic
		var ts2 Time
		errBytes := ts2.Scan([]byte(input))

		// Both paths should agree on success/failure
		if (errStr == nil) != (errBytes == nil) {
			t.Errorf("string/bytes scan disagree: string=%v bytes=%v", errStr, errBytes)
		}

		if errStr != nil {
			return
		}

		// Round-trip via Value
		val, err := ts.Value()
		if err != nil {
			t.Fatalf("Value() failed: %v", err)
		}

		var ts3 Time
		if err := ts3.Scan(val); err != nil {
			t.Fatalf("round-trip Scan failed: %v", err)
		}

		if !ts.Time.Round(stdtime.Microsecond).Equal(ts3.Time.Round(stdtime.Microsecond)) {
			t.Errorf("round-trip mismatch: %v vs %v", ts, ts3)
		}
	})
}
