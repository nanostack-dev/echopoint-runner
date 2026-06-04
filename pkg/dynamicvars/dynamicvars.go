// Package dynamicvars resolves {{$name}} template variables to generated fake
// data. The {{$...}} namespace is owned here; generators are backed by
// gofakeit for breadth and by hand for the validity-critical financial ones
// (IBAN, BIC), so e.g. {{$iban:FR}} passes a real mod-97 check.
//
// Determinism: a Context is seeded from the execution id, so a given execution
// produces a stable stream of values and different executions differ. Draws are
// mutex-guarded; under concurrent node execution the draw order (and thus the
// exact values) can vary between runs — values are guaranteed unique-per-run,
// not yet identical on replay. See docs/dynamic-template-variables-design.md.
package dynamicvars

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

// Generator categories.
const (
	catTime      = "time"
	catIdentity  = "identity"
	catContact   = "contact"
	catAddress   = "address"
	catFinance   = "finance"
	catInternet  = "internet"
	catCommerce  = "commerce"
	catText      = "text"
	catPrimitive = "primitive"
)

const (
	seedHashByteCount = 8
	runIDByteLen      = 6
)

// Context holds the per-execution generator state.
type Context struct {
	mu      sync.Mutex
	faker   *gofakeit.Faker
	runID   string
	started time.Time
}

// New builds a Context seeded deterministically from the execution id.
func New(executionID string) *Context {
	sum := sha256.Sum256([]byte(executionID))
	seed := binary.BigEndian.Uint64(sum[:seedHashByteCount])
	return &Context{
		faker:   gofakeit.New(seed),
		runID:   hex.EncodeToString(sum[:runIDByteLen]),
		started: time.Now().UTC(),
	}
}

// Resolve returns the value for $name with optional colon-separated args.
// It implements the resolver interface the node package expects.
func (c *Context) Resolve(name string, args []string) (string, error) {
	entry, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown dynamic variable {{$%s}}", name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return entry.gen(c, args)
}

// Entry documents one generator. Desc and Example feed `flows vars` / docs.
type Entry struct {
	Name     string
	Category string
	Desc     string
	Example  string
	gen      func(*Context, []string) (string, error)
}

