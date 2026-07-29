package currency

import (
	_ "embed"
	"encoding/csv"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listOnePath is the committed copy of ISO 4217 list one, trimmed to
// (code, minor_units, numeric_code). listThreePath is the same for list three,
// the standard's withdrawn codes, trimmed to the ones this package retains. See
// their headers for provenance and how to regenerate them.
//
// Both are embedded into the test binary rather than read at run time, so the
// tests cannot reach the network and cannot depend on the working directory. The
// trade-off is that they detect drift between the maps and the CSVs, not between
// the CSVs and the standard — keeping the CSVs current is a deliberate act, which
// is why their headers record the publication date.
const (
	listOnePath   = "testdata/list-one.csv"
	listThreePath = "testdata/list-three.csv"
)

//go:embed testdata/list-one.csv
var listOneCSV string

//go:embed testdata/list-three.csv
var listThreeCSV string

// naMinorUnits is the value ISO 4217 uses for codes that have no minor unit.
const naMinorUnits = "N.A."

// loadListOne parses the committed list one CSV into the codes that carry a
// numeric exponent, the codes the standard marks "N.A.", and the numeric code of
// every code regardless of which of the two it fell into.
func loadListOne(t *testing.T) (
	numeric map[string]int,
	notApplicable map[string]struct{},
	numericCodes map[string]NumericCode,
) {
	t.Helper()

	records := readTestdataCSV(t, listOneCSV, listOnePath, []string{"code", "minor_units", "numeric_code"})

	numeric = map[string]int{}
	notApplicable = map[string]struct{}{}
	numericCodes = map[string]NumericCode{}

	for _, rec := range records {
		code, units, number := rec[0], rec[1], rec[2]

		require.Len(t, code, 3, "%s: currency code %q is not three characters", listOnePath, code)
		_, dup := numericCodes[code]
		require.False(t, dup, "%s: duplicate row for %s", listOnePath, code)

		numericCodes[code] = parseNumericCode(t, listOnePath, code, number)

		if units == naMinorUnits {
			notApplicable[code] = struct{}{}
			continue
		}

		exponent, err := strconv.Atoi(units)
		require.NoError(t, err, "%s: %s has non-numeric minor_units %q", listOnePath, code, units)
		numeric[code] = exponent
	}

	return numeric, notApplicable, numericCodes
}

// loadListThree parses the committed list three CSV into the numeric code and
// withdrawal date of each code this package retains.
func loadListThree(t *testing.T) (numericCodes map[string]NumericCode, withdrawn map[string]string) {
	t.Helper()

	records := readTestdataCSV(t, listThreeCSV, listThreePath, []string{"code", "numeric_code", "withdrawn"})

	numericCodes = map[string]NumericCode{}
	withdrawn = map[string]string{}

	for _, rec := range records {
		code, number, date := rec[0], rec[1], rec[2]

		require.Len(t, code, 3, "%s: currency code %q is not three characters", listThreePath, code)
		_, dup := numericCodes[code]
		require.False(t, dup, "%s: duplicate row for %s", listThreePath, code)

		numericCodes[code] = parseNumericCode(t, listThreePath, code, number)

		require.Regexp(t, `^\d{4}-\d{2}$`, date,
			"%s: %s has withdrawal date %q, want YYYY-MM as list three publishes it",
			listThreePath, code, date)
		withdrawn[code] = date
	}

	return numericCodes, withdrawn
}

// readTestdataCSV parses an embedded CSV, checks its header and returns its data
// rows. Comment lines carry the provenance headers and are skipped. name is only
// used in failure messages.
func readTestdataCSV(t *testing.T, content, name string, header []string) [][]string {
	t.Helper()

	require.NotEmpty(t, content, "%s is embedded but empty", name)

	r := csv.NewReader(strings.NewReader(content))
	r.Comment = '#'
	r.FieldsPerRecord = len(header)

	records, err := r.ReadAll()
	require.NoError(t, err, "cannot parse %s", name)
	require.NotEmpty(t, records, "%s has no rows", name)
	require.Equal(t, header, records[0], "unexpected header in %s", name)

	return records[1:]
}

// parseNumericCode checks that a numeric_code field is exactly the three digits
// the standard publishes, leading zeros included, before converting it.
func parseNumericCode(t *testing.T, path, code, number string) NumericCode {
	t.Helper()

	require.Regexp(t, `^\d{3}$`, number,
		"%s: %s has numeric_code %q, want three digits with any leading zeros kept",
		path, code, number)

	n, err := strconv.Atoi(number)
	require.NoError(t, err, "%s: %s has non-numeric numeric_code %q", path, code, number)

	return NumericCode(n)
}

// TestISO4217CurrenciesMatchesListOne is the drift guard. It fails whenever
// ISO4217Currencies stops agreeing with the committed list one data — a changed
// exponent, a code the standard added, or an entry that is neither active nor
// recorded as withdrawn.
func TestISO4217CurrenciesMatchesListOne(t *testing.T) {
	t.Parallel()

	iso, isoNA, _ := loadListOne(t)

	t.Run("every active code is present with the standard's exponent", func(t *testing.T) {
		t.Parallel()

		var missing []string
		for code, want := range iso {
			got, ok := ISO4217Currencies[code]
			if !ok {
				missing = append(missing, code+" (minor units "+strconv.Itoa(want)+")")
				continue
			}
			assert.Equal(t, want, got,
				"%s: ISO4217Currencies says %d minor units, list one says %d", code, got, want)
		}
		sort.Strings(missing)
		assert.Empty(t, missing,
			"active in list one but missing from ISO4217Currencies; add them: %v", missing)
	})

	t.Run("every entry is either active or a recorded withdrawn code", func(t *testing.T) {
		t.Parallel()

		var unaccounted []string
		for code := range ISO4217Currencies {
			if _, active := iso[code]; active {
				continue
			}
			if _, withdrawn := ISO4217WithdrawnCodes[code]; withdrawn {
				continue
			}
			unaccounted = append(unaccounted, code)
		}
		sort.Strings(unaccounted)
		assert.Empty(t, unaccounted,
			"in ISO4217Currencies but not in list one and not in ISO4217WithdrawnCodes; "+
				"either the standard dropped them (add them to ISO4217WithdrawnCodes with a "+
				"comment recording when and why) or they are bogus: %v", unaccounted)
	})

	t.Run("no code marked N.A. carries an exponent", func(t *testing.T) {
		t.Parallel()

		var wrong []string
		for code := range isoNA {
			if _, ok := ISO4217Currencies[code]; ok {
				wrong = append(wrong, code)
			}
		}
		sort.Strings(wrong)
		assert.Empty(t, wrong,
			"list one gives these codes no minor unit, so they must not appear in "+
				"ISO4217Currencies with a made-up exponent: %v", wrong)
	})

	t.Run("every list one code is accounted for exactly once", func(t *testing.T) {
		t.Parallel()

		// Guards against a code being silently dropped from both collections
		// during a regeneration.
		assert.Len(t, ISO4217NoMinorUnitCodes, len(isoNA))
		assert.Equal(t, len(iso)+len(isoNA),
			len(ISO4217Currencies)-len(ISO4217WithdrawnCodes)+len(ISO4217NoMinorUnitCodes),
			"active entries + N.A. codes should equal the number of rows in %s", listOnePath)
	})
}

// TestISO4217NoMinorUnitCodesMatchesListOne pins the set of codes the standard
// gives no minor unit, so a newly added N.A. code cannot pass unnoticed.
func TestISO4217NoMinorUnitCodesMatchesListOne(t *testing.T) {
	t.Parallel()

	_, isoNA, _ := loadListOne(t)

	assert.Equal(t, keys(isoNA), keys(ISO4217NoMinorUnitCodes),
		"ISO4217NoMinorUnitCodes disagrees with the N.A. rows in %s", listOnePath)
}

// TestISO4217WithdrawnCodesAreConsistent checks the withdrawn set against both
// the map and the standard: a withdrawn code must still carry its exponent, and
// must not have quietly come back.
func TestISO4217WithdrawnCodesAreConsistent(t *testing.T) {
	t.Parallel()

	iso, isoNA, _ := loadListOne(t)

	for code := range ISO4217WithdrawnCodes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			_, kept := ISO4217Currencies[code]
			assert.True(t, kept,
				"%s is listed as withdrawn but has no exponent in ISO4217Currencies; "+
					"withdrawn codes are kept so historical amounts stay resolvable", code)

			_, active := iso[code]
			_, na := isoNA[code]
			assert.False(t, active || na,
				"%s is in list one, so it is not withdrawn; remove it from "+
					"ISO4217WithdrawnCodes", code)
		})
	}
}

