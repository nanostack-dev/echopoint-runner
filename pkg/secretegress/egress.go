// Package secretegress stops a flow from sending a secret to a host that is
// not allowed to receive it.
package secretegress

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInURL is returned when a secret value is placed in a request URL.
var ErrInURL = errors.New("a secret cannot be placed in a URL")

// ErrHost is returned when a secret value is sent to a host that is not allowed.
var ErrHost = errors.New("a secret cannot be sent to this host")

// Check refuses a request that carries a secret value.
//
// A secret in the URL is always refused. A secret in a header or a body is
// refused unless hosts lists that request host for the secret's key.
//
// Example: value "s3cret" in "https://evil.example/a?token=s3cret" returns
// ErrInURL. The same value in an Authorization header is allowed only when
// hosts["API_KEY"] contains "api.example.com" and the URL host is that name.
func Check(
	values map[string]string,
	hosts map[string][]string,
	rawURL string,
	headers map[string]string,
	body any,
) error {
	if len(values) == 0 {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse request URL: %w", err)
	}
	requestHost := strings.ToLower(parsed.Hostname())

	for key, secret := range values {
		if secret == "" {
			continue
		}
		if strings.Contains(rawURL, secret) {
			return fmt.Errorf("%w: %s", ErrInURL, key)
		}
		if !carries(secret, headers, body) {
			continue
		}
		if !hostAllowed(requestHost, hosts[key]) {
			return fmt.Errorf("%w: %s to %s", ErrHost, key, requestHost)
		}
	}
	return nil
}

func carries(secret string, headers map[string]string, body any) bool {
	for key, value := range headers {
		if strings.Contains(key, secret) || strings.Contains(value, secret) {
			return true
		}
	}
	if body == nil {
		return false
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return strings.Contains(fmt.Sprint(body), secret)
	}
	return strings.Contains(string(encoded), secret)
}

// PublicMessage is the refusal text safe to show to a caller.
// A surrounding URL error is dropped, because its URL can hold the secret.
func PublicMessage(err error) string {
	for err != nil {
		var urlErr *url.Error
		if !errors.As(err, &urlErr) || urlErr.Err == nil {
			return err.Error()
		}
		err = urlErr.Err
	}
	return ""
}

func hostAllowed(host string, allowed []string) bool {
	if host == "" {
		return false
	}
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), host) {
			return true
		}
	}
	return false
}
