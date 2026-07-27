package currency

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatAsset(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct{ currency, expected string }{
		"with precision": {
			currency: "EUR",
			expected: "EUR/2",
		},
		"zero decimals": {
			currency: "VND",
			expected: "VND",
		},
		"not in list": {
			currency: "BBB",
			expected: "BBB",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := FormatAsset(ISO4217Currencies, tc.currency)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestFormatAssetWithPrecision(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		currency  string
		precision int
		expected  string
	}{
		"typical":     {"EUR", 2, "EUR/2"},
		"lowercased":  {"eur", 2, "EUR/2"},
		"not in list": {"BBB", 2, "BBB/2"},
		// Unlike FormatAsset, the suffix is always present.
		"zero precision": {"JPY", 0, "JPY/0"},
		// PrecisionUnknown must render as a visibly broken asset rather than
		// something a ledger would plausibly accept. This is what makes a
		// dropped GetPrecision error fail loudly.
		"unknown precision": {"USD", PrecisionUnknown, "USD/-1"},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, FormatAssetWithPrecision(tc.currency, tc.precision))
		})
	}
}

// TestFormatAssetConventionsDivergeAtZero pins the documented asymmetry between
// the two formatters: at exponent 0 they produce different ledger assets, which
// will not net against each other. If this ever starts passing with equal
// values, the convention changed and existing ledgers need migrating.
func TestFormatAssetConventionsDivergeAtZero(t *testing.T) {
	t.Parallel()

	const zeroDecimalCurrency = "JPY"

	require.Equal(t, 0, ISO4217Currencies[zeroDecimalCurrency],
		"%s is expected to be a zero-decimal currency", zeroDecimalCurrency)

	bare := FormatAsset(ISO4217Currencies, zeroDecimalCurrency)
	qualified := FormatAssetWithPrecision(zeroDecimalCurrency, 0)

	assert.Equal(t, "JPY", bare)
	assert.Equal(t, "JPY/0", qualified)
	assert.NotEqual(t, bare, qualified,
		"documented divergence: do not mix the two formatters on one ledger")
}

// TestGetCurrencyAndPrecisionFromAssetRoundTrips is why the returned code is
// normalized: the code must be usable both to key back into the table and to
// rebuild the same asset, whatever the input's casing.
func TestGetCurrencyAndPrecisionFromAssetRoundTrips(t *testing.T) {
	t.Parallel()

	for _, asset := range []string{"EUR/2", "eur/2", "eUr/2", "JPY/0", "jpy/0"} {
		t.Run(asset, func(t *testing.T) {
			t.Parallel()

			cur, precision, err := GetCurrencyAndPrecisionFromAsset(ISO4217Currencies, asset)
			require.NoError(t, err)

			_, ok := ISO4217Currencies[cur]
			assert.True(t, ok, "returned code %q must key back into the table", cur)

			assert.Equal(t, strings.ToUpper(asset), FormatAssetWithPrecision(cur, precision),
				"the code and precision must rebuild the same asset")
		})
	}
}

func TestGetCurrencyAndPrecisionFromAsset(t *testing.T) {
	currencies := map[string]int{
		"USD": 2,
		"EUR": 2,
		"BTC": 8,
	}

	tests := map[string]struct {
		asset       string
		expectedCur string
		expectedPre int
		expectErr   bool
	}{
		"typical format":                                               {"USD/2", "USD", 2, false},
		"lowercased asset is normalized":                               {"usd/2", "USD", 2, false},
		"mixed case asset is normalized":                               {"uSd/2", "USD", 2, false},
		"different precision provided than in currency list":           {"BTC/55", "BTC", 8, false},
		"unexpected value after slash still returns correct precision": {"EUR/JPY", "EUR", 2, false},
		"invalid value":                                                {"INVALID", "", PrecisionUnknown, true},
		"too many parts":                                               {"USD/4/2", "", PrecisionUnknown, true},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			cur, pre, err := GetCurrencyAndPrecisionFromAsset(currencies, tt.asset)
			if tt.expectErr {
				assert.Error(t, err)
				// The precision on the error path must be unusable, not 0.
				assert.Equal(t, tt.expectedPre, pre)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCur, cur)
				assert.Equal(t, tt.expectedPre, pre)
			}
		})
	}
}
