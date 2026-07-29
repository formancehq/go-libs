package currency

import (
	"fmt"
	"strconv"
	"strings"
)

// NumericCode is an ISO 4217 numeric currency code: the three-digit number the
// standard assigns alongside each alphabetic code, used by ISO 20022, card
// networks and other wire formats that carry a number rather than letters.
//
// It is a distinct type rather than a bare int for two reasons. It keeps a
// numeric-code table from being passed to GetPrecision, which takes a
// map[string]int and would otherwise happily read 978 as "EUR has 978 minor
// units". And it carries String, because 16 of the 178 assigned codes have a
// leading zero — ALL is 008, not 8 — so the canonical three-digit form has to be
// rendered deliberately.
type NumericCode int

// NumericCodeUnknown is the numeric code returned, alongside a non-nil error,
// when a currency code has none recorded. It is negative for the same reason as
// PrecisionUnknown: no valid ISO 4217 numeric code can collide with it, so a
// dropped error cannot be mistaken for a real currency.
const NumericCodeUnknown NumericCode = -1

// String renders the code in its canonical zero-padded three-digit form, so
// NumericCode(8) is "008" and NumericCode(999) is "999".
//
// Anything outside the representable range, including NumericCodeUnknown, renders
// as a visibly invalid string rather than a plausible-looking number, so a
// dropped error shows up in whatever message the value was written into.
func (c NumericCode) String() string {
	if c < 0 || c > 999 {
		return "invalid(" + strconv.Itoa(int(c)) + ")"
	}

	return fmt.Sprintf("%03d", int(c))
}

