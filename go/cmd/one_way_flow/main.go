// one_way_flow requests a one-sided quote (BUY or SELL), views it, then executes it:
//
//  1. POST /v1/quote_request            request a BUY or SELL quote (one price)
//  2. GET  /v1/quotes/{quote_id}        view it back
//  3. POST /v1/execute_quote/{quote_id} execute it on that same side   (a REAL trade)
//
// --side BUY|SELL is the side you trade. It is sent with the quote request, so the
// quote is priced on that side only and can only be executed on that side. For a
// quote priced on both sides, see two_way_flow.
//
// Quotes are short-lived (a few seconds), so the calls run back to back over one
// keep-alive connection. --skip-execute stops after step 2 (nothing is traded),
// --skip-view goes straight from request to execution when the expiry is tight,
// and --dry-run prints the requests without sending anything.
package main

import (
	"fmt"

	"selini-rfq-samples/rfqclient"
)

func main() { rfqclient.Run(run) }

func run() error {
	fs := rfqclient.NewFlagSet("Request a BUY or SELL quote, view it, then execute it. Step 3 sends a real order.",
		"go run ./cmd/one_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL",
		"go run ./cmd/one_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY --skip-execute",
		"go run ./cmd/one_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY --dry-run")
	common := rfqclient.AddCommonFlags(fs)
	q := rfqclient.AddQuoteRequestFlags(fs)
	side := fs.String("side", "", "side you trade, requested and executed: BUY (at the ask) | SELL (at the bid) (required)")
	clOrdID := fs.String("clordid", "", "your unique client order id for the execution; auto-generated if omitted")
	skipView := fs.Bool("skip-view", false, "skip the GET /v1/quotes lookup between request and execution")
	skipExecute := fs.Bool("skip-execute", false, "stop after viewing the quote; do not execute")
	rfqclient.Parse(fs, "config", "symbol", "quantity", "side")

	sd, err := rfqclient.NormalizeExecutionSide(*side) // BUY or SELL, never TWO_WAY
	if err != nil {
		return err
	}
	symbol, quantity, sd, currency, err := rfqclient.ValidateQuoteParams(q.Symbol, q.Quantity, sd, q.Currency)
	if err != nil {
		return err
	}
	_, client, err := rfqclient.MakeClient(common)
	if err != nil {
		return err
	}
	dryRun := common.DryRun

	steps := 3
	if *skipView {
		steps--
	}
	if *skipExecute {
		steps--
	}
	n := 1

	// --- 1. request a one-sided quote -----------------------------------------------
	rfqclient.Step("Step %d of %d: request a %s quote for %s %s%s", n, steps, sd, quantity, symbol, rfqclient.CurrencyNote(currency))
	quote, err := client.RequestQuote(symbol, quantity, sd, currency, q.QuoteRequestID)
	if err != nil {
		return err
	}
	quoteID := "<quote_id>" // dry run: no quote_id to continue with; show the remaining requests anyway
	if !dryRun {
		rfqclient.Say("%s", rfqclient.QuoteSummary(quote))
		if quoteID, err = rfqclient.ValidateQuoteID(rfqclient.Str(quote, "quote_id")); err != nil {
			return err
		}
	}

	// --- 2. view it -----------------------------------------------------------------
	if !*skipView {
		n++
		rfqclient.Step("Step %d of %d: view quote %s", n, steps, quoteID)
		if dryRun {
			if _, err = client.Call("GET", fmt.Sprintf(rfqclient.QuoteLookupPath, quoteID), nil); err != nil {
				return err
			}
		} else {
			if quote, err = client.GetQuote(quoteID); err != nil {
				return err
			}
			rfqclient.Say("%s", rfqclient.QuoteSummary(quote))
		}
	}

	if *skipExecute {
		rfqclient.Step("Stopping before execution (--skip-execute); nothing traded")
		return nil
	}

	// --- 3. execute it on the same side ---------------------------------------------
	n++
	cl := *clOrdID
	if cl == "" {
		cl = rfqclient.NewClOrdID()
	}
	note := ""
	if !dryRun {
		// The server normalizes the side; make sure the quote really is on ours.
		if sd, err = rfqclient.ResolveExecutionSide(rfqclient.Str(quote, "side"), sd); err != nil {
			return err
		}
		if note = rfqclient.ExpiryNote(rfqclient.Str(quote, "expires_at")); note != "" {
			note = " (" + note + ")"
		}
	}
	rfqclient.Step("Step %d of %d: execute quote %s: %s at the %s, cl_ord_id %s%s",
		n, steps, quoteID, sd, rfqclient.PriceLeg(sd), cl, note)
	if dryRun {
		_, err = client.Call("POST", fmt.Sprintf(rfqclient.ExecuteQuotePath, quoteID),
			rfqclient.ExecuteRequest{ClOrdID: cl, Side: sd})
		return err
	}
	execution, err := client.ExecuteQuote(quoteID, sd, cl)
	if err != nil {
		return err
	}
	rfqclient.Say("%s", rfqclient.FillSummary(execution))
	return nil
}
