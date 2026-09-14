// view_quote looks up an existing quote by id (GET /v1/quotes/{quote_id}) and prints it.
//
// Read-only. The lookup returns the same fields as the original quote response
// (minus quote_request_id). An unknown id, or a quote belonging to another
// account, is reported as HTTP 400 QUOTE_NOT_FOUND.
package main

import "selini-rfq-samples/rfqclient"

func main() { rfqclient.Run(run) }

func run() error {
	fs := rfqclient.NewFlagSet("Look up a quote by id (GET /v1/quotes/{quote_id}).",
		"go run ./cmd/view_quote --config ../config.json --quote-id 123456789")
	common := rfqclient.AddCommonFlags(fs)
	quoteID := fs.String("quote-id", "", "quote_id returned by get_quote (required)")
	rfqclient.Parse(fs, "config", "quote-id")

	id, err := rfqclient.ValidateQuoteID(*quoteID)
	if err != nil {
		return err
	}
	_, client, err := rfqclient.MakeClient(common)
	if err != nil {
		return err
	}

	rfqclient.Step("Viewing quote %s", id)
	quote, err := client.GetQuote(id)
	if err != nil || quote == nil { // nil quote: dry run
		return err
	}
	rfqclient.Say("%s", rfqclient.QuoteSummary(quote))
	return nil
}
