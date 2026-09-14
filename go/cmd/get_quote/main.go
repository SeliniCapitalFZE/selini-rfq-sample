// get_quote requests an RFQ quote (POST /v1/quote_request) and prints it.
//
// --symbol and --quantity are required; --side picks which side(s) to price (BUY,
// SELL, or the default TWO_WAY) and --currency which leg the quantity is in. The
// response's quote_id is what view_quote and execute_quote take via --quote-id.
// Requesting a quote does not trade: the quote is firm until its expires_at and
// only trades via execute_quote or one of the *_flow commands.
package main

import "selini-rfq-samples/rfqclient"

func main() { rfqclient.Run(run) }

func run() error {
	fs := rfqclient.NewFlagSet("Request an RFQ quote (POST /v1/quote_request).",
		"go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 0.01",
		"go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 0.5 --side BUY",
		"go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 10000 --currency USDT")
	common := rfqclient.AddCommonFlags(fs)
	q := rfqclient.AddQuoteRequestFlags(fs)
	side := fs.String("side", "TWO_WAY", "which side(s) to price: BUY | SELL | TWO_WAY")
	rfqclient.Parse(fs, "config", "symbol", "quantity")

	symbol, quantity, sd, currency, err := rfqclient.ValidateQuoteParams(q.Symbol, q.Quantity, *side, q.Currency)
	if err != nil {
		return err
	}
	_, client, err := rfqclient.MakeClient(common)
	if err != nil {
		return err
	}

	rfqclient.Step("Requesting a %s quote for %s %s%s", sd, quantity, symbol, rfqclient.CurrencyNote(currency))
	quote, err := client.RequestQuote(symbol, quantity, sd, currency, q.QuoteRequestID)
	if err != nil || quote == nil { // nil quote: dry run
		return err
	}
	id := rfqclient.Str(quote, "quote_id")
	rfqclient.Say("%s", rfqclient.QuoteSummary(quote))
	rfqclient.Say("Next: view_quote --quote-id %s, or execute_quote --quote-id %s --side BUY|SELL", id, id)
	return nil
}
