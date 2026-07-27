package currency

import (
	"errors"
	"fmt"
	"strings"
)

// PrecisionUnknown is the precision returned, alongside a non-nil error, when a
// currency code has no known number of minor units.
//
// It is deliberately negative rather than 0. A precision is a decimal exponent,
// so 0 is the most dangerous possible sentinel: a caller that drops the error
// and scales by 10^0 turns "1234.56" into 1234, destroying the minor units
// silently and plausibly. -1 fails loudly instead — scaling by it is rejected by
// GetAmountWithPrecisionFromString with ErrInvalidPrecision, and
// FormatAssetWithPrecision renders it as a visibly broken "XYZ/-1" asset.
const PrecisionUnknown = -1

// ErrMissingCurrencies is returned when a currency code is absent from the
// currency table it was looked up in.
var ErrMissingCurrencies = errors.New("missing currencies")

// ISO4217Currencies maps an ISO 4217 alphabetic currency code to its number of
// minor units: the decimal exponent that converts a human-readable amount into
// an integer amount of minor units ("1234.56" EUR -> 123456 with exponent 2).
//
// Generated from ISO 4217 list one, as published by SIX Group, the ISO 4217
// maintenance agency.
//
// Source URL: https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml
// List published: 2026-01-01 (the Pblshd attribute of that XML)
// Regenerated: 2026-07-27
//
// Do not hand-edit. testdata/list-one.csv holds the same data trimmed to
// (code, minor_units), and TestISO4217CurrenciesMatchesListOne diffs this map
// against it, so the table cannot drift from the CSV unnoticed. To update:
// re-download the URL above, regenerate the CSV, apply the differences the test
// reports, and bump Published and Regenerated in this comment.
//
// Two membership decisions are deliberate, and the test enforces both.
//
//  1. Codes whose minor units are "N.A." in the standard are NOT in this map.
//     map[string]int cannot express "no minor unit defined", and 0 is not a
//     truthful stand-in: 0 asserts that the currency has no sub-unit, the way
//     JPY genuinely has none. That is wrong for XAU (gold is traded by weight,
//     not in hundredths) and actively misleading for XXX, which denotes that no
//     currency is involved at all. Leaving them out makes GetPrecision error,
//     which is the honest answer — there is no exponent by which to scale an
//     amount of gold. The codes are still recorded, in ISO4217NoMinorUnitCodes.
//
//  2. Codes withdrawn from the standard are KEPT, and marked below. Their
//     exponents were correct while they were current and remain correct for the
//     amounts that were denominated in them, so a consumer replaying historical
//     data still needs them; dropping them would turn a resolvable historical
//     amount into a lookup failure. They are listed in ISO4217WithdrawnCodes so
//     that a caller minting new amounts can reject them.
var ISO4217Currencies = map[string]int{
	// Active in ISO 4217 list one as of the published date above.
	"AED": 2, // UAE Dirham
	"AFN": 2, // Afghani
	"ALL": 2, // Lek
	"AMD": 2, // Armenian Dram
	"AOA": 2, // Kwanza
	"ARS": 2, // Argentine Peso
	"AUD": 2, // Australian Dollar
	"AWG": 2, // Aruban Florin
	"AZN": 2, // Azerbaijan Manat
	"BAM": 2, // Convertible Mark
	"BBD": 2, // Barbados Dollar
	"BDT": 2, // Taka
	"BHD": 3, // Bahraini Dinar
	"BIF": 0, // Burundi Franc
	"BMD": 2, // Bermudian Dollar
	"BND": 2, // Brunei Dollar
	"BOB": 2, // Boliviano
	"BOV": 2, // Mvdol (funds code)
	"BRL": 2, // Brazilian Real
	"BSD": 2, // Bahamian Dollar
	"BTN": 2, // Ngultrum
	"BWP": 2, // Pula
	"BYN": 2, // Belarusian Ruble
	"BZD": 2, // Belize Dollar
	"CAD": 2, // Canadian Dollar
	"CDF": 2, // Congolese Franc
	"CHE": 2, // WIR Euro (funds code)
	"CHF": 2, // Swiss Franc
	"CHW": 2, // WIR Franc (funds code)
	"CLF": 4, // Unidad de Fomento (funds code)
	"CLP": 0, // Chilean Peso
	"CNY": 2, // Yuan Renminbi
	"COP": 2, // Colombian Peso
	"COU": 2, // Unidad de Valor Real (funds code)
	"CRC": 2, // Costa Rican Colon
	"CUP": 2, // Cuban Peso
	"CVE": 2, // Cabo Verde Escudo
	"CZK": 2, // Czech Koruna
	"DJF": 0, // Djibouti Franc
	"DKK": 2, // Danish Krone
	"DOP": 2, // Dominican Peso
	"DZD": 2, // Algerian Dinar
	"EGP": 2, // Egyptian Pound
	"ERN": 2, // Nakfa
	"ETB": 2, // Ethiopian Birr
	"EUR": 2, // Euro
	"FJD": 2, // Fiji Dollar
	"FKP": 2, // Falkland Islands Pound
	"GBP": 2, // Pound Sterling
	"GEL": 2, // Lari
	"GHS": 2, // Ghana Cedi
	"GIP": 2, // Gibraltar Pound
	"GMD": 2, // Dalasi
	"GNF": 0, // Guinean Franc
	"GTQ": 2, // Quetzal
	"GYD": 2, // Guyana Dollar
	"HKD": 2, // Hong Kong Dollar
	"HNL": 2, // Lempira
	"HTG": 2, // Gourde
	"HUF": 2, // Forint
	"IDR": 2, // Rupiah
	"ILS": 2, // New Israeli Sheqel
	"INR": 2, // Indian Rupee
	"IQD": 3, // Iraqi Dinar
	"IRR": 2, // Iranian Rial
	"ISK": 0, // Iceland Krona
	"JMD": 2, // Jamaican Dollar
	"JOD": 3, // Jordanian Dinar
	"JPY": 0, // Yen
	"KES": 2, // Kenyan Shilling
	"KGS": 2, // Som
	"KHR": 2, // Riel
	"KMF": 0, // Comorian Franc
	"KPW": 2, // North Korean Won
	"KRW": 0, // Won
	"KWD": 3, // Kuwaiti Dinar
	"KYD": 2, // Cayman Islands Dollar
	"KZT": 2, // Tenge
	"LAK": 2, // Lao Kip
	"LBP": 2, // Lebanese Pound
	"LKR": 2, // Sri Lanka Rupee
	"LRD": 2, // Liberian Dollar
	"LSL": 2, // Loti
	"LYD": 3, // Libyan Dinar
	"MAD": 2, // Moroccan Dirham
	"MDL": 2, // Moldovan Leu
	"MGA": 2, // Malagasy Ariary
	"MKD": 2, // Denar
	"MMK": 2, // Kyat
	"MNT": 2, // Tugrik
	"MOP": 2, // Pataca
	"MRU": 2, // Ouguiya
	"MUR": 2, // Mauritius Rupee
	"MVR": 2, // Rufiyaa
	"MWK": 2, // Malawi Kwacha
	"MXN": 2, // Mexican Peso
	"MXV": 2, // Mexican Unidad de Inversion (UDI) (funds code)
	"MYR": 2, // Malaysian Ringgit
	"MZN": 2, // Mozambique Metical
	"NAD": 2, // Namibia Dollar
	"NGN": 2, // Naira
	"NIO": 2, // Cordoba Oro
	"NOK": 2, // Norwegian Krone
	"NPR": 2, // Nepalese Rupee
	"NZD": 2, // New Zealand Dollar
	"OMR": 3, // Rial Omani
	"PAB": 2, // Balboa
	"PEN": 2, // Sol
	"PGK": 2, // Kina
	"PHP": 2, // Philippine Peso
	"PKR": 2, // Pakistan Rupee
	"PLN": 2, // Zloty
	"PYG": 0, // Guarani
	"QAR": 2, // Qatari Rial
	"RON": 2, // Romanian Leu
	"RSD": 2, // Serbian Dinar
	"RUB": 2, // Russian Ruble
	"RWF": 0, // Rwanda Franc
	"SAR": 2, // Saudi Riyal
	"SBD": 2, // Solomon Islands Dollar
	"SCR": 2, // Seychelles Rupee
	"SDG": 2, // Sudanese Pound
	"SEK": 2, // Swedish Krona
	"SGD": 2, // Singapore Dollar
	"SHP": 2, // Saint Helena Pound
	"SLE": 2, // Leone
	"SOS": 2, // Somali Shilling
	"SRD": 2, // Surinam Dollar
	"SSP": 2, // South Sudanese Pound
	"STN": 2, // Dobra
	"SVC": 2, // El Salvador Colon
	"SYP": 2, // Syrian Pound
	"SZL": 2, // Lilangeni
	"THB": 2, // Baht
	"TJS": 2, // Somoni
	"TMT": 2, // Turkmenistan New Manat
	"TND": 3, // Tunisian Dinar
	"TOP": 2, // Pa’anga
	"TRY": 2, // Turkish Lira
	"TTD": 2, // Trinidad and Tobago Dollar
	"TWD": 2, // New Taiwan Dollar
	"TZS": 2, // Tanzanian Shilling
	"UAH": 2, // Hryvnia
	"UGX": 0, // Uganda Shilling
	"USD": 2, // US Dollar
	"USN": 2, // US Dollar (Next day) (funds code)
	"UYI": 0, // Uruguay Peso en Unidades Indexadas (UI) (funds code)
	"UYU": 2, // Peso Uruguayo
	"UYW": 4, // Unidad Previsional
	"UZS": 2, // Uzbekistan Sum
	"VED": 2, // Bolívar Soberano
	"VES": 2, // Bolívar Soberano
	"VND": 0, // Dong
	"VUV": 0, // Vatu
	"WST": 2, // Tala
	"XAD": 2, // Arab Accounting Dinar
	"XAF": 0, // CFA Franc BEAC
	"XCD": 2, // East Caribbean Dollar
	"XCG": 2, // Caribbean Guilder
	"XOF": 0, // CFA Franc BCEAO
	"XPF": 0, // CFP Franc
	"YER": 2, // Yemeni Rial
	"ZAR": 2, // Rand
	"ZMW": 2, // Zambian Kwacha
	"ZWG": 2, // Zimbabwe Gold

	// Withdrawn from the standard, kept deliberately — see (2) above. These
	// must not be used to denominate new amounts.
	"ANG": 2, // Netherlands Antillean guilder — withdrawn 2025-03-31: replaced by XCG (Caribbean guilder)
	"BGN": 2, // Bulgarian lev — withdrawn 2026-01-01: Bulgaria adopted the euro
	"CUC": 2, // Cuban convertible peso — withdrawn 2021: Cuba's dual-currency system ended
	"HRK": 2, // Croatian kuna — withdrawn 2023-01-01: Croatia adopted the euro
	"SLL": 2, // Sierra Leonean leone (old) — withdrawn 2022-07-01: redenominated to SLE (1 SLE = 1000 SLL)
	"ZWL": 2, // Zimbabwean dollar — withdrawn 2024-06-25: replaced by ZWG (Zimbabwe Gold)
}

