package rfqclient

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var amountRe = regexp.MustCompile(`^(\d+(\.\d*)?|\.\d+)$`)

// ValidateAmount checks a positive decimal string with no sign, exponent, or
// thousands separators (the API's Amount format) and returns it trimmed.
func ValidateAmount(value, name string) (string, error) {
	s := strings.TrimSpace(value)
	if !amountRe.MatchString(s) {
		return "", configErrorf("%s must be a plain decimal such as 0.25 (got %q)", name, s)
	}
	if strings.Trim(s, "0.") == "" {
		return "", configErrorf("%s must be greater than zero", name)
	}
	return s, nil
}

// NormalizeQuoteSide returns BUY, SELL or TWO_WAY (case-insensitive; TWOWAY, 2WAY
// and TWO-WAY are accepted). Empty means TWO_WAY.
func NormalizeQuoteSide(value string) (string, error) {
	s := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(value)), "-", "_")
	switch s {
	case "", "TWO_WAY", "TWOWAY", "2WAY":
		return "TWO_WAY", nil
	case "BUY", "SELL":
		return s, nil
	}
	return "", configErrorf("side must be one of BUY, SELL, TWO_WAY (got %q)", value)
}

// NormalizeExecutionSide returns BUY or SELL.
func NormalizeExecutionSide(value string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(value))
	if s == "BUY" || s == "SELL" {
		return s, nil
	}
	return "", configErrorf("execution side must be BUY or SELL (got %q)", value)
}

// ValidateQuoteID checks a positive integer string.
func ValidateQuoteID(value string) (string, error) {
	s := strings.TrimSpace(value)
	if s == "" || strings.Trim(s, "0123456789") != "" || strings.Trim(s, "0") == "" {
		return "", configErrorf("quote id must be a positive integer string (got %q)", value)
	}
	return s, nil
}

// ValidateQuoteParams validates and normalizes the quote-request flags. Side
// defaults to TWO_WAY; currency may be "" (the API then assumes the base leg).
func ValidateQuoteParams(symbol, quantity, side, currency string) (sym, qty, sd, ccy string, err error) {
	sym = strings.TrimSpace(symbol)
	if sym == "" {
		return "", "", "", "", configErrorf("--symbol is required")
	}
	if qty, err = ValidateAmount(quantity, "--quantity"); err != nil {
		return "", "", "", "", err
	}
	if sd, err = NormalizeQuoteSide(side); err != nil {
		return "", "", "", "", err
	}
	return sym, qty, sd, strings.ToUpper(strings.TrimSpace(currency)), nil
}

// ResolveExecutionSide picks the execution side for a quote. A one-sided quote can
// only be executed on its own side (an explicit requested side that disagrees is an
// error). A TWO_WAY quote executes on requested (the flow commands' --side), else BUY.
func ResolveExecutionSide(quoteSide, requested string) (string, error) {
	qs, err := NormalizeQuoteSide(quoteSide)
	if err != nil {
		return "", err
	}
	if qs == "BUY" || qs == "SELL" {
		if requested != "" {
			r, err := NormalizeExecutionSide(requested)
			if err != nil {
				return "", err
			}
			if r != qs {
				return "", configErrorf("quote is %s-only; it cannot be executed as %s", qs, r)
			}
		}
		return qs, nil
	}
	if requested == "" {
		requested = "BUY"
	}
	return NormalizeExecutionSide(requested)
}

// NewQuoteRequestID mints a unique quote_request_id.
func NewQuoteRequestID() string { return "rfq-" + randomHex(16) }

// NewClOrdID mints a unique cl_ord_id.
func NewClOrdID() string { return "exec-" + randomHex(16) }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
