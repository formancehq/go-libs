package currency

import (
	"math/big"
	"testing"
)

func FuzzGetAmountWithPrecisionFromString(f *testing.F) {
	// Seed corpus from existing test cases
	f.Add("123.45", 2)
	f.Add("0.00", 2)
	f.Add("123.", 0)
	f.Add("123.4567", 4)
	f.Add("123", 2)
	f.Add("123", 0)
	f.Add("0", 0)

	// Edge cases
	f.Add("", 0)
	f.Add("123.45.67", 2)
	f.Add("123.4a", 2)
	f.Add("12a3.4", 2)
	f.Add("123a", 2)
	f.Add("999999999999999999999999999999", 10)
	f.Add("0.000000000000000001", 18)
	f.Add("-123.45", 2)
	f.Add("+123.45", 2)
	f.Add(".", 0)
	f.Add(".5", 1)

	f.Fuzz(func(t *testing.T, amountString string, precision int) {
		// Bound precision to avoid OOM from huge zero-padding
		if precision < 0 || precision > 30 {
			return
		}

		// Must not panic
		result, err := GetAmountWithPrecisionFromString(amountString, precision)
		if err != nil {
			return
		}

		if result == nil {
			t.Fatal("nil result without error")
		}
	})
}

func FuzzAmountRoundTrip(f *testing.F) {
	// Seed corpus: (amount string, precision) pairs that should round-trip
	f.Add("123.45", 2)
	f.Add("0.00", 2)
	f.Add("123.4567", 4)
	f.Add("123", 0)
	f.Add("0", 0)
	f.Add("1.00", 2)
	f.Add("0.01", 2)
	f.Add("999.999", 3)
	f.Add("0.000001", 6)

	f.Fuzz(func(t *testing.T, amountString string, precision int) {
		if precision < 0 || precision > 30 {
			return
		}

		// Parse string -> big.Int
		parsed, err := GetAmountWithPrecisionFromString(amountString, precision)
		if err != nil {
			return
		}

		// Convert back big.Int -> string
		backToString, err := GetStringAmountFromBigIntWithPrecision(parsed, precision)
		if err != nil {
			t.Fatalf("round-trip serialize failed: %v", err)
		}

		// Re-parse the serialized string
		reparsed, err := GetAmountWithPrecisionFromString(backToString, precision)
		if err != nil {
			t.Fatalf("round-trip reparse failed for %q: %v", backToString, err)
		}

		// Values must match
		if parsed.Cmp(reparsed) != 0 {
			t.Errorf("round-trip mismatch: %q (precision %d) -> %s -> %q -> %s",
				amountString, precision, parsed.String(), backToString, reparsed.String())
		}
	})
}

func FuzzGetStringAmountFromBigIntWithPrecision(f *testing.F) {
	// Seed corpus: (int64 value, precision)
	f.Add(int64(12345), 2)
	f.Add(int64(0), 0)
	f.Add(int64(0), 2)
	f.Add(int64(123), 6)
	f.Add(int64(1), 18)
	f.Add(int64(-12345), 2)
	f.Add(int64(9999999999), 5)

	f.Fuzz(func(t *testing.T, value int64, precision int) {
		if precision < 0 || precision > 30 {
			return
		}

		amount := big.NewInt(value)

		// Must not panic
		result, err := GetStringAmountFromBigIntWithPrecision(amount, precision)
		if err != nil {
			return
		}

		if result == "" {
			t.Fatal("empty result without error")
		}
	})
}
