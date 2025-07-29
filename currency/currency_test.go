package currency

import (
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
		"typical format": {"USD/2", "USD", 2, false},
		"different precision provided than in currency list":           {"BTC/55", "BTC", 8, false},
		"unexpected value after slash still returns correct precision": {"EUR/JPY", "EUR", 2, false},
		"invalid value":  {"INVALID", "", 0, true},
		"too many parts": {"USD/4/2", "", 0, true},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			cur, pre, err := GetCurrencyAndPrecisionFromAsset(currencies, tt.asset)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCur, cur)
				assert.Equal(t, tt.expectedPre, pre)
			}
		})
	}
}
