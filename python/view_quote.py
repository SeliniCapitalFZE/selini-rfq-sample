#!/usr/bin/env python3
"""view_quote - look up an existing quote by id (GET /v1/quotes/{quote_id}) and print it.

Read-only. The lookup returns the same fields as the original quote response
(minus quote_request_id). An unknown id, or a quote belonging to another
account, is reported as HTTP 400 QUOTE_NOT_FOUND.
"""

import rfqclient


def main():
    p = rfqclient.parser(
        "Look up a quote by id (GET /v1/quotes/{quote_id}).",
        epilog="example:\n"
               "  python view_quote.py --config ../config.json --quote-id 123456789\n")
    rfqclient.add_common_args(p)
    p.add_argument("--quote-id", required=True, metavar="ID",
                   help="quote_id returned by get_quote.py")
    args = p.parse_args()

    quote_id = rfqclient.validate_quote_id(args.quote_id)
    _, client = rfqclient.make_client(args)

    rfqclient.step("Viewing quote %s" % quote_id)
    quote = client.get_quote(quote_id)
    if quote is not None:  # None on a dry run
        rfqclient.say(rfqclient.quote_summary(quote))
    return 0


if __name__ == "__main__":
    rfqclient.run(main)
