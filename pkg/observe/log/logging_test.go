package logging

import (
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{"trace", TraceLevel, false},
		{"TRACE", TraceLevel, false},
		{"  Trace  ", TraceLevel, false},
		{"debug", DebugLevel, false},
		{"Debug", DebugLevel, false},
		{"info", InfoLevel, false},
		{"INFO", InfoLevel, false},
		{"error", ErrorLevel, false},
		{"Error", ErrorLevel, false},
		{"warn", 0, true},
		{"", 0, true},
		{"verbose", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLevel(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) returned error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level Level
		want  string
	}{
		{TraceLevel, "trace"},
		{DebugLevel, "debug"},
		{InfoLevel, "info"},
		{ErrorLevel, "error"},
		{Level(42), "level(42)"},
	}

	for _, tc := range cases {
		got := tc.level.String()
		if got != tc.want {
			t.Errorf("Level(%d).String() = %q, want %q", int(tc.level), got, tc.want)
		}
	}
}

func TestLevelOrdering(t *testing.T) {
	t.Parallel()

	// Trace is more verbose than Debug, which is more verbose than Info, etc.
	// The numeric ordering matters because callers compare levels directly
	// and adapters map them to backend levels.
	if !(TraceLevel < DebugLevel) {
		t.Errorf("TraceLevel (%d) should be < DebugLevel (%d)", TraceLevel, DebugLevel)
	}
	if !(DebugLevel < InfoLevel) {
		t.Errorf("DebugLevel (%d) should be < InfoLevel (%d)", DebugLevel, InfoLevel)
	}
	if !(InfoLevel < ErrorLevel) {
		t.Errorf("InfoLevel (%d) should be < ErrorLevel (%d)", InfoLevel, ErrorLevel)
	}
}
