package currency

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"
)

// This package provides a way to convert a string amount to a big.Int
// with a given precision. The precision is the number of decimals that
// the amount has. For example, if the amount is 123.45 and the precision is 2,
// the amount will be 12345.
// We also provide a way to convert a big.Int to a string amount with a given
// precision.
// We developed this package because we need to convert amounts from strings,
// and apply the precision to them to have minor units. If we do that with
// the big package using floats and divisions/multiplcations, we will
// sometimes lose precision, which is unacceptable.

var (
	// ErrInvalidAmount is returned when the amount is invalid
	ErrInvalidAmount = fmt.Errorf("invalid amount")
	// ErrInvalidPrecision is returned when the precision is inferior to the
	// number of decimals in the amount or negative, and by ParseMinorUnits
	// when it is above MaxPrecision
	ErrInvalidPrecision = fmt.Errorf("invalid precision")
	// ErrPrecisionLoss is returned when an amount carries a non-zero digit beyond the precision.
	ErrPrecisionLoss = fmt.Errorf("precision loss")
)

// MaxPrecision is the largest precision a Formance Ledger asset can carry ("USD/255").
const MaxPrecision = 255

func GetAmountWithPrecisionFromString(amountString string, precision int) (*big.Int, error) {
	if precision < 0 {
		return nil, fmt.Errorf("precision is negative: %d: %w", precision, ErrInvalidPrecision)
	}

	if amountString == "" {
		return nil, fmt.Errorf("amount string is empty: %w", ErrInvalidAmount)
	}

	parts := strings.Split(amountString, ".")
	lengthParts := len(parts)

	if lengthParts > 2 || lengthParts == 0 {
		// More than one dot, invalid amount
		return nil, fmt.Errorf("got multiple dots in amount: %s: %w", amountString, ErrInvalidAmount)
	}

	if lengthParts == 2 && len(parts[1]) > precision {
		// The decimal part is longer than the precision, we have to send an
		// error because we don't want to lose the precision
		return nil, ErrInvalidPrecision
	}

	if _, _, _, ok := splitDecimal(amountString); !ok {
		// Rejects digitless amounts and signs anywhere but the first character,
		// which big.Int.SetString would accept once the parts are concatenated
		return nil, fmt.Errorf("amount is not a decimal number: %s: %w", amountString, ErrInvalidAmount)
	}

	if lengthParts == 1 {
		// No dot, which means it's an integer
		for range precision {
			amountString += "0"
		}
		res, ok := new(big.Int).SetString(amountString, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount: %s: %w", amountString, ErrInvalidAmount)
		}
		return res, nil
	}

	// Here we are in the case where we have one dot, which means we have a
	// decimal amount. The decimal part is at most as long as the precision, we
	// add zeros at its end up to the precision and concatenate the two parts
	decimalPart := parts[1] + strings.Repeat("0", precision-len(parts[1]))
	res, ok := new(big.Int).SetString(parts[0]+decimalPart, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount computed: %s from amount %s: %w", parts[0]+decimalPart, amountString, ErrInvalidAmount)
	}
	return res, nil
}

// ParseMinorUnits converts a decimal string to its exact integer value at precision.
// Unlike GetAmountWithPrecisionFromString, fractional zeros beyond the precision are exact
// and accepted ("1.230" at precision 2 is 123); a non-zero digit beyond it is ErrPrecisionLoss.
func ParseMinorUnits(amount string, precision int) (*big.Int, error) {
	if precision < 0 || precision > MaxPrecision {
		return nil, fmt.Errorf("precision out of range [0, %d]: %d: %w", MaxPrecision, precision, ErrInvalidPrecision)
	}

	if amount == "" {
		return nil, fmt.Errorf("amount string is empty: %w", ErrInvalidAmount)
	}

	sign, integer, fraction, ok := splitDecimal(amount)
	if !ok {
		return nil, fmt.Errorf("amount is not a decimal number: %s: %w", amount, ErrInvalidAmount)
	}

	if len(fraction) > precision {
		// Digits beyond the precision are exact only when they are all zeros
		if strings.Trim(fraction[precision:], "0") != "" {
			return nil, fmt.Errorf("amount %s has non-zero digits beyond precision %d: %w", amount, precision, ErrPrecisionLoss)
		}
		fraction = fraction[:precision]
	}

	digits := integer + fraction + strings.Repeat("0", precision-len(fraction))
	if digits == "" {
		// Only zeros beyond the precision, e.g. ".00" at precision 0
		return new(big.Int), nil
	}

	res, ok := new(big.Int).SetString(sign+digits, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount computed: %s from amount %s: %w", sign+digits, amount, ErrInvalidAmount)
	}
	return res, nil
}

// splitDecimal splits a decimal string into its sign, integer digits and
// fractional digits. It accepts an optional single leading '+' or '-', ASCII
// digits and at most one dot, with at least one digit overall; ok is false for
// anything else.
func splitDecimal(amount string) (sign, integer, fraction string, ok bool) {
	rest := amount
	if rest != "" && (rest[0] == '-' || rest[0] == '+') {
		sign, rest = rest[:1], rest[1:]
	}

	integer, fraction, _ = strings.Cut(rest, ".")
	if integer == "" && fraction == "" {
		return "", "", "", false
	}
	if !isDigits(integer) || !isDigits(fraction) {
		return "", "", "", false
	}
	return sign, integer, fraction, true
}

func isDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func GetStringAmountFromBigIntWithPrecision(amount *big.Int, precision int) (string, error) {
	if precision < 0 {
		return "", fmt.Errorf("precision is negative: %d: %w", precision, ErrInvalidPrecision)
	}
	if amount == nil {
		return "", fmt.Errorf("amount is nil")
	}

	// Handle negative numbers: work with absolute value then prepend sign
	prefix := ""
	abs := new(big.Int).Set(amount)
	if abs.Sign() < 0 {
		prefix = "-"
		abs.Neg(abs)
	}

	amountString := abs.String()
	amountStringLength := len(amountString)

	if precision == 0 {
		// Nothing to do
		return prefix + amountString, nil
	}

	decimalPart := bytes.NewBufferString("")
	for p := precision; p > 0; p-- {
		if amountStringLength < p {
			decimalPart.WriteByte('0')
			continue
		}
		decimalPart.WriteByte(amountString[amountStringLength-p])
	}

	if amountStringLength < precision || amountStringLength == precision {
		return prefix + "0." + decimalPart.String(), nil
	}

	// Here we are in the case where the amount has more digits than the
	// precision, we need to add a dot at the right place
	return prefix + amountString[:amountStringLength-precision] + "." + decimalPart.String(), nil
}
