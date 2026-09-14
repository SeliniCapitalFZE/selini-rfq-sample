package rfqclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ------------------------------------------------------------------ output

// Say prints one plain line saying what is happening.
func Say(format string, a ...any) { fmt.Printf(format+"\n", a...) }

// Step is Say preceded by a blank line: it opens a new stage of a command's output.
func Step(format string, a ...any) { fmt.Printf("\n"+format+"\n", a...) }

// PrintError prints an error line.
func PrintError(msg string) { fmt.Println("Error: " + msg) }

// ShowJSON pretty-prints a request body.
func ShowJSON(v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Printf("%v\n", v)
		return
	}
	fmt.Print(buf.String())
}

// ------------------------------------------------------------------ decode

// Str returns m[key] as a string; "" when absent or neither a string nor a number.
func Str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

func orQuestion(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// SecondsUntil is the time left until an API timestamp (yyyy-MM-ddTHH:mm:ss.fffZ);
// ok is false when it cannot be parsed.
func SecondsUntil(s string) (seconds float64, ok bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, false
	}
	return time.Until(t).Seconds(), true
}

// ExpiryNote is "2.9 s left on the quote", "quote expired 1.2 s ago", or "" when
// expires_at is unusable.
func ExpiryNote(expiresAt string) string {
	remaining, ok := SecondsUntil(expiresAt)
	if !ok {
		return ""
	}
	if remaining >= 0 {
		return fmt.Sprintf("%.1f s left on the quote", remaining)
	}
	return fmt.Sprintf("quote expired %.1f s ago", -remaining)
}

// PriceLeg names the quote price an execution trades at: BUY hits the ask, SELL the bid.
func PriceLeg(executionSide string) string {
	if executionSide == "BUY" {
		return "ask"
	}
	return "bid"
}

// CurrencyNote is " (quantity in USDT)", or "" for the default base leg.
func CurrencyNote(currency string) string {
	if currency == "" {
		return ""
	}
	return " (quantity in " + currency + ")"
}

// QuoteSummary is one line for a quote response, e.g.
// "Quote 123: bid 111000.1 / ask 111050.3, 2.9 s left on the quote".
func QuoteSummary(q map[string]any) string {
	var prices []string
	if bid := Str(q, "bid_price"); bid != "" {
		prices = append(prices, "bid "+bid)
	}
	if ask := Str(q, "ask_price"); ask != "" {
		prices = append(prices, "ask "+ask)
	}
	s := "Quote " + orQuestion(Str(q, "quote_id")) + ": "
	if len(prices) == 0 {
		s += "no prices"
	} else {
		s += strings.Join(prices, " / ")
	}
	if note := ExpiryNote(Str(q, "expires_at")); note != "" {
		s += ", " + note
	}
	return s
}

// FillSummary is one line for an execution response, e.g.
// "Filled: BUY 0.0001 BTC/USDT at 111050.3, notional 11.11 (order_id 1, exec_id 2)".
func FillSummary(e map[string]any) string {
	g := func(k string) string { return orQuestion(Str(e, k)) }
	return fmt.Sprintf("%s: %s %s %s at %s, notional %s (order_id %s, exec_id %s)",
		g("ord_status"), g("side"), g("quantity"), g("symbol"), g("price"), g("notional"),
		g("order_id"), g("exec_id"))
}

// ------------------------------------------------------------------ flags

// Common holds the flags every command takes.
type Common struct {
	Config      string
	DryRun      bool
	Verbose     bool
	UseEnvProxy bool
	Timeout     float64 // seconds
}

// QuoteRequestFlags holds the POST /v1/quote_request flags shared by get_quote and
// the flow commands. Each command adds its own --side, because what it means differs.
type QuoteRequestFlags struct {
	Symbol, Quantity, Currency, QuoteRequestID string
}

// NewFlagSet returns a FlagSet whose --help prints the description, the flags and
// the examples. Both --flag and -flag are accepted.
func NewFlagSet(description string, examples ...string) *flag.FlagSet {
	fs := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "usage: %s [flags]\n\n%s\n\nflags:\n", fs.Name(), description)
		fs.PrintDefaults()
		if len(examples) > 0 {
			fmt.Fprintln(w, "\nexamples:")
			for _, e := range examples {
				fmt.Fprintf(w, "  %s\n", e)
			}
		}
	}
	return fs
}