// TestGetPrecisionOnMissDoesNotReturnZero pins the sentinel. Returning 0 here
// would let a caller who drops the error scale by 10^0 and destroy an amount's
// minor units without any signal.
func TestGetPrecisionOnMissDoesNotReturnZero(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"BBB", "", "XAU", "HRK/2"} {
		t.Run("miss "+code, func(t *testing.T) {
			t.Parallel()

			precision, err := GetPrecision(ISO4217Currencies, code)
			require.ErrorIs(t, err, ErrMissingCurrencies)
			assert.Equal(t, PrecisionUnknown, precision)
			assert.Negative(t, precision, "a dropped error must not yield a usable precision")
		})
	}

	t.Run("hit", func(t *testing.T) {
		t.Parallel()

		precision, err := GetPrecision(ISO4217Currencies, "eur")
		require.NoError(t, err)
		assert.Equal(t, 2, precision)
	})
}

// TestGetCurrencyAndPrecisionFromAssetErrorPrecision covers the same sentinel on
// the path most consumers actually call.
func TestGetCurrencyAndPrecisionFromAssetErrorPrecision(t *testing.T) {
	t.Parallel()

	for name, asset := range map[string]string{
		"malformed asset": "INVALID",
		"too many parts":  "USD/4/2",
		"unknown code":    "BBB/2",
		"N.A. code":       "XAU/2",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cur, precision, err := GetCurrencyAndPrecisionFromAsset(ISO4217Currencies, asset)
			require.Error(t, err)
			assert.Empty(t, cur)
			assert.Equal(t, PrecisionUnknown, precision)
		})
	}
}