// ISO4217NoMinorUnitCodes is the set of ISO 4217 codes whose number of minor
// units the standard gives as "N.A." — precious metals, fund and bond-market
// units of account, and the testing/no-currency codes. They are absent from
// ISO4217Currencies on purpose; see decision (1) there.
//
// Membership means "this is a real ISO 4217 code that has no minor-unit
// exponent", which is different from "unknown code". Use it to tell a caller
// that an amount in XAU cannot be scaled, rather than that XAU is unrecognised.
var ISO4217NoMinorUnitCodes = map[string]struct{}{
	"XAG": {}, // Silver
	"XAU": {}, // Gold
	"XBA": {}, // Bond Markets Unit European Composite Unit (EURCO)
	"XBB": {}, // Bond Markets Unit European Monetary Unit (E.M.U.-6)
	"XBC": {}, // Bond Markets Unit European Unit of Account 9 (E.U.A.-9)
	"XBD": {}, // Bond Markets Unit European Unit of Account 17 (E.U.A.-17)
	"XDR": {}, // SDR (Special Drawing Right)
	"XPD": {}, // Palladium
	"XPT": {}, // Platinum
	"XSU": {}, // Sucre
	"XTS": {}, // Codes specifically reserved for testing purposes
	"XUA": {}, // ADB Unit of Account
	"XXX": {}, // The codes assigned for transactions where no currency is involved
}

