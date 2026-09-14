package rfqclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	QuoteRequestPath = "/v1/quote_request"
	QuoteLookupPath  = "/v1/quotes/%s"
	ExecuteQuotePath = "/v1/execute_quote/%s"

	// DefaultTimeout is the per-request HTTP timeout; overridable with --timeout.
	DefaultTimeout = 30 * time.Second
)

// APIError is a non-2xx response. Code is the decoded error code, Body the parsed
// JSON object (nil if the body was not an object).
type APIError struct {
	Status     int
	Code       string
	Body       map[string]any
	RetryAfter string
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d %s", e.Status, e.Code) }

// TransportError is a DNS/TCP/TLS/timeout failure: no HTTP response was received.
type TransportError struct{ Msg string }

func (e *TransportError) Error() string { return e.Msg }

// Client signs and sends RFQ REST requests over a keep-alive connection. With
// dryRun it prints what would be sent and returns nil instead of using the network.
type Client struct {
	cfg     *Config
	dryRun  bool
	verbose bool
	http    *http.Client
}

// NewClient builds a Client. Credentials are required unless dryRun. Proxies from
// HTTP(S)_PROXY are ignored unless useEnvProxy, so a stray environment variable
// cannot silently reroute signed requests.
func NewClient(cfg *Config, dryRun, verbose, useEnvProxy bool) (*Client, error) {
	if !dryRun && (cfg.APIKey == "" || cfg.APISecret == "") {
		return nil, configErrorf(`config "secret" must provide "api_key" and "api_secret"`)
	}
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: cfg.Timeout,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
	}
	if useEnvProxy {
		transport.Proxy = http.ProxyFromEnvironment
	}
	return &Client{
		cfg:     cfg,
		dryRun:  dryRun,
		verbose: verbose,
		http:    &http.Client{Transport: transport, Timeout: cfg.Timeout},
	}, nil
}

// QuoteRequest is the POST /v1/quote_request body. Field order is the wire order.
type QuoteRequest struct {
	QuoteRequestID string `json:"quote_request_id"`
	Symbol         string `json:"symbol"`
	Quantity       string `json:"quantity"` // decimal string; the API preserves precision
	Side           string `json:"side"`
	Currency       string `json:"currency,omitempty"`
}

// ExecuteRequest is the POST /v1/execute_quote/{quote_id} body.
type ExecuteRequest struct {
	ClOrdID string `json:"cl_ord_id"`
	Side    string `json:"side"`
}

// RequestQuote is POST /v1/quote_request. Nothing is traded.
func (c *Client) RequestQuote(symbol, quantity, side, currency, quoteRequestID string) (map[string]any, error) {
	if quoteRequestID == "" {
		quoteRequestID = NewQuoteRequestID()
	}
	return c.Call("POST", QuoteRequestPath, QuoteRequest{
		QuoteRequestID: quoteRequestID,
		Symbol:         symbol,
		Quantity:       quantity,
		Side:           side,
		Currency:       currency,
	})
}

// GetQuote is GET /v1/quotes/{quote_id}. Read-only.
func (c *Client) GetQuote(quoteID string) (map[string]any, error) {
	id, err := ValidateQuoteID(quoteID)
	if err != nil {
		return nil, err
	}
	return c.Call("GET", fmt.Sprintf(QuoteLookupPath, id), nil)
}

// ExecuteQuote is POST /v1/execute_quote/{quote_id}. It places a real order.
func (c *Client) ExecuteQuote(quoteID, side, clOrdID string) (map[string]any, error) {
	id, err := ValidateQuoteID(quoteID)
	if err != nil {
		return nil, err
	}
	sd, err := NormalizeExecutionSide(side)
	if err != nil {
		return nil, err
	}
	if clOrdID == "" {
		clOrdID = NewClOrdID()
	}
	return c.Call("POST", fmt.Sprintf(ExecuteQuotePath, id), ExecuteRequest{ClOrdID: clOrdID, Side: sd})
}

