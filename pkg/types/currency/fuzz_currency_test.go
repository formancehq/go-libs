package currency

import (
	"testing"
)

func FuzzGetCurrencyAndPrecisionFromAsset(f *testing.F) {
	// Valid assets
	f.Add("EUR/2")
	f.Add("USD/2")
	f.Add("BHD/3")
	f.Add("JPY/0")
	f.Add("CLF/4")

	// Edge cases
	f.Add("")
	f.Add("/")
	f.Add("EUR")
	f.Add("EUR/")
	f.Add("/2")
	f.Add("EUR/2/extra")
	f.Add("UNKNOWN/2")
	f.Add("eur/2")
	f.Add("EUR/-1")

	f.Fuzz(func(t *testing.T, asset string) {
		// Must not panic
		currency, precision, err := GetCurrencyAndPrecisionFromAsset(ISO4217Currencies, asset)
		if err != nil {
			return
		}

		if currency == "" {
			t.Fatal("empty currency without error")
		}

		if precision < 0 {
			t.Fatal("negative precision without error")
		}
	})
}

func FuzzFormatAsset(f *testing.F) {
	f.Add("EUR")
	f.Add("USD")
	f.Add("JPY")
	f.Add("BHD")
	f.Add("UNKNOWN")
	f.Add("")
	f.Add("eur")
	f.Add("💰")

	f.Fuzz(func(t *testing.T, cur string) {
		// Must not panic
		result := FormatAsset(ISO4217Currencies, cur)

		if result == "" && cur != "" {
			t.Error("empty result for non-empty input")
		}
	})
}
