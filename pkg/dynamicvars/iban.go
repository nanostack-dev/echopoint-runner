package dynamicvars

import (
	"fmt"
	"strings"
)

const (
	ibanMod97          = 97
	ibanCheckBase      = 98
	letterToNumberBase = 10 // A -> 10, B -> 11, ...
	twoDigitThreshold  = 10 // values >= this occupy two decimal places
	ibanPrefixLen      = 4  // 2 country letters + 2 check digits
	bicBankLen         = 4
	bicCountryLen      = 2
)

// ibanLengths maps a country code to its total IBAN length (ISO 13616).
//
//nolint:gochecknoglobals,mnd // static reference data
var ibanLengths = map[string]int{
	"FR": 27, "DE": 22, "GB": 22, "ES": 24, "IT": 27,
	"NL": 18, "BE": 16, "CH": 21, "PT": 25, "IE": 22,
}

// genIBAN produces an IBAN with a correct mod-97 check digit. The BBAN is
// random digits of the country-appropriate length, which is sufficient for a
// value that passes IBAN validation (mod-97 == 1).
func genIBAN(c *Context, country string) (string, error) {
	total, ok := ibanLengths[country]
	if !ok {
		return "", fmt.Errorf(
			"$iban: unsupported country %q (supported: %s)",
			country,
			supportedIBANCountries(),
		)
	}
	bban := c.faker.DigitN(uint(total - ibanPrefixLen)) // 4 = 2 country + 2 check
	check := ibanCheckDigits(country, bban)
	return country + check + bban, nil
}

// ibanCheckDigits computes the two check digits per ISO 13616: move the country
// code + "00" to the end, map letters to numbers (A=10..Z=35), and compute
// 98 - (number mod 97), zero-padded to two digits.
func ibanCheckDigits(country, bban string) string {
	rearranged := bban + country + "00"
	remainder := 0
	for _, r := range rearranged {
		var value int
		switch {
		case r >= '0' && r <= '9':
			value = int(r - '0')
		case r >= 'A' && r <= 'Z':
			value = int(r-'A') + letterToNumberBase
		default:
			continue
		}
		// Fold one or two decimal digits in at a time, mod 97.
		if value >= twoDigitThreshold {
			remainder = (remainder*100 + value) % ibanMod97
		} else {
			remainder = (remainder*10 + value) % ibanMod97
		}
	}
	check := ibanCheckBase - remainder
	return fmt.Sprintf("%02d", check)
}

func supportedIBANCountries() string {
	keys := make([]string, 0, len(ibanLengths))
	for k := range ibanLengths {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// genBIC produces a structurally valid BIC/SWIFT code: 4 bank letters,
// 2 country letters, 2 alphanumeric location chars, and an optional 3-char
// branch (making it 8 or 11 characters total).
func genBIC(c *Context) string {
	bank := strings.ToUpper(c.faker.LetterN(bicBankLen))
	country := strings.ToUpper(c.faker.LetterN(bicCountryLen))
	location := strings.ToUpper(c.faker.Lexify("??"))
	bic := bank + country + location
	if c.faker.Bool() {
		bic += strings.ToUpper(c.faker.Lexify("???"))
	}
	return bic
}
