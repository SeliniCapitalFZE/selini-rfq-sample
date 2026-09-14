# Selini RFQ sample clients

Minimal reference clients for the **Selini Capital FZE RFQ REST API v1**: request
a firm quote, look it up, and execute it. HTTPS + JSON with HMAC-SHA256 signed
requests.

| Script | What it does |
| ------ | ------------ |
| `get_quote`     | Request a quote for a symbol and quantity, one-sided (BUY or SELL) or two-way. Nothing is traded. |
| `view_quote`    | Look up a quote by id. Read-only. |
| `execute_quote` | Trade on a live quote. **Places a real order.** |
| `one_way_flow`  | One-way quote and execute: request a BUY or SELL quote, view it, then execute it. |
| `two_way_flow`  | Two-way quote and execute: request a two-way quote, view it, then execute the side you choose. |

Quotes are firm for only a few seconds, so the flow scripts make their calls back
to back over one connection. Both stop after the view with `--skip-execute`.

Implementations: [Python](python/) and [Go](go/), both standard library only, with
the same commands, flags and output. Setup and commands are in
[python/usage.md](python/usage.md) and [go/usage.md](go/usage.md).

## Configuration

The config file holds only where to connect and who you are:

```json
{
  "base_url": "https://YOUR_RFQ_ENDPOINT",
  "secret": { "api_key": "...", "api_secret": "..." }
}
```

Copy `config.example.json` to `config.json` and fill it in with the endpoint and
credentials Selini gave you at onboarding. `config.json` is git-ignored. What to
trade (symbol, quantity, side) is always given on the command line.

## Quick start

```sh
cd python
python get_quote.py    --config ../config.json --symbol BTC/USDT --quantity 0.01
python two_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --skip-execute
python two_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side BUY   # real trade
```

The Go commands take the same flags, e.g.
`cd go && go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 0.01`.

Add `--dry-run` to any command to print the requests without sending them. Each
script prints what it is doing, then the request and the response; a failure ends
with `Error: HTTP <status> <CODE>`.

## Authentication

Every request is signed with HMAC-SHA256 using your API key and secret. The
samples contain a complete working example of the signing: see `sign()` and
`signing_payload()` in [python/rfqclient.py](python/rfqclient.py) or
[go/rfqclient/auth.go](go/rfqclient/auth.go), and run any script with `--verbose`
to print the exact payload and signature it produced.

## API in brief

- Amounts are decimal strings, ids are integer strings, timestamps are UTC with milliseconds.
- `POST /v1/quote_request` takes `symbol` (`BASE/QUOTE`), `quantity`, and optionally `side` (`BUY`, `SELL`, default `TWO_WAY`) and `currency` (which leg the quantity is in). It returns a `quote_id`, a bid and/or ask, and `expires_at`.
- `GET /v1/quotes/{quote_id}` returns that quote again.
- `POST /v1/execute_quote/{quote_id}` takes `side` (`BUY` trades at the ask, `SELL` at the bid) and returns the fill. A `200` is a complete fill; anything else means nothing traded.
- Errors come back as `{"error": "<CODE>"}`, for example `QUOTE_EXPIRED` or `QUOTE_NOT_FOUND`; request a new quote and try again.