// ISO4217WithdrawnCodes is the set of codes present in ISO4217Currencies that
// ISO 4217 list one no longer contains. Their exponents are retained so
// historical amounts stay resolvable; see decision (2) on ISO4217Currencies.
//
// Look a code up here before accepting it for a new amount.
var ISO4217WithdrawnCodes = map[string]struct{}{
	"ANG": {}, // withdrawn 2025-03-31: replaced by XCG (Caribbean guilder)
	"BGN": {}, // withdrawn 2026-01-01: Bulgaria adopted the euro
	"CUC": {}, // withdrawn 2021: Cuba's dual-currency system ended
	"HRK": {}, // withdrawn 2023-01-01: Croatia adopted the euro
	"SLL": {}, // withdrawn 2022-07-01: redenominated to SLE (1 SLE = 1000 SLL)
	"ZWL": {}, // withdrawn 2024-06-25: replaced by ZWG (Zimbabwe Gold)
}

// FormatAsset renders a currency code as a ledger asset qualified by its number
// of minor units, looked up in currencies: "EUR" becomes "EUR/2".
//
// It returns the bare code with no "/n" suffix in two cases: when the code's
// exponent is 0, so "JPY" becomes "JPY"; and when the code is absent from
// currencies, so "BBB" becomes "BBB". Note that the two are indistinguishable in
// the result — use GetPrecision if you need to know whether the code was found.
//
// Both forms are valid ledger assets, but "JPY" and "JPY/0" are two DIFFERENT
// assets and amounts posted to them will not net against each other. Do not mix
// FormatAsset and FormatAssetWithPrecision for the same currency: for JPY this
// one yields "JPY" while FormatAssetWithPrecision("JPY", 0) yields "JPY/0".
// Choose one convention per ledger and keep to it. The asymmetry is preserved
// for compatibility with assets already written to existing ledgers.
func FormatAsset(currencies map[string]int, cur string) string {
	asset := strings.ToUpper(cur)

	def, ok := currencies[asset]
	if !ok {
		return asset
	}

	if def == 0 {
		return asset
	}

	return fmt.Sprintf("%s/%d", asset, def)
}