// TestWithdrawnCodesStayResolvable is the reason withdrawn codes are kept: a
// consumer replaying historical data must still be able to scale these amounts.
func TestWithdrawnCodesStayResolvable(t *testing.T) {
	t.Parallel()

	for code := range ISO4217WithdrawnCodes {
		t.Run(code, func(t *testing.T) {
			t.Parallel()

			precision, err := GetPrecision(ISO4217Currencies, code)
			require.NoError(t, err, "%s must stay resolvable for historical amounts", code)
			assert.GreaterOrEqual(t, precision, 0)
		})
	}
}

// keys returns a map's keys sorted, so two collections can be compared by
// membership and the failure message lists them in a stable order.
func keys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// TestISO4217NumericCodesMatchesISOLists is the drift guard for the numeric
// codes. Active codes are checked against list one, withdrawn ones against
// list three, so both halves of the map have a committed source.
func TestISO4217NumericCodesMatchesISOLists(t *testing.T) {
	t.Parallel()

	_, _, isoNumbers := loadListOne(t)
	histNumbers, _ := loadListThree(t)

	t.Run("every active code has the standard's number", func(t *testing.T) {
		t.Parallel()

		var missing []string
		for code, want := range isoNumbers {
			got, ok := ISO4217NumericCodes[code]
			if !ok {
				missing = append(missing, code+" ("+want.String()+")")
				continue
			}
			assert.Equal(t, want, got,
				"%s: ISO4217NumericCodes says %s, list one says %s", code, got, want)
		}
		sort.Strings(missing)
		assert.Empty(t, missing,
			"in list one but missing from ISO4217NumericCodes; add them: %v", missing)
	})

	t.Run("every withdrawn code has the number list three records", func(t *testing.T) {
		t.Parallel()

		for code, want := range histNumbers {
			got, ok := ISO4217NumericCodes[code]
			if assert.True(t, ok, "%s is withdrawn but has no numeric code", code) {
				assert.Equal(t, want, got,
					"%s: ISO4217NumericCodes says %s, list three says %s", code, got, want)
			}
		}
	})

	t.Run("no entry is unaccounted for", func(t *testing.T) {
		t.Parallel()

		var unaccounted []string
		for code := range ISO4217NumericCodes {
			_, active := isoNumbers[code]
			_, hist := histNumbers[code]
			if !active && !hist {
				unaccounted = append(unaccounted, code)
			}
		}
		sort.Strings(unaccounted)
		assert.Empty(t, unaccounted,
			"in ISO4217NumericCodes but in neither %s nor %s: %v",
			listOnePath, listThreePath, unaccounted)

		assert.Len(t, ISO4217NumericCodes, len(isoNumbers)+len(histNumbers))
	})

	t.Run("wider than ISO4217Currencies by exactly the N.A. codes", func(t *testing.T) {
		t.Parallel()

		// The asymmetry is deliberate: a numeric code exists for every ISO 4217
		// code, an exponent does not. If these ever match, one of the two maps
		// gained or lost a code without the other being updated.
		assert.Equal(t, len(ISO4217Currencies)+len(ISO4217NoMinorUnitCodes),
			len(ISO4217NumericCodes),
			"ISO4217NumericCodes should cover ISO4217Currencies plus the N.A. codes")

		for code := range ISO4217NoMinorUnitCodes {
			_, ok := ISO4217NumericCodes[code]
			assert.True(t, ok,
				"%s has no minor unit but does have a numeric code, so it belongs here", code)
		}
	})
}

