package currency

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"testing"
)

// decimalGrammar is an independent oracle for the accepted input syntax: an
// optional single leading sign, ASCII digits and at most one dot, with at
// least one digit.
var decimalGrammar = regexp.MustCompile(`^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)$`)

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

	// Inputs that must be rejected: digitless, or a sign after the first character
	f.Add(".", 2)
	f.Add("-", 2)
	f.Add("+.", 2)
	f.Add(".-5", 2)
	f.Add(".+5", 3)
	f.Add("1.-5", 2)
	f.Add("1-", 2)
	f.Add("--1", 2)

	f.Fuzz(func(t *testing.T, amountString string, precision int) {
		if precision < 0 || precision > 30 {
			return
		}

		// Parse string -> big.Int
		parsed, err := GetAmountWithPrecisionFromString(amountString, precision)
		if err != nil {
			return
		}

		// Round-trip alone cannot catch a digitless input parsed as zero
		if !decimalGrammar.MatchString(amountString) {
			t.Fatalf("accepted malformed amount %q (precision %d) as %s", amountString, precision, parsed.String())
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

func FuzzParseMinorUnitsRoundTrip(f *testing.F) {
	// Seed corpus: (amount string, precision) pairs
	f.Add("123.45", 2)
	f.Add("1.230", 2)
	f.Add("1.235", 2)
	f.Add("-1.23", 2)
	f.Add("-0.00", 2)
	f.Add("+.5", 1)
	f.Add("1.", 2)
	f.Add("007.5", 1)
	f.Add(".00", 0)
	f.Add("1", MaxPrecision)
	f.Add("", 0)
	f.Add(".", 2)
	f.Add("-", 2)
	f.Add(".-5", 2)
	f.Add("1.-5", 2)
	f.Add(" 1", 2)

	f.Fuzz(func(t *testing.T, amount string, precision int) {
		if precision < 0 || precision > MaxPrecision {
			return
		}

		parsed, err := ParseMinorUnits(amount, precision)

		// Independent exactness oracle: amount * 10^precision as a rational
		if decimalGrammar.MatchString(amount) {
			scaled, ok := new(big.Rat).SetString(amount)
			if !ok {
				t.Fatalf("big.Rat rejects %q", amount)
			}
			scaled.Mul(scaled, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)))
			switch {
			case scaled.IsInt() && (err != nil || scaled.Num().Cmp(parsed) != 0):
				t.Fatalf("%q (precision %d): exact value %s, ParseMinorUnits gives %v, %v",
					amount, precision, scaled.Num().String(), parsed, err)
			case !scaled.IsInt() && !errors.Is(err, ErrPrecisionLoss):
				t.Fatalf("%q (precision %d): inexact value, ParseMinorUnits gives %v, %v",
					amount, precision, parsed, err)
			}
		}

		// Wherever GetAmountWithPrecisionFromString accepts, ParseMinorUnits must agree
		legacy, legacyErr := GetAmountWithPrecisionFromString(amount, precision)
		if legacyErr == nil && (err != nil || legacy.Cmp(parsed) != 0) {
			t.Fatalf("%q (precision %d): GetAmountWithPrecisionFromString gives %s, ParseMinorUnits gives %v, %v",
				amount, precision, legacy.String(), parsed, err)
		}

		if err != nil {
			return
		}

		// Without digits beyond the precision, GetAmountWithPrecisionFromString must accept too
		if _, fraction, _ := strings.Cut(amount, "."); len(fraction) <= precision && legacyErr != nil {
			t.Fatalf("%q (precision %d): ParseMinorUnits gives %s, GetAmountWithPrecisionFromString fails: %v",
				amount, precision, parsed.String(), legacyErr)
		}

		if !decimalGrammar.MatchString(amount) {
			t.Fatalf("accepted malformed amount %q (precision %d) as %s", amount, precision, parsed.String())
		}

		// Convert back big.Int -> string
		backToString, err := GetStringAmountFromBigIntWithPrecision(parsed, precision)
		if err != nil {
			t.Fatalf("round-trip serialize failed: %v", err)
		}

		// Re-parse the serialized string
		reparsed, err := ParseMinorUnits(backToString, precision)
		if err != nil {
			t.Fatalf("round-trip reparse failed for %q: %v", backToString, err)
		}

		// Values must match
		if parsed.Cmp(reparsed) != 0 {
			t.Errorf("round-trip mismatch: %q (precision %d) -> %s -> %q -> %s",
				amount, precision, parsed.String(), backToString, reparsed.String())
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
