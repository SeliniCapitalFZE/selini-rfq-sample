// two_way_flow requests a TWO_WAY quote, views it, then executes one side of it:
//
//  1. POST /v1/quote_request            request a two-way quote (bid and ask)
//  2. GET  /v1/quotes/{quote_id}        view it back
//  3. POST /v1/execute_quote/{quote_id} execute --side of it   (a REAL trade)
//
// A two-way request is priced on both sides: --side (BUY trades at the ask, SELL
// at the bid) is only sent at execution. It is required unless --skip-execute.
// For a quote priced on one side only, see one_way_flow.
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
	fs := rfqclient.NewFlagSet("Request a two-way quote, view it, then execute one side of it. Step 3 sends a real order.",
		"go run ./cmd/two_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY",
		"go run ./cmd/two_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --skip-execute",
		"go run ./cmd/two_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL --dry-run")
	common := rfqclient.AddCommonFlags(fs)
	q := rfqclient.AddQuoteRequestFlags(fs)
	side := fs.String("side", "", "side to execute: BUY (at the ask) | SELL (at the bid); required unless --skip-execute")
	clOrdID := fs.String("clordid", "", "your unique client order id for the execution; auto-generated if omitted")
	skipView := fs.Bool("skip-view", false, "skip the GET /v1/quotes lookup between request and execution")
	skipExecute := fs.Bool("skip-execute", false, "stop after viewing the quote; do not execute")
	rfqclient.Parse(fs, "config", "symbol", "quantity")

	symbol, quantity, quoteSide, currency, err := rfqclient.ValidateQuoteParams(q.Symbol, q.Quantity, "TWO_WAY", q.Currency)
	if err != nil {
		return err
	}
	executeSide := ""
	if !*skipExecute {
		if *side == "" {
			return &rfqclient.ConfigError{Msg: "--side BUY|SELL is required unless --skip-execute"}
		}
		if executeSide, err = rfqclient.NormalizeExecutionSide(*side); err != nil {
			return err
		}
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

	// --- 1. request a two-way quote -------------------------------------------------
	rfqclient.Step("Step %d of %d: request a two-way quote for %s %s%s", n, steps, quantity, symbol, rfqclient.CurrencyNote(currency))
	quote, err := client.RequestQuote(symbol, quantity, quoteSide, currency, q.QuoteRequestID)
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

	// --- 3. execute the chosen side -------------------------------------------------
	n++
	cl := *clOrdID
	if cl == "" {
		cl = rfqclient.NewClOrdID()
	}
	note := ""
	if !dryRun {
		// Re-check the side against the quote the server actually returned.
		if executeSide, err = rfqclient.ResolveExecutionSide(rfqclient.Str(quote, "side"), executeSide); err != nil {
			return err
		}
		if note = rfqclient.ExpiryNote(rfqclient.Str(quote, "expires_at")); note != "" {
			note = " (" + note + ")"
		}
	}
	rfqclient.Step("Step %d of %d: execute quote %s: %s at the %s, cl_ord_id %s%s",
		n, steps, quoteID, executeSide, rfqclient.PriceLeg(executeSide), cl, note)
	if dryRun {
		_, err = client.Call("POST", fmt.Sprintf(rfqclient.ExecuteQuotePath, quoteID),
			rfqclient.ExecuteRequest{ClOrdID: cl, Side: executeSide})
		return err
	}
	execution, err := client.ExecuteQuote(quoteID, executeSide, cl)
	if err != nil {
		return err
	}
	rfqclient.Say("%s", rfqclient.FillSummary(execution))
	return nil
}