// TestISO4217WithdrawnCodesMatchListThree pins the withdrawn set against its
// source, so the set and the CSV cannot diverge.
func TestISO4217WithdrawnCodesMatchListThree(t *testing.T) {
	t.Parallel()

	histNumbers, _ := loadListThree(t)

	assert.Equal(t, keys(histNumbers), keys(ISO4217WithdrawnCodes),
		"ISO4217WithdrawnCodes disagrees with %s", listThreePath)
}

// TestNumericCodeString covers the zero padding. 16 of the assigned codes have a
// leading zero, and dropping it produces a code no ISO 20022 or card-network
// parser accepts.
func TestNumericCodeString(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		code     NumericCode
		expected string
	}{
		"single digit":       {8, "008"},
		"two digits":         {36, "036"},
		"three digits":       {978, "978"},
		"highest valid":      {999, "999"},
		"unknown is visible": {NumericCodeUnknown, "invalid(-1)"},
		"out of range":       {1000, "invalid(1000)"},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, tc.code.String())
		})
	}
}

// TestNumericCodeStringIsThreeDigitsForEveryCode is the property the wire
// formats actually depend on.
func TestNumericCodeStringIsThreeDigitsForEveryCode(t *testing.T) {
	t.Parallel()

	for code, number := range ISO4217NumericCodes {
		assert.Regexp(t, `^\d{3}$`, number.String(),
			"%s renders as %q, which is not a three-digit ISO numeric code", code, number)
	}
}

func TestGetNumericCode(t *testing.T) {
	t.Parallel()

	t.Run("hit", func(t *testing.T) {
		t.Parallel()

		code, err := GetNumericCode(ISO4217NumericCodes, "EUR")
		require.NoError(t, err)
		assert.Equal(t, NumericCode(978), code)
		assert.Equal(t, "978", code.String())
	})

	t.Run("lowercased input is normalized", func(t *testing.T) {
		t.Parallel()

		code, err := GetNumericCode(ISO4217NumericCodes, "all")
		require.NoError(t, err)
		assert.Equal(t, "008", code.String(), "ALL is 008, and the padding must survive")
	})

	t.Run("codes with no minor unit still resolve", func(t *testing.T) {
		t.Parallel()

		// The whole point of the wider membership: GetPrecision rejects XAU
		// because no exponent is truthful for gold, but 959 is its real number.
		code, err := GetNumericCode(ISO4217NumericCodes, "XAU")
		require.NoError(t, err)
		assert.Equal(t, NumericCode(959), code)

		_, err = GetPrecision(ISO4217Currencies, "XAU")
		require.ErrorIs(t, err, ErrMissingCurrencies,
			"XAU must still have no exponent")
	})

	t.Run("withdrawn codes still resolve", func(t *testing.T) {
		t.Parallel()

		code, err := GetNumericCode(ISO4217NumericCodes, "HRK")
		require.NoError(t, err)
		assert.Equal(t, NumericCode(191), code)
	})

	for _, unknown := range []string{"BBB", "", "EUR/2"} {
		t.Run("miss "+unknown, func(t *testing.T) {
			t.Parallel()

			code, err := GetNumericCode(ISO4217NumericCodes, unknown)
			require.ErrorIs(t, err, ErrMissingCurrencies)
			assert.Equal(t, NumericCodeUnknown, code)
			assert.Equal(t, "invalid(-1)", code.String(),
				"a dropped error must not render as a plausible code")
		})
	}
}

// TestNumericCodesAreNotUniqueAcrossTime pins the ANG/XCG collision. It is the
// reason this package offers no reverse numeric-to-alpha lookup; if the data ever
// stops colliding, that decision can be revisited.
func TestNumericCodesAreNotUniqueAcrossTime(t *testing.T) {
	t.Parallel()

	ang, err := GetNumericCode(ISO4217NumericCodes, "ANG")
	require.NoError(t, err)
	xcg, err := GetNumericCode(ISO4217NumericCodes, "XCG")
	require.NoError(t, err)

	assert.Equal(t, ang, xcg,
		"XCG took over ANG's number when it replaced it; a reverse lookup on %s is ambiguous", ang)

	byNumber := map[NumericCode][]string{}
	for code, number := range ISO4217NumericCodes {
		byNumber[number] = append(byNumber[number], code)
	}
	collisions := map[string][]string{}
	for number, codes := range byNumber {
		if len(codes) > 1 {
			sort.Strings(codes)
			collisions[number.String()] = codes
		}
	}
	assert.Equal(t, map[string][]string{"532": {"ANG", "XCG"}}, collisions,
		"the set of colliding numeric codes changed; revisit the no-reverse-lookup decision")
}
