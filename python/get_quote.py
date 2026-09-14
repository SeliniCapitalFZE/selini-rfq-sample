#!/usr/bin/env python3
"""get_quote - request an RFQ quote (POST /v1/quote_request) and print it.

--symbol and --quantity are required; --side picks which side(s) to price (BUY,
SELL, or the default TWO_WAY) and --currency which leg the quantity is in. The
response's quote_id is what view_quote.py and execute_quote.py take via
--quote-id. Requesting a quote does not trade: the quote is firm until its
expires_at and only trades via execute_quote.py or one of the *_flow.py scripts.
"""

import rfqclient


def main():
    p = rfqclient.parser(
        "Request an RFQ quote (POST /v1/quote_request).",
        epilog="examples:\n"
               "  python get_quote.py --config ../config.json --symbol BTC/USDT --quantity 0.01\n"
               "  python get_quote.py --config ../config.json --symbol BTC/USDT --quantity 0.5 --side BUY\n"
               "  python get_quote.py --config ../config.json --symbol BTC/USDT --quantity 10000 --currency USDT\n")
    rfqclient.add_common_args(p)
    rfqclient.add_quote_request_args(p)
    p.add_argument("--side", default="TWO_WAY",
                   help="which side(s) to price: BUY | SELL | TWO_WAY (default: TWO_WAY)")
    args = p.parse_args()

    symbol, quantity, side, currency = rfqclient.validate_quote_params(
        args.symbol, args.quantity, args.side, args.currency)
    _, client = rfqclient.make_client(args)

    rfqclient.step("Requesting a %s quote for %s %s%s" % (
        side, quantity, symbol, (" (quantity in %s)" % currency) if currency else ""))
    quote = client.request_quote(symbol, quantity, side, currency, args.quote_request_id)
    if quote is None:  # dry run
        return 0

    quote_id = quote.get("quote_id", "")
    rfqclient.say(rfqclient.quote_summary(quote))
    rfqclient.say("Next: view_quote.py --quote-id %s, or execute_quote.py --quote-id %s --side BUY|SELL"
                  % (quote_id, quote_id))
    return 0


if __name__ == "__main__":
    rfqclient.run(main)