// Call signs and sends one request, printing it and the response. It returns the
// decoded JSON object, or an *APIError on a non-2xx status. On a dry run it prints
// the request and returns nil, nil.
func (c *Client) Call(method, path string, payload any) (map[string]any, error) {
	var body []byte
	if payload != nil {
		var err error
		if body, err = CompactJSON(payload); err != nil {
			return nil, err
		}
	}
	target := c.cfg.BaseURL + path
	Say("%s %s", method, target)
	if payload != nil {
		ShowJSON(payload) // the same JSON goes on the wire without the whitespace
	}
	if c.dryRun {
		Say("(dry run: not sent)")
		return nil, nil
	}

	timestamp := UTCTimestamp()
	signed := SigningPayload(method, timestamp, c.cfg.Host, path, "", body)
	signature := Sign(c.cfg.APISecret, signed)

	var reader io.Reader = http.NoBody
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, &TransportError{Msg: fmt.Sprintf("%s %s: %v", method, target, err)}
	}
	req.Host = c.cfg.Host // sent verbatim so it always equals the signed HOST line
	// Headers are assigned directly rather than via Set so the names go out exactly
	// as documented instead of in Go's canonical capitalization.
	req.Header["Accept"] = []string{"application/json"}
	req.Header[APIKeyHeader] = []string{c.cfg.APIKey}
	req.Header[TimestampHeader] = []string{timestamp}
	req.Header[SignatureHeader] = []string{signature}
	if len(body) > 0 {
		req.Header["Content-Type"] = []string{"application/json"}
	}
	if c.verbose {
		Say("Signing payload (key %s): %s", MaskKey(c.cfg.APIKey), Humanize(signed))
		Say("%s: %s", SignatureHeader, signature)
	}

	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &TransportError{Msg: fmt.Sprintf("%s %s failed: %v", method, target, err)}
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, &TransportError{Msg: fmt.Sprintf("%s %s: reading the response failed: %v", method, target, err)}
	}
	elapsed := time.Since(started)

	Say("HTTP %d in %.0f ms", resp.StatusCode, float64(elapsed.Microseconds())/1000)
	text := strings.TrimSpace(string(raw))
	var data any
	if text != "" {
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber() // keep large integer ids exact
		var pretty bytes.Buffer
		if dec.Decode(&data) == nil && json.Indent(&pretty, []byte(text), "", "  ") == nil {
			fmt.Println(pretty.String()) // re-indented as received: key order preserved
		} else {
			data = nil
			Say("%s", text)
		}
	}
	obj, isObject := data.(map[string]any)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			Status:     resp.StatusCode,
			Code:       ErrorCode(resp.StatusCode, data),
			Body:       obj,
			RetryAfter: resp.Header.Get("Retry-After"),
		}
	}
	if !isObject {
		return nil, &APIError{Status: resp.StatusCode, Code: "response body is not a JSON object"}
	}
	return obj, nil
}

// ErrorCode extracts the best-effort error code from any of the API's error body shapes:
//
//	endpoint errors        {"error": "QUOTE_EXPIRED", "quote_id": ...}
//	authentication errors  {"detail": {"error": {"message": "AUTHENTICATION_FAILED", ...}}}
//	problem details        {"title": ..., "status": 401, "detail": "..."}
func ErrorCode(status int, data any) string {
	if m, ok := data.(map[string]any); ok {
		if v, ok := m["error"].(string); ok && v != "" {
			return v
		}
		switch detail := m["detail"].(type) {
		case map[string]any:
			if e, ok := detail["error"].(map[string]any); ok {
				if msg, ok := e["message"].(string); ok && msg != "" {
					return msg
				}
			}
		case string:
			if detail != "" {
				return detail
			}
		}
		if t, ok := m["title"].(string); ok && t != "" {
			return t
		}
	}
	return fmt.Sprintf("HTTP %d", status)
}