// Catalog returns every generator, sorted by category then name, for
// documentation and discovery by users and AI agents.
func Catalog() []Entry {
	out := make([]Entry, 0, len(registry))
	for name, e := range registry {
		e.Name = name
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// --- helpers ---

func argInt(args []string, idx, def int) int {
	if idx < len(args) {
		if v, err := strconv.Atoi(args[idx]); err == nil {
			return v
		}
	}
	return def
}

func argFloat(args []string, idx int, def float64) float64 {
	if idx < len(args) {
		if v, err := strconv.ParseFloat(args[idx], 64); err == nil {
			return v
		}
	}
	return def
}

//nolint:gochecknoglobals,mnd // generator registry is static config with inline defaults
var registry = map[string]Entry{
	// ---- run / time (stable per execution) ----
	"runId": {
		Category: catTime, Example: "{{$runId}}",
		Desc: "Stable short id for this execution (same everywhere in one run).",
		gen:  func(c *Context, _ []string) (string, error) { return c.runID, nil },
	},
	"timestamp": {
		Category: catTime,
		Desc:     "Unix seconds at execution start (stable).",
		Example:  "{{$timestamp}}",
		gen:      func(c *Context, _ []string) (string, error) { return strconv.FormatInt(c.started.Unix(), 10), nil },
	},
	"isoTimestamp": {
		Category: catTime,
		Desc:     "RFC3339 timestamp at execution start (stable).",
		Example:  "{{$isoTimestamp}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.started.Format(time.RFC3339), nil },
	},
	"today": {
		Category: catTime,
		Desc:     "Execution date as YYYY-MM-DD (stable).",
		Example:  "{{$today}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.started.Format("2006-01-02"), nil },
	},
	"futureDate": {
		Category: catTime,
		Desc:     "A random RFC3339 date in the future.",
		Example:  "{{$futureDate}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.FutureDate().Format(time.RFC3339), nil },
	},
	"pastDate": {
		Category: catTime,
		Desc:     "A random RFC3339 date in the past.",
		Example:  "{{$pastDate}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.PastDate().Format(time.RFC3339), nil },
	},
	"weekday": {Category: catTime, Desc: "A weekday name.", Example: "{{$weekday}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.WeekDay(), nil }},

	// ---- identity ----
	"firstName": {Category: catIdentity, Desc: "A first name.", Example: "{{$firstName}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.FirstName(), nil }},
	"lastName": {Category: catIdentity, Desc: "A last name.", Example: "{{$lastName}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.LastName(), nil }},
	"fullName": {Category: catIdentity, Desc: "A full name.", Example: "{{$fullName}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Name(), nil }},
	"username": {Category: catIdentity, Desc: "A username.", Example: "{{$username}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Username(), nil }},
	"gender": {Category: catIdentity, Desc: "A gender.", Example: "{{$gender}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Gender(), nil }},
	"jobTitle": {Category: catIdentity, Desc: "A job title.", Example: "{{$jobTitle}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.JobTitle(), nil }},
	"ssn": {Category: catIdentity, Desc: "A US-format SSN (fake).", Example: "{{$ssn}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.SSN(), nil }},

	// ---- contact ----
	"email": {Category: catContact, Desc: "An email address.", Example: "{{$email}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Email(), nil }},
	"phone": {Category: catContact, Desc: "A phone number.", Example: "{{$phone}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Phone(), nil }},
	"phoneFormatted": {
		Category: catContact,
		Desc:     "A locally-formatted phone number.",
		Example:  "{{$phoneFormatted}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.PhoneFormatted(), nil },
	},

	// ---- address ----
	"street": {Category: catAddress, Desc: "A street address.", Example: "{{$street}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Street(), nil }},
	"city": {Category: catAddress, Desc: "A city.", Example: "{{$city}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.City(), nil }},
	"state": {Category: catAddress, Desc: "A state/region.", Example: "{{$state}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.State(), nil }},
	"stateAbbr": {Category: catAddress, Desc: "A state abbreviation.", Example: "{{$stateAbbr}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.StateAbr(), nil }},
	"zip": {Category: catAddress, Desc: "A postal code.", Example: "{{$zip}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Zip(), nil }},
	"country": {Category: catAddress, Desc: "A country name.", Example: "{{$country}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Country(), nil }},
	"countryAbbr": {
		Category: catAddress,
		Desc:     "A 2-letter country code.",
		Example:  "{{$countryAbbr}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.CountryAbr(), nil },
	},
	"latitude": {Category: catAddress, Desc: "A latitude.", Example: "{{$latitude}}",
		gen: func(c *Context, _ []string) (string, error) {
			return strconv.FormatFloat(c.faker.Latitude(), 'f', 6, 64), nil
		}},
	"longitude": {Category: catAddress, Desc: "A longitude.", Example: "{{$longitude}}",
		gen: func(c *Context, _ []string) (string, error) {
			return strconv.FormatFloat(c.faker.Longitude(), 'f', 6, 64), nil
		}},
	"timezone": {Category: catAddress, Desc: "A timezone.", Example: "{{$timezone}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.TimeZone(), nil }},

	// ---- finance ----
	"creditCard": {
		Category: catFinance,
		Desc:     "A Luhn-valid card number. Arg: visa, mastercard, american-express, discover.",
		Example:  "{{$creditCard:visa}}",
		gen: func(c *Context, args []string) (string, error) {
			opts := &gofakeit.CreditCardOptions{}
			if len(args) > 0 {
				opts.Types = []string{args[0]}
			}
			return c.faker.CreditCardNumber(opts), nil
		},
	},
	"creditCardCvv": {Category: catFinance, Desc: "A card CVV.", Example: "{{$creditCardCvv}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.CreditCardCvv(), nil }},
	"currency": {
		Category: catFinance,
		Desc:     "A 3-letter currency code (ISO 4217).",
		Example:  "{{$currency}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.CurrencyShort(), nil },
	},
	"price": {
		Category: catFinance,
		Desc:     "A price. Args: min max (defaults 1 1000).",
		Example:  "{{$price:9.99:199.99}}",
		gen: func(c *Context, args []string) (string, error) {
			return strconv.FormatFloat(
				c.faker.Price(argFloat(args, 0, 1), argFloat(args, 1, 1000)),
				'f',
				2,
				64,
			), nil
		},
	},
	"achRouting": {Category: catFinance, Desc: "An ACH routing number.", Example: "{{$achRouting}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.AchRouting(), nil }},
	"achAccount": {Category: catFinance, Desc: "An ACH account number.", Example: "{{$achAccount}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.AchAccount(), nil }},
	"bitcoinAddress": {
		Category: catFinance,
		Desc:     "A bitcoin address.",
		Example:  "{{$bitcoinAddress}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.BitcoinAddress(), nil },
	},

	// ---- internet / tech ----
	"url": {Category: catInternet, Desc: "A URL.", Example: "{{$url}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.URL(), nil }},
	"domain": {Category: catInternet, Desc: "A domain name.", Example: "{{$domain}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.DomainName(), nil }},
	"ipv4": {Category: catInternet, Desc: "An IPv4 address.", Example: "{{$ipv4}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.IPv4Address(), nil }},
	"ipv6": {Category: catInternet, Desc: "An IPv6 address.", Example: "{{$ipv6}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.IPv6Address(), nil }},
	"mac": {Category: catInternet, Desc: "A MAC address.", Example: "{{$mac}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.MacAddress(), nil }},
	"userAgent": {
		Category: catInternet,
		Desc:     "A browser user-agent string.",
		Example:  "{{$userAgent}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.UserAgent(), nil },
	},
	"httpMethod": {Category: catInternet, Desc: "An HTTP method.", Example: "{{$httpMethod}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.HTTPMethod(), nil }},
	"emoji": {Category: catInternet, Desc: "An emoji.", Example: "{{$emoji}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Emoji(), nil }},

	// ---- commerce / company ----
	"company": {Category: catCommerce, Desc: "A company name.", Example: "{{$company}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Company(), nil }},
	"productName": {Category: catCommerce, Desc: "A product name.", Example: "{{$productName}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.ProductName(), nil }},
	"productCategory": {
		Category: catCommerce,
		Desc:     "A product category.",
		Example:  "{{$productCategory}}",
		gen:      func(c *Context, _ []string) (string, error) { return c.faker.ProductCategory(), nil },
	},
	"sku": {Category: catCommerce, Desc: "An 8-char uppercase SKU.", Example: "{{$sku}}",
		gen: func(c *Context, _ []string) (string, error) {
			return strings.ToUpper(c.faker.LetterN(4)) + c.faker.DigitN(4), nil
		}},

	// ---- text / primitives ----
	"word": {Category: catText, Desc: "A single word.", Example: "{{$word}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Word(), nil }},
	"words": {Category: catText, Desc: "N words (default 3).", Example: "{{$words:5}}",
		gen: func(c *Context, args []string) (string, error) {
			n := argInt(args, 0, 3)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = c.faker.Word()
			}
			return strings.Join(parts, " "), nil
		}},
	"sentence": {
		Category: catText,
		Desc:     "A sentence of N words (default 8).",
		Example:  "{{$sentence:10}}",
		gen:      func(c *Context, args []string) (string, error) { return c.faker.Sentence(argInt(args, 0, 8)), nil },
	},
	"slug": {
		Category: catText,
		Desc:     "A kebab-case slug of N words (default 3).",
		Example:  "{{$slug:2}}",
		gen: func(c *Context, args []string) (string, error) {
			n := argInt(args, 0, 3)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = strings.ToLower(c.faker.Word())
			}
			return strings.Join(parts, "-"), nil
		},
	},
	"color": {Category: catText, Desc: "A colour name.", Example: "{{$color}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.Color(), nil }},
	"hexColor": {Category: catText, Desc: "A hex colour.", Example: "{{$hexColor}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.HexColor(), nil }},
	"int": {
		Category: catPrimitive,
		Desc:     "An integer. Args: min max (defaults 0 1000).",
		Example:  "{{$int:1:100}}",
		gen: func(c *Context, args []string) (string, error) {
			return strconv.Itoa(c.faker.Number(argInt(args, 0, 0), argInt(args, 1, 1000))), nil
		},
	},
	"float": {
		Category: catPrimitive,
		Desc:     "A float. Args: min max (defaults 0 1000).",
		Example:  "{{$float:0:1}}",
		gen: func(c *Context, args []string) (string, error) {
			return strconv.FormatFloat(
				c.faker.Float64Range(argFloat(args, 0, 0), argFloat(args, 1, 1000)),
				'f',
				4,
				64,
			), nil
		},
	},
	"bool": {Category: catPrimitive, Desc: "true or false.", Example: "{{$bool}}",
		gen: func(c *Context, _ []string) (string, error) { return strconv.FormatBool(c.faker.Bool()), nil }},
	"digits": {Category: catPrimitive, Desc: "N digits (default 6).", Example: "{{$digits:4}}",
		gen: func(c *Context, args []string) (string, error) { return c.faker.DigitN(uint(argInt(args, 0, 6))), nil }},
	"letters": {Category: catPrimitive, Desc: "N letters (default 8).", Example: "{{$letters:10}}",
		gen: func(c *Context, args []string) (string, error) { return c.faker.LetterN(uint(argInt(args, 0, 8))), nil }},
	"string": {
		Category: catPrimitive,
		Desc:     "N-char alphanumeric (default 16).",
		Example:  "{{$string:12}}",
		gen: func(c *Context, args []string) (string, error) {
			return c.faker.Lexify(strings.Repeat("?", argInt(args, 0, 16))), nil
		},
	},
	"uuid": {Category: catPrimitive, Desc: "A UUIDv4.", Example: "{{$uuid}}",
		gen: func(c *Context, _ []string) (string, error) { return c.faker.UUID(), nil }},
}
