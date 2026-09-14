# Python sample

Python 3.9 or newer, standard library only. There is nothing to install;
`requirements.txt` is empty so scripted installs still succeed.

## Setup

```sh
cd python
cp ../config.example.json ../config.json
$EDITOR ../config.json        # base_url, api_key, api_secret
```

## Commands

```sh
# request a quote (nothing traded): two-way by default, or --side BUY|SELL
python get_quote.py --config ../config.json --symbol BTC/USDT --quantity 0.01
python get_quote.py --config ../config.json --symbol BTC/USDT --quantity 10000 --currency USDT   # sized in USDT

# look up a quote
python view_quote.py --config ../config.json --quote-id 123456789

# execute a quote -- REAL TRADE (BUY trades at the ask, SELL at the bid)
python execute_quote.py --config ../config.json --quote-id 123456789 --side BUY

# two-way flow: request bid and ask, view, then execute the side you choose -- REAL TRADE
python two_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL
python two_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --skip-execute   # no trade

# one-way flow: request a BUY or SELL quote, view, then execute it -- REAL TRADE
python one_way_flow.py --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL
```

Quotes expire a few seconds after they are made. The flow scripts reuse one
connection; add `--skip-view` if the extra round trip costs you the quote.

## Flags

Every script: `--config PATH` (required), `--dry-run` (print the requests, send
nothing), `--verbose` (also print the signing payload and signature),
`--timeout SECONDS`, `--use-env-proxy`.

- `get_quote.py`: `--symbol` and `--quantity` (required); `--side BUY|SELL|TWO_WAY`, `--currency`, `--quote-request-id`.
- `view_quote.py`: `--quote-id`.
- `execute_quote.py`: `--quote-id` and `--side BUY|SELL` (required); `--clordid`.
- `two_way_flow.py` and `one_way_flow.py`: `--symbol`, `--quantity` and `--side BUY|SELL`, the side you trade (the two-way flow needs it only when executing); `--currency`, `--quote-request-id`, `--clordid`, `--skip-view`, `--skip-execute`.

## Output

```
Step 1 of 2: request a two-way quote for 0.0001 BTC/USDT
POST https://YOUR_RFQ_ENDPOINT/v1/quote_request
{
  "quote_request_id": "rfq-…",
  "symbol": "BTC/USDT",
  "quantity": "0.0001",
  "side": "TWO_WAY"
}
HTTP 200 in 84 ms
{
  "quote_id": "…",
  "bid_price": "77240.05",
  "ask_price": "77263.24",
  "expires_at": "…",
  …
}
Quote …: bid 77240.05 / ask 77263.24, 2.9 s left on the quote
```

A failure prints the response body, then `Error: HTTP <status> <CODE>`. Exit codes:
0 success, 1 API or network error, 2 bad arguments or config.
