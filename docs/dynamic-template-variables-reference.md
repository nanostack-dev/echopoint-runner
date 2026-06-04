# Dynamic template variables — reference

Auto-generated from the generator registry (`pkg/dynamicvars`). Use any of these as `{{$name}}` (or `{{$name:arg}}`) anywhere a flow node template is resolved (URL, headers, body). Values are generated per execution, seeded from the execution id.


## address

| Variable | Description | Example |
|---|---|---|
| `{{$city}}` | A city. | `{{$city}}` |
| `{{$country}}` | A country name. | `{{$country}}` |
| `{{$countryAbbr}}` | A 2-letter country code. | `{{$countryAbbr}}` |
| `{{$latitude}}` | A latitude. | `{{$latitude}}` |
| `{{$longitude}}` | A longitude. | `{{$longitude}}` |
| `{{$state}}` | A state/region. | `{{$state}}` |
| `{{$stateAbbr}}` | A state abbreviation. | `{{$stateAbbr}}` |
| `{{$street}}` | A street address. | `{{$street}}` |
| `{{$timezone}}` | A timezone. | `{{$timezone}}` |
| `{{$zip}}` | A postal code. | `{{$zip}}` |

## commerce

| Variable | Description | Example |
|---|---|---|
| `{{$company}}` | A company name. | `{{$company}}` |
| `{{$productCategory}}` | A product category. | `{{$productCategory}}` |
| `{{$productName}}` | A product name. | `{{$productName}}` |
| `{{$sku}}` | An 8-char uppercase SKU. | `{{$sku}}` |

## contact

| Variable | Description | Example |
|---|---|---|
| `{{$email}}` | An email address. | `{{$email}}` |
| `{{$phone}}` | A phone number. | `{{$phone}}` |
| `{{$phoneFormatted}}` | A locally-formatted phone number. | `{{$phoneFormatted}}` |

## finance

| Variable | Description | Example |
|---|---|---|
| `{{$achAccount}}` | An ACH account number. | `{{$achAccount}}` |
| `{{$achRouting}}` | An ACH routing number. | `{{$achRouting}}` |
| `{{$bitcoinAddress}}` | A bitcoin address. | `{{$bitcoinAddress}}` |
| `{{$creditCard}}` | A Luhn-valid card number. Arg: visa, mastercard, american-express, discover. | `{{$creditCard:visa}}` |
| `{{$creditCardCvv}}` | A card CVV. | `{{$creditCardCvv}}` |
| `{{$currency}}` | A 3-letter currency code (ISO 4217). | `{{$currency}}` |
| `{{$price}}` | A price. Args: min max (defaults 1 1000). | `{{$price:9.99:199.99}}` |

## identity

| Variable | Description | Example |
|---|---|---|
| `{{$firstName}}` | A first name. | `{{$firstName}}` |
| `{{$fullName}}` | A full name. | `{{$fullName}}` |
| `{{$gender}}` | A gender. | `{{$gender}}` |
| `{{$jobTitle}}` | A job title. | `{{$jobTitle}}` |
| `{{$lastName}}` | A last name. | `{{$lastName}}` |
| `{{$ssn}}` | A US-format SSN (fake). | `{{$ssn}}` |
| `{{$username}}` | A username. | `{{$username}}` |

## internet

| Variable | Description | Example |
|---|---|---|
| `{{$domain}}` | A domain name. | `{{$domain}}` |
| `{{$emoji}}` | An emoji. | `{{$emoji}}` |
| `{{$httpMethod}}` | An HTTP method. | `{{$httpMethod}}` |
| `{{$ipv4}}` | An IPv4 address. | `{{$ipv4}}` |
| `{{$ipv6}}` | An IPv6 address. | `{{$ipv6}}` |
| `{{$mac}}` | A MAC address. | `{{$mac}}` |
| `{{$url}}` | A URL. | `{{$url}}` |
| `{{$userAgent}}` | A browser user-agent string. | `{{$userAgent}}` |

## primitive

| Variable | Description | Example |
|---|---|---|
| `{{$bool}}` | true or false. | `{{$bool}}` |
| `{{$digits}}` | N digits (default 6). | `{{$digits:4}}` |
| `{{$float}}` | A float. Args: min max (defaults 0 1000). | `{{$float:0:1}}` |
| `{{$int}}` | An integer. Args: min max (defaults 0 1000). | `{{$int:1:100}}` |
| `{{$letters}}` | N letters (default 8). | `{{$letters:10}}` |
| `{{$string}}` | N-char alphanumeric (default 16). | `{{$string:12}}` |
| `{{$uuid}}` | A UUIDv4. | `{{$uuid}}` |

## text

| Variable | Description | Example |
|---|---|---|
| `{{$color}}` | A colour name. | `{{$color}}` |
| `{{$hexColor}}` | A hex colour. | `{{$hexColor}}` |
| `{{$sentence}}` | A sentence of N words (default 8). | `{{$sentence:10}}` |
| `{{$slug}}` | A kebab-case slug of N words (default 3). | `{{$slug:2}}` |
| `{{$word}}` | A single word. | `{{$word}}` |
| `{{$words}}` | N words (default 3). | `{{$words:5}}` |

## time

| Variable | Description | Example |
|---|---|---|
| `{{$futureDate}}` | A random RFC3339 date in the future. | `{{$futureDate}}` |
| `{{$isoTimestamp}}` | RFC3339 timestamp at execution start (stable). | `{{$isoTimestamp}}` |
| `{{$pastDate}}` | A random RFC3339 date in the past. | `{{$pastDate}}` |
| `{{$runId}}` | Stable short id for this execution (same everywhere in one run). | `{{$runId}}` |
| `{{$timestamp}}` | Unix seconds at execution start (stable). | `{{$timestamp}}` |
| `{{$today}}` | Execution date as YYYY-MM-DD (stable). | `{{$today}}` |
| `{{$weekday}}` | A weekday name. | `{{$weekday}}` |

> Determinism: values are unique per run and seeded from the execution id. Stable-per-execution vars (`runId`, `timestamp`, `isoTimestamp`, `today`) return the same value everywhere in one run; all others are fresh per occurrence.
