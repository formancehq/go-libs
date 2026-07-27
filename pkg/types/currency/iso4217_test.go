package currency

import (
	"encoding/csv"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listOnePath is the committed copy of ISO 4217 list one, trimmed to
// (code, minor_units). See its header for provenance and how to regenerate it.
//
// The tests below read only this file: they never reach the network, so they stay
// deterministic and offline. The trade-off is that they detect drift between the
// map and the CSV, not between the CSV and the standard — keeping the CSV current
// is a deliberate act, which is why its header records the publication date.
const listOnePath = "testdata/list-one.csv"

// naMinorUnits is the value ISO 4217 uses for codes that have no minor unit.
const naMinorUnits = "N.A."

// loadListOne parses the committed CSV into the codes that carry a numeric
// exponent and the codes the standard marks "N.A.".
func loadListOne(t *testing.T) (numeric map[string]int, notApplicable map[string]struct{}) {
	t.Helper()

	f, err := os.Open(listOnePath)
	require.NoError(t, err, "cannot open %s", listOnePath)
	t.Cleanup(func() { require.NoError(t, f.Close()) })

	r := csv.NewReader(f)
	r.Comment = '#'
	r.FieldsPerRecord = 2

	records, err := r.ReadAll()
	require.NoError(t, err, "cannot parse %s", listOnePath)
	require.NotEmpty(t, records, "%s is empty", listOnePath)

	require.Equal(t, []string{"code", "minor_units"}, records[0],
		"unexpected header in %s", listOnePath)

	numeric = map[string]int{}
	notApplicable = map[string]struct{}{}

	for _, rec := range records[1:] {
		code, units := rec[0], rec[1]

		require.Len(t, code, 3, "%s: currency code %q is not three characters", listOnePath, code)
		_, dupNum := numeric[code]
		_, dupNA := notApplicable[code]
		require.False(t, dupNum || dupNA, "%s: duplicate row for %s", listOnePath, code)

		if units == naMinorUnits {
			notApplicable[code] = struct{}{}
			continue
		}

		exponent, err := strconv.Atoi(units)
		require.NoError(t, err, "%s: %s has non-numeric minor_units %q", listOnePath, code, units)
		numeric[code] = exponent
	}

	return numeric, notApplicable
}

// TestISO4217CurrenciesMatchesListOne is the drift guard. It fails whenever
// ISO4217Currencies stops agreeing with the committed list one data — a changed
// exponent, a code the standard added, or an entry that is neither active nor
// recorded as withdrawn.
func TestISO4217CurrenciesMatchesListOne(t *testing.T) {
	t.Parallel()

	iso, isoNA := loadListOne(t)

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

	_, isoNA := loadListOne(t)

	assert.Equal(t, keys(isoNA), keys(ISO4217NoMinorUnitCodes),
		"ISO4217NoMinorUnitCodes disagrees with the N.A. rows in %s", listOnePath)
}

// TestISO4217WithdrawnCodesAreConsistent checks the withdrawn set against both
// the map and the standard: a withdrawn code must still carry its exponent, and
// must not have quietly come back.
func TestISO4217WithdrawnCodesAreConsistent(t *testing.T) {
	t.Parallel()

	iso, isoNA := loadListOne(t)

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

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
