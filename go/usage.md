# Go sample

Go 1.21 or newer, standard library only. There are no dependencies to download.

## Setup

```sh
cd go
cp ../config.example.json ../config.json
$EDITOR ../config.json        # base_url, api_key, api_secret
go build -o bin/ ./...        # optional: binaries in bin/; otherwise use `go run`
```

## Commands

Same flags and output as the Python sample. Both `--flag` and `-flag` work.

```sh
# request a quote (nothing traded): two-way by default, or --side BUY|SELL
go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 0.01
go run ./cmd/get_quote --config ../config.json --symbol BTC/USDT --quantity 10000 --currency USDT   # sized in USDT

# look up a quote
go run ./cmd/view_quote --config ../config.json --quote-id 123456789

# execute a quote -- REAL TRADE (BUY trades at the ask, SELL at the bid)
go run ./cmd/execute_quote --config ../config.json --quote-id 123456789 --side BUY

# two-way flow: request bid and ask, view, then execute the side you choose -- REAL TRADE
go run ./cmd/two_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL
go run ./cmd/two_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --skip-execute   # no trade

# one-way flow: request a BUY or SELL quote, view, then execute it -- REAL TRADE
go run ./cmd/one_way_flow --config ../config.json --symbol BTC/USDT --quantity 0.01 --side SELL
```

Quotes expire a few seconds after they are made. The flow commands reuse one
connection; add `--skip-view` if the extra round trip costs you the quote.

## Flags

Every command: `--config PATH` (required), `--dry-run` (print the requests, send
nothing), `--verbose` (also print the signing payload and signature),
`--timeout SECONDS`, `--use-env-proxy`. Per-command flags match the Python sample;
run any command with `--help`.

## Layout

| Path | Role |
| ---- | ---- |
| `rfqclient/` | Shared package: config loading, HMAC signing, HTTP, output helpers. |
| `cmd/get_quote`, `cmd/view_quote`, `cmd/execute_quote` | One command per endpoint. |
| `cmd/two_way_flow`, `cmd/one_way_flow` | Request → view → execute in one run. |

## Output

Identical to the Python sample: a line saying what is happening, the request, the
response, and a one-line summary. A failure prints the response body, then
`Error: HTTP <status> <CODE>`. Exit codes: 0 success, 1 API or network error,
2 bad arguments or config.