// AddCommonFlags registers --config, --dry-run, --verbose, --use-env-proxy and --timeout.
func AddCommonFlags(fs *flag.FlagSet) *Common {
	c := &Common{}
	fs.StringVar(&c.Config, "config", "", "JSON config holding base_url and secret{api_key, api_secret}, nothing else (required)")
	fs.BoolVar(&c.DryRun, "dry-run", false, "print the request(s) that would be sent and exit without sending")
	fs.BoolVar(&c.Verbose, "verbose", false, "also print the HMAC signing payload and signature")
	fs.BoolVar(&c.UseEnvProxy, "use-env-proxy", false, "honor HTTP_PROXY/HTTPS_PROXY (ignored by default)")
	fs.Float64Var(&c.Timeout, "timeout", DefaultTimeout.Seconds(), "HTTP timeout per request, in seconds")
	return c
}

// AddQuoteRequestFlags registers --symbol, --quantity, --currency and --quote-request-id.
func AddQuoteRequestFlags(fs *flag.FlagSet) *QuoteRequestFlags {
	q := &QuoteRequestFlags{}
	fs.StringVar(&q.Symbol, "symbol", "", "instrument as BASE/QUOTE, e.g. BTC/USDT (required)")
	fs.StringVar(&q.Quantity, "quantity", "", "positive decimal quantity, e.g. 0.25, denominated in --currency (required)")
	fs.StringVar(&q.Currency, "currency", "", "leg of the symbol the quantity is denominated in: the base (default) or the quote leg, e.g. USDT to size the request by notional")
	fs.StringVar(&q.QuoteRequestID, "quote-request-id", "", "your unique id for the quote request; auto-generated if omitted")
	return q
}

// Parse parses the command line (bad flags and --help are handled by the FlagSet)
// and exits 2 if any of the named flags is empty or a stray argument is present.
func Parse(fs *flag.FlagSet, required ...string) {
	_ = fs.Parse(os.Args[1:]) // ExitOnError: never returns an error
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "%s: error: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		os.Exit(2)
	}
	var missing []string
	for _, name := range required {
		if f := fs.Lookup(name); f == nil || strings.TrimSpace(f.Value.String()) == "" {
			missing = append(missing, "--"+name)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "%s: error: the following flags are required: %s\n", fs.Name(), strings.Join(missing, ", "))
		os.Exit(2)
	}
}

// MakeClient loads --config, applies --timeout and builds a Client, printing the
// target (or the dry-run banner).
func MakeClient(c *Common) (*Config, *Client, error) {
	cfg, err := Load(c.Config)
	if err != nil {
		return nil, nil, err
	}
	if !(c.Timeout > 0) { // phrased so NaN is rejected too
		return nil, nil, configErrorf("--timeout must be a positive number of seconds")
	}
	cfg.Timeout = time.Duration(c.Timeout * float64(time.Second))
	client, err := NewClient(cfg, c.DryRun, c.Verbose, c.UseEnvProxy)
	if err != nil {
		return nil, nil, err
	}
	if c.DryRun {
		Say("Dry run against %s: requests are printed, nothing is sent", cfg.BaseURL)
	} else {
		Say("Using %s with key %s", cfg.BaseURL, MaskKey(cfg.APIKey))
	}
	return cfg, client, nil
}

// Run executes a command and maps its error to the exit code: 0 ok, 1 API or
// transport failure, 2 usage or config error.
func Run(command func() error) {
	err := command()
	if err == nil {
		return
	}
	var apiErr *APIError
	var cfgErr *ConfigError
	switch {
	case errors.As(err, &apiErr):
		msg := apiErr.Error()
		if apiErr.RetryAfter != "" {
			msg += fmt.Sprintf(" (retry after %s s)", apiErr.RetryAfter)
		}
		PrintError(msg)
		os.Exit(1)
	case errors.As(err, &cfgErr):
		PrintError(err.Error())
		os.Exit(2)
	default:
		PrintError(err.Error())
		os.Exit(1)
	}
}
