// execute_quote executes (trades on) an existing quote: POST /v1/execute_quote/{quote_id}.
//
// THIS PLACES A REAL TRADE against the configured endpoint. --quote-id and --side
// are both required. The quote must still be live (before its expires_at) and the
// side must be compatible with the quote: BUY executes at the quote's ask_price,
// SELL at its bid_price; a one-sided quote can only be executed on its own side.
// Pass --dry-run to print the request without sending it.
package main

import "selini-rfq-samples/rfqclient"

func main() { rfqclient.Run(run) }

func run() error {
	fs := rfqclient.NewFlagSet("Execute a quote (POST /v1/execute_quote/{quote_id}). Sends a real order.",
		"go run ./cmd/execute_quote --config ../config.json --quote-id 123456789 --side BUY",
		"go run ./cmd/execute_quote --config ../config.json --quote-id 123456789 --side SELL --dry-run")
	common := rfqclient.AddCommonFlags(fs)
	quoteID := fs.String("quote-id", "", "quote_id returned by get_quote (required)")
	side := fs.String("side", "", "BUY (trade at the quote's ask_price) | SELL (trade at its bid_price) (required)")
	clOrdID := fs.String("clordid", "", "your unique client order id (cl_ord_id); auto-generated if omitted")
	rfqclient.Parse(fs, "config", "quote-id", "side")

	id, err := rfqclient.ValidateQuoteID(*quoteID)
	if err != nil {
		return err
	}
	sd, err := rfqclient.NormalizeExecutionSide(*side)
	if err != nil {
		return err
	}
	cl := *clOrdID
	if cl == "" {
		cl = rfqclient.NewClOrdID()
	}
	_, client, err := rfqclient.MakeClient(common)
	if err != nil {
		return err
	}

	rfqclient.Step("Executing quote %s: %s at the %s, cl_ord_id %s", id, sd, rfqclient.PriceLeg(sd), cl)
	execution, err := client.ExecuteQuote(id, sd, cl)
	if err != nil || execution == nil { // nil execution: dry run
		return err
	}
	rfqclient.Say("%s", rfqclient.FillSummary(execution))
	return nil
}
