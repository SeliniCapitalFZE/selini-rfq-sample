// Package rfqclient is the shared config/auth/HTTP layer for the Selini Go RFQ
// sample clients: get_quote, view_quote, execute_quote, one_way_flow and
// two_way_flow. Standard library only.
//
// Every request carries three headers, SELINI-KEY, SELINI-TS and SELINI-SIGN; the
// last is an HMAC-SHA256 over METHOD\nTIMESTAMP\nHOST\nPATH[\nQUERY][\nBODY],
// URL-safe base64 encoded (see auth.go). Requests go over one keep-alive
// connection, so a request/view/execute sequence pays the TCP+TLS handshake once;
// quotes live only a few seconds, so that latency matters.
//
// Output is one ordered stream on stdout: for every call, a plain line saying what
// is happening, then the request (method, URL, pretty-printed JSON body) and the
// response (status, latency, pretty-printed body).
package rfqclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// ConfigError is a bad config key or command-line value; commands exit 2 on it.
type ConfigError struct{ Msg string }

func (e *ConfigError) Error() string { return e.Msg }

func configErrorf(format string, a ...any) error {
	return &ConfigError{Msg: fmt.Sprintf(format, a...)}
}

// Config is the parsed config.json plus the connection settings from the command line.
type Config struct {
	BaseURL   string // scheme://host[:port], no path
	Host      string // derived: the Host header value used in the signature
	APIKey    string
	APISecret string
	Timeout   time.Duration // from --timeout, never from the config file
}

var configKeys = map[string]bool{"base_url": true, "secret": true}

// Load reads config.json. It holds the endpoint and credentials and nothing else:
//
//	{"base_url": "https://...", "secret": {"api_key": "...", "api_secret": "..."}}
//
// What to trade (symbol, quantity, sides) and the HTTP timeout are command-line
// flags. Credentials are validated lazily by NewClient (not needed for --dry-run).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, configErrorf("cannot open config file: %s", path)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, configErrorf("invalid config JSON in %s: %v", path, err)
	}
	if root == nil {
		return nil, configErrorf("config must be a JSON object")
	}
	keys := make([]string, 0, len(root))
	for k := range root {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if configKeys[key] {
			continue
		}
		return nil, configErrorf("config key %q is not supported; the config holds only base_url and secret", key)
	}

	baseURL, err := reqString(root, "base_url")
	if err != nil {
		return nil, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, configErrorf(`config key "base_url" must be scheme://host[:port] with no path, e.g. "https://api.example.com"`)
	}

	secret, err := optObject(root, "secret")
	if err != nil {
		return nil, err
	}
	apiKey, err := optString(secret, "api_key")
	if err != nil {
		return nil, err
	}
	apiSecret, err := optString(secret, "api_secret")
	if err != nil {
		return nil, err
	}
	return &Config{
		BaseURL:   baseURL,
		Host:      u.Host,
		APIKey:    strings.TrimSpace(apiKey),
		APISecret: strings.TrimSpace(apiSecret),
		Timeout:   DefaultTimeout,
	}, nil
}

func isNull(v json.RawMessage) bool { return strings.TrimSpace(string(v)) == "null" }

func asString(v json.RawMessage, key string) (string, error) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", configErrorf("config key %q must be a string", key)
	}
	return s, nil
}

func reqString(d map[string]json.RawMessage, key string) (string, error) {
	v, ok := d[key]
	if !ok || isNull(v) {
		return "", configErrorf("config key %q is required", key)
	}
	return asString(v, key)
}

// optString returns "" when the key is absent.
func optString(d map[string]json.RawMessage, key string) (string, error) {
	v, ok := d[key]
	if !ok || isNull(v) {
		return "", nil
	}
	return asString(v, key)
}

// optObject returns an empty map when the key is absent.
func optObject(d map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	v, ok := d[key]
	if !ok || isNull(v) {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(v, &m); err != nil || m == nil {
		return nil, configErrorf("config key %q must be an object", key)
	}
	return m, nil
}
