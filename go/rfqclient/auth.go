package rfqclient

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Header names carried on every signed request.
const (
	APIKeyHeader    = "SELINI-KEY"
	TimestampHeader = "SELINI-TS"
	SignatureHeader = "SELINI-SIGN"
)

// UTCTimestamp is ISO-8601 UTC with millisecond precision, e.g. 2026-01-02T03:04:05.678Z.
func UTCTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// SigningPayload is the exact bytes the server signs:
// METHOD\nTIMESTAMP\nHOST\nPATH[\nQUERY][\nBODY], with no trailing newline.
// QUERY is present only when the URL has one; BODY only when non-empty.
func SigningPayload(method, timestamp, host, path, query string, body []byte) []byte {
	parts := []string{strings.ToUpper(method), timestamp, host, path}
	if query != "" {
		parts = append(parts, query)
	}
	payload := []byte(strings.Join(parts, "\n"))
	if len(body) > 0 {
		payload = append(payload, '\n')
		payload = append(payload, body...)
	}
	return payload
}

// Sign is HMAC-SHA256 over the payload with the API secret, encoded as URL-safe
// base64 with the '=' padding kept.
func Sign(apiSecret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write(payload)
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// CompactJSON is the canonical request body: no whitespace, UTF-8 kept as is, no
// HTML escaping. The very same bytes are signed and sent.
func CompactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// MaskKey shows only the first and last four characters of an API key.
func MaskKey(key string) string {
	if len(key) <= 8 {
		if len(key) < 2 {
			return key + "..."
		}
		return key[:2] + "..."
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// Humanize renders a signing payload on one line, newlines shown as \n.
func Humanize(b []byte) string {
	return strings.ReplaceAll(string(b), "\n", `\n`)
}