// FormatAssetWithPrecision renders a currency code as a ledger asset qualified by
// the given precision. The suffix is always present, so a precision of 0 yields
// "JPY/0".
//
// This differs from FormatAsset, which omits the suffix entirely when the
// exponent is 0. See FormatAsset for why mixing the two conventions on one
// ledger produces assets that do not net.
func FormatAssetWithPrecision(cur string, precision int) string {
	asset := strings.ToUpper(cur)
	return fmt.Sprintf("%s/%d", asset, precision)
}

// GetPrecision returns the number of minor units recorded for cur in currencies.
//
// On a miss it returns PrecisionUnknown (-1) and an error wrapping
// ErrMissingCurrencies. The precision is deliberately not 0 on the error path,
// so that a caller who drops the error corrupts nothing quietly; see
// PrecisionUnknown.
func GetPrecision(currencies map[string]int, cur string) (int, error) {
	asset := strings.ToUpper(cur)

	def, ok := currencies[asset]
	if !ok {
		return PrecisionUnknown, fmt.Errorf("%s: %w", asset, ErrMissingCurrencies)
	}

	return def, nil
}

// GetCurrencyAndPrecisionFromAsset splits a "CUR/n" ledger asset and returns the
// currency code together with the precision recorded for it in currencies.
//
// The precision comes from currencies, not from the asset string: the "/n" part
// only has to be present, and any mismatch with the table is ignored. On error
// the returned precision is PrecisionUnknown (-1), never 0.
func GetCurrencyAndPrecisionFromAsset(currencies map[string]int, asset string) (string, int, error) {
	parts := strings.Split(asset, "/")
	if len(parts) != 2 {
		return "", PrecisionUnknown, fmt.Errorf("invalid asset: %s", asset)
	}

	currency := parts[0]
	precision, err := GetPrecision(currencies, currency)
	if err != nil {
		return "", PrecisionUnknown, err
	}

	return currency, precision, nil
}
