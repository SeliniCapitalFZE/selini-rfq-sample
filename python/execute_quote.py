#!/usr/bin/env python3
"""execute_quote - execute (trade on) an existing quote: POST /v1/execute_quote/{quote_id}.

THIS PLACES A REAL TRADE against the configured endpoint. --quote-id and --side are
both required. The quote must still be live (before its expires_at) and the side
must be compatible with the quote: BUY executes at the quote's ask_price, SELL at
its bid_price; a one-sided quote can only be executed on its own side. Pass
--dry-run to print the request without sending it.
"""

import rfqclient


def main():
    p = rfqclient.parser(
        "Execute a quote (POST /v1/execute_quote/{quote_id}). Sends a real order.",
        epilog="examples:\n"
               "  python execute_quote.py --config ../config.json --quote-id 123456789 --side BUY\n"
               "  python execute_quote.py --config ../config.json --quote-id 123456789 --side SELL --dry-run\n")
    rfqclient.add_common_args(p)
    p.add_argument("--quote-id", required=True, metavar="ID",
                   help="quote_id returned by get_quote.py")
    p.add_argument("--side", required=True,
                   help="BUY (trade at the quote's ask_price) | SELL (trade at its bid_price)")
    p.add_argument("--clordid", default="", metavar="ID",
                   help="your unique client order id (cl_ord_id); auto-generated if omitted")
    args = p.parse_args()

    quote_id = rfqclient.validate_quote_id(args.quote_id)
    side = rfqclient.normalize_execution_side(args.side)
    cl_ord_id = args.clordid or rfqclient.new_cl_ord_id()
    _, client = rfqclient.make_client(args)

    rfqclient.step("Executing quote %s: %s at the %s, cl_ord_id %s"
                   % (quote_id, side, rfqclient.price_leg(side), cl_ord_id))
    execution = client.execute_quote(quote_id, side, cl_ord_id)
    if execution is not None:  # None on a dry run
        rfqclient.say(rfqclient.fill_summary(execution))
    return 0


if __name__ == "__main__":
    rfqclient.run(main)