// ISO4217NumericCodes maps an ISO 4217 alphabetic currency code to its numeric
// code.
//
// Generated from ISO 4217 lists one and three, as published by SIX Group, the
// ISO 4217 maintenance agency.
//
// Source URL (active):    https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml
// Source URL (withdrawn): https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-three.xml
// Lists published: 2026-01-01 (the Pblshd attribute of both XMLs)
// Regenerated: 2026-07-29
//
// Do not hand-edit. testdata/list-one.csv and testdata/list-three.csv carry the
// same numbers, and TestISO4217NumericCodesMatchesISOLists diffs this map against
// both.
//
// Membership deliberately differs from ISO4217Currencies, and it is wider: every
// ISO 4217 code has a numeric code, including the thirteen whose minor units are
// "N.A.". Those are absent from ISO4217Currencies because no exponent is
// truthful for them (see the decisions recorded there), but 959 really is the
// number for gold, so withholding it here would lose information for no reason.
// This map therefore holds every active code plus the withdrawn ones that
// ISO4217Currencies retains.
//
// Numeric codes are NOT unique across time. ANG and XCG both use 532, because
// XCG took over the number when it replaced ANG. Any reverse numeric-to-alpha
// lookup has to decide what to do about that, which is why this package does not
// offer one.
var ISO4217NumericCodes = map[string]NumericCode{
	// Active in ISO 4217 list one, including the codes that have no minor unit.
	"AED": 784, // UAE Dirham
	"AFN": 971, // Afghani
	"ALL": 8,   // Lek
	"AMD": 51,  // Armenian Dram
	"AOA": 973, // Kwanza
	"ARS": 32,  // Argentine Peso
	"AUD": 36,  // Australian Dollar
	"AWG": 533, // Aruban Florin
	"AZN": 944, // Azerbaijan Manat
	"BAM": 977, // Convertible Mark
	"BBD": 52,  // Barbados Dollar
	"BDT": 50,  // Taka
	"BHD": 48,  // Bahraini Dinar
	"BIF": 108, // Burundi Franc
	"BMD": 60,  // Bermudian Dollar
	"BND": 96,  // Brunei Dollar
	"BOB": 68,  // Boliviano
	"BOV": 984, // Mvdol
	"BRL": 986, // Brazilian Real
	"BSD": 44,  // Bahamian Dollar
	"BTN": 64,  // Ngultrum
	"BWP": 72,  // Pula
	"BYN": 933, // Belarusian Ruble
	"BZD": 84,  // Belize Dollar
	"CAD": 124, // Canadian Dollar
	"CDF": 976, // Congolese Franc
	"CHE": 947, // WIR Euro
	"CHF": 756, // Swiss Franc
	"CHW": 948, // WIR Franc
	"CLF": 990, // Unidad de Fomento
	"CLP": 152, // Chilean Peso
	"CNY": 156, // Yuan Renminbi
	"COP": 170, // Colombian Peso
	"COU": 970, // Unidad de Valor Real
	"CRC": 188, // Costa Rican Colon
	"CUP": 192, // Cuban Peso
	"CVE": 132, // Cabo Verde Escudo
	"CZK": 203, // Czech Koruna
	"DJF": 262, // Djibouti Franc
	"DKK": 208, // Danish Krone
	"DOP": 214, // Dominican Peso
	"DZD": 12,  // Algerian Dinar
	"EGP": 818, // Egyptian Pound
	"ERN": 232, // Nakfa
	"ETB": 230, // Ethiopian Birr
	"EUR": 978, // Euro
	"FJD": 242, // Fiji Dollar
	"FKP": 238, // Falkland Islands Pound
	"GBP": 826, // Pound Sterling
	"GEL": 981, // Lari
	"GHS": 936, // Ghana Cedi
	"GIP": 292, // Gibraltar Pound
	"GMD": 270, // Dalasi
	"GNF": 324, // Guinean Franc
	"GTQ": 320, // Quetzal
	"GYD": 328, // Guyana Dollar
	"HKD": 344, // Hong Kong Dollar
	"HNL": 340, // Lempira
	"HTG": 332, // Gourde
	"HUF": 348, // Forint
	"IDR": 360, // Rupiah
	"ILS": 376, // New Israeli Sheqel
	"INR": 356, // Indian Rupee
	"IQD": 368, // Iraqi Dinar
	"IRR": 364, // Iranian Rial
	"ISK": 352, // Iceland Krona
	"JMD": 388, // Jamaican Dollar
	"JOD": 400, // Jordanian Dinar
	"JPY": 392, // Yen
	"KES": 404, // Kenyan Shilling
	"KGS": 417, // Som
	"KHR": 116, // Riel
	"KMF": 174, // Comorian Franc
	"KPW": 408, // North Korean Won
	"KRW": 410, // Won
	"KWD": 414, // Kuwaiti Dinar
	"KYD": 136, // Cayman Islands Dollar
	"KZT": 398, // Tenge
	"LAK": 418, // Lao Kip
	"LBP": 422, // Lebanese Pound
	"LKR": 144, // Sri Lanka Rupee
	"LRD": 430, // Liberian Dollar
	"LSL": 426, // Loti
	"LYD": 434, // Libyan Dinar
	"MAD": 504, // Moroccan Dirham
	"MDL": 498, // Moldovan Leu
	"MGA": 969, // Malagasy Ariary
	"MKD": 807, // Denar
	"MMK": 104, // Kyat
	"MNT": 496, // Tugrik
	"MOP": 446, // Pataca
	"MRU": 929, // Ouguiya
	"MUR": 480, // Mauritius Rupee
	"MVR": 462, // Rufiyaa
	"MWK": 454, // Malawi Kwacha
	"MXN": 484, // Mexican Peso
	"MXV": 979, // Mexican Unidad de Inversion (UDI)
	"MYR": 458, // Malaysian Ringgit
	"MZN": 943, // Mozambique Metical
	"NAD": 516, // Namibia Dollar
	"NGN": 566, // Naira
	"NIO": 558, // Cordoba Oro
	"NOK": 578, // Norwegian Krone
	"NPR": 524, // Nepalese Rupee
	"NZD": 554, // New Zealand Dollar
	"OMR": 512, // Rial Omani
	"PAB": 590, // Balboa
	"PEN": 604, // Sol
	"PGK": 598, // Kina
	"PHP": 608, // Philippine Peso
	"PKR": 586, // Pakistan Rupee
	"PLN": 985, // Zloty
	"PYG": 600, // Guarani
	"QAR": 634, // Qatari Rial
	"RON": 946, // Romanian Leu
	"RSD": 941, // Serbian Dinar
	"RUB": 643, // Russian Ruble
	"RWF": 646, // Rwanda Franc
	"SAR": 682, // Saudi Riyal
	"SBD": 90,  // Solomon Islands Dollar
	"SCR": 690, // Seychelles Rupee
	"SDG": 938, // Sudanese Pound
	"SEK": 752, // Swedish Krona
	"SGD": 702, // Singapore Dollar
	"SHP": 654, // Saint Helena Pound
	"SLE": 925, // Leone
	"SOS": 706, // Somali Shilling
	"SRD": 968, // Surinam Dollar
	"SSP": 728, // South Sudanese Pound
	"STN": 930, // Dobra
	"SVC": 222, // El Salvador Colon
	"SYP": 760, // Syrian Pound
	"SZL": 748, // Lilangeni
	"THB": 764, // Baht
	"TJS": 972, // Somoni
	"TMT": 934, // Turkmenistan New Manat
	"TND": 788, // Tunisian Dinar
	"TOP": 776, // Pa’anga
	"TRY": 949, // Turkish Lira
	"TTD": 780, // Trinidad and Tobago Dollar
	"TWD": 901, // New Taiwan Dollar
	"TZS": 834, // Tanzanian Shilling
	"UAH": 980, // Hryvnia
	"UGX": 800, // Uganda Shilling
	"USD": 840, // US Dollar
	"USN": 997, // US Dollar (Next day)
	"UYI": 940, // Uruguay Peso en Unidades Indexadas (UI)
	"UYU": 858, // Peso Uruguayo
	"UYW": 927, // Unidad Previsional
	"UZS": 860, // Uzbekistan Sum
	"VED": 926, // Bolívar Soberano
	"VES": 928, // Bolívar Soberano
	"VND": 704, // Dong
	"VUV": 548, // Vatu
	"WST": 882, // Tala
	"XAD": 396, // Arab Accounting Dinar
	"XAF": 950, // CFA Franc BEAC
	"XAG": 961, // Silver (no minor unit)
	"XAU": 959, // Gold (no minor unit)
	"XBA": 955, // Bond Markets Unit European Composite Unit (EURCO) (no minor unit)
	"XBB": 956, // Bond Markets Unit European Monetary Unit (E.M.U.-6) (no minor unit)
	"XBC": 957, // Bond Markets Unit European Unit of Account 9 (E.U.A.-9) (no minor unit)
	"XBD": 958, // Bond Markets Unit European Unit of Account 17 (E.U.A.-17) (no minor unit)
	"XCD": 951, // East Caribbean Dollar
	"XCG": 532, // Caribbean Guilder
	"XDR": 960, // SDR (Special Drawing Right) (no minor unit)
	"XOF": 952, // CFA Franc BCEAO
	"XPD": 964, // Palladium (no minor unit)
	"XPF": 953, // CFP Franc
	"XPT": 962, // Platinum (no minor unit)
	"XSU": 994, // Sucre (no minor unit)
	"XTS": 963, // Codes specifically reserved for testing purposes (no minor unit)
	"XUA": 965, // ADB Unit of Account (no minor unit)
	"XXX": 999, // The codes assigned for transactions where no currency is involved (no minor unit)
	"YER": 886, // Yemeni Rial
	"ZAR": 710, // Rand
	"ZMW": 967, // Zambian Kwacha
	"ZWG": 924, // Zimbabwe Gold

	// Withdrawn, retained to match ISO4217Currencies. Dates from list three.
	"ANG": 532, // Netherlands Antillean guilder — withdrawn 2025-03
	"BGN": 975, // Bulgarian lev — withdrawn 2026-01
	"CUC": 931, // Cuban convertible peso — withdrawn 2021-06
	"HRK": 191, // Croatian kuna — withdrawn 2023-01
	"SLL": 694, // Sierra Leonean leone (old) — withdrawn 2023-12
	"ZWL": 932, // Zimbabwean dollar — withdrawn 2024-09
}

// GetNumericCode returns the ISO 4217 numeric code recorded for cur in
// numericCodes, for example 978 for "EUR" and 8 for "ALL".
//
// Use NumericCode.String when writing the value into a message or a ledger
// field: the canonical form of 8 is "008", and formatting it as a plain integer
// produces a code no ISO 20022 or card-network parser will accept.
//
// On a miss it returns NumericCodeUnknown (-1) and an error wrapping
// ErrMissingCurrencies. Codes with no minor unit, such as XAU, DO resolve here
// even though GetPrecision rejects them — a numeric code exists for them, an
// exponent does not.
func GetNumericCode(numericCodes map[string]NumericCode, cur string) (NumericCode, error) {
	asset := strings.ToUpper(cur)

	code, ok := numericCodes[asset]
	if !ok {
		return NumericCodeUnknown, fmt.Errorf("%s: %w", asset, ErrMissingCurrencies)
	}

	return code, nil
}
