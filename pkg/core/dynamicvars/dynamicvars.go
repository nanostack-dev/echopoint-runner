// Package dynamicvars resolves {{$name:args}} template variables to freshly
// generated values (uuids, emails, timestamps, ...). It satisfies node.Resolver.
package dynamicvars

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"time"
)

const (
	uuidBytes       = 16
	emailLocalBytes = 6
	defaultHexBytes = 8
	defaultMaxInt   = 1_000_000
)

// Generators is a node.Resolver backed by built-in value generators.
type Generators struct{}

// New returns the default set of dynamic-variable generators.
func New() *Generators { return &Generators{} }

// Resolve produces a value for a dynamic variable name (with optional args).
func (Generators) Resolve(name string, args []string) (string, error) {
	switch name {
	case "uuid":
		return newHex(uuidBytes), nil
	case "hex":
		return newHex(argInt(args, defaultHexBytes)), nil
	case "email":
		return newHex(emailLocalBytes) + "@example.com", nil
	case "timestamp", "now":
		return strconv.FormatInt(time.Now().Unix(), 10), nil
	case "int", "randomInt":
		return randInt(int64(argInt(args, defaultMaxInt))), nil
	default:
		return "", fmt.Errorf("unknown dynamic variable %q", name)
	}
}

func newHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randInt(maxExclusive int64) string {
	if maxExclusive <= 0 {
		maxExclusive = defaultMaxInt
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(maxExclusive))
	return n.String()
}

func argInt(args []string, fallback int) int {
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}
