#!/usr/bin/env python3
"""one_way_flow - request a one-sided quote (BUY or SELL), view it, then execute it:

    1. POST /v1/quote_request            request a BUY or SELL quote (one price)
    2. GET  /v1/quotes/{quote_id}        view it back
    3. POST /v1/execute_quote/{quote_id} execute it on that same side   (a REAL trade)

--side BUY|SELL is the side you trade. It is sent with the quote request, so the
quote is priced on that side only and can only be executed on that side. For a
quote priced on both sides, see two_way_flow.py.

Quotes are short-lived (a few seconds), so the calls run back to back over one
keep-alive connection. --skip-execute stops after step 2 (nothing is traded),
--skip-view goes straight from request to execution when the expiry is tight,
and --dry-run prints the requests without sending anything.
"""

import rfqclient


def main():
    p = rfqclient.parser(
        "Request a BUY or SELL quote, view it, then execute it. Step 3 sends a real order.",
        epilog="examples:\n"
               "  python one_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL\n"
               "  python one_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY --skip-execute\n"
               "  python one_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY --dry-run\n")
    rfqclient.add_common_args(p)
    rfqclient.add_quote_request_args(p)
    p.add_argument("--side", required=True,
                   help="side you trade, requested and executed: BUY (at the ask) | SELL (at the bid)")
    p.add_argument("--clordid", default="", metavar="ID",
                   help="your unique client order id for the execution; auto-generated if omitted")
    p.add_argument("--skip-view", action="store_true",
                   help="skip the GET /v1/quotes lookup between request and execution")
    p.add_argument("--skip-execute", action="store_true",
                   help="stop after viewing the quote; do not execute")
    args = p.parse_args()

    side = rfqclient.normalize_execution_side(args.side)  # BUY or SELL, never TWO_WAY
    symbol, quantity, side, currency = rfqclient.validate_quote_params(
        args.symbol, args.quantity, side, args.currency)
    _, client = rfqclient.make_client(args)

    steps = 1 + (not args.skip_view) + (not args.skip_execute)
    n = 1

    # --- 1. request a one-sided quote -----------------------------------------------
    rfqclient.step("Step %d of %d: request a %s quote for %s %s%s" % (
        n, steps, side, quantity, symbol, (" (quantity in %s)" % currency) if currency else ""))
    quote = client.request_quote(symbol, quantity, side, currency, args.quote_request_id)
    if args.dry_run:  # no quote_id to continue with; show the remaining requests anyway
        quote_id = "<quote_id>"
    else:
        rfqclient.say(rfqclient.quote_summary(quote))
        quote_id = rfqclient.validate_quote_id(quote.get("quote_id"))

    # --- 2. view it -----------------------------------------------------------------
    if not args.skip_view:
        n += 1
        rfqclient.step("Step %d of %d: view quote %s" % (n, steps, quote_id))
        if args.dry_run:
            client.call("GET", rfqclient.QUOTE_LOOKUP_PATH.format(quote_id=quote_id))
        else:
            quote = client.get_quote(quote_id)
            rfqclient.say(rfqclient.quote_summary(quote))

    if args.skip_execute:
        rfqclient.step("Stopping before execution (--skip-execute); nothing traded")
        return 0

    # --- 3. execute it on the same side ---------------------------------------------
    n += 1
    cl_ord_id = args.clordid or rfqclient.new_cl_ord_id()
    note = ""
    if not args.dry_run:
        # The server normalizes the side; make sure the quote really is on ours.
        side = rfqclient.resolve_execution_side(quote.get("side"), side)
        note = rfqclient.expiry_note(quote.get("expires_at"))
    rfqclient.step("Step %d of %d: execute quote %s: %s at the %s, cl_ord_id %s%s" % (
        n, steps, quote_id, side, rfqclient.price_leg(side), cl_ord_id, (" (%s)" % note) if note else ""))
    if args.dry_run:
        client.call("POST", rfqclient.EXECUTE_QUOTE_PATH.format(quote_id=quote_id),
                    {"cl_ord_id": cl_ord_id, "side": side})
        return 0

    execution = client.execute_quote(quote_id, side, cl_ord_id)
    rfqclient.say(rfqclient.fill_summary(execution))
    return 0


if __name__ == "__main__":
    rfqclient.run(main)
