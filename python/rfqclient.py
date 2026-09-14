"""Shared config/auth/HTTP layer for the Selini Python RFQ sample clients.

Standard library only (http.client, hmac, hashlib, json). Implements the Selini
RFQ REST API v1 request signing plus the small amount of plumbing shared by
get_quote.py, view_quote.py, execute_quote.py, one_way_flow.py and two_way_flow.py.
Requests go over one keep-alive connection, so a request/view/execute sequence pays
the TCP+TLS handshake once -- quotes live only a few seconds, so that latency matters.

Output is one ordered stream on stdout: for every call, a plain line saying what
is happening, then the request (method, URL, pretty-printed JSON body) and the
response (status, latency, pretty-printed body).

Request signing (three headers on every request):
    SELINI-KEY   API key id
    SELINI-TS    request timestamp, ISO-8601 UTC (e.g. 2026-01-02T03:04:05.678Z)
    SELINI-SIGN  urlsafe_base64( HMAC-SHA256( api_secret, payload ) )

    payload = METHOD \\n TIMESTAMP \\n HOST \\n PATH [\\n QUERY] [\\n BODY]

  * METHOD is upper-case; HOST is the HTTP Host header value (host[:port]); PATH
    is the URL path only; QUERY is the raw query string without the leading '?'
    and is present only when the URL has one; BODY is the exact UTF-8 request
    body and is present only when non-empty. Lines are joined with '\\n' and there
    is no trailing newline.
  * The signature is standard base64 with '+' -> '-' and '/' -> '_' ('=' padding
    kept), which is exactly Python's base64.urlsafe_b64encode.
  * The body is serialized once; the same bytes are signed and sent.
"""

import argparse
import base64
import hashlib
import hmac
import http.client
import json
import re
import select
import sys
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation
from urllib.parse import urlsplit
from urllib.request import getproxies, proxy_bypass

QUOTE_REQUEST_PATH = "/v1/quote_request"
QUOTE_LOOKUP_PATH = "/v1/quotes/{quote_id}"
EXECUTE_QUOTE_PATH = "/v1/execute_quote/{quote_id}"

QUOTE_SIDES = ("BUY", "SELL", "TWO_WAY")
EXECUTION_SIDES = ("BUY", "SELL")

DEFAULT_TIMEOUT = 30.0  # seconds; overridable with --timeout

API_KEY_HEADER = "SELINI-KEY"
TIMESTAMP_HEADER = "SELINI-TS"
SIGNATURE_HEADER = "SELINI-SIGN"


# --------------------------------------------------------------------------- config

class ConfigError(Exception):
    """Raised for a missing/invalid config key or CLI value; scripts map it to exit code 2."""


@dataclass
class Config:
    base_url: str          # scheme://host[:port], no path
    host: str              # derived: the Host header value used in the signature
    api_key: str = ""
    api_secret: str = ""
    timeout: float = DEFAULT_TIMEOUT  # from --timeout, never from the config file


def _parse_root(path):
    try:
        with open(path) as f:
            root = json.load(f)
    except OSError:
        raise ConfigError("cannot open config file: " + path)
    except json.JSONDecodeError as e:
        raise ConfigError("invalid config JSON in " + path + ": " + str(e))
    if not isinstance(root, dict):
        raise ConfigError("config must be a JSON object")
    return root


def _as_string(v, key):
    if not isinstance(v, str):
        raise ConfigError('config key "' + key + '" must be a string')
    return v


def _req_string(d, key):
    if key not in d or d[key] is None:
        raise ConfigError('config key "' + key + '" is required')
    return _as_string(d[key], key)


def _opt_string(d, key):
    """Optional string key; returns "" when absent."""
    if key not in d or d[key] is None:
        return ""
    return _as_string(d[key], key)


def _opt_object(d, key):
    """Optional object key; returns {} when absent."""
    if key not in d or d[key] is None:
        return {}
    if not isinstance(d[key], dict):
        raise ConfigError('config key "' + key + '" must be an object')
    return d[key]


CONFIG_KEYS = ("base_url", "secret")


def load(path):
    """Load config.json. It holds the endpoint and credentials and nothing else:

        {"base_url": "https://...", "secret": {"api_key": "...", "api_secret": "..."}}

    What to trade (symbol, quantity, sides) and the HTTP timeout are command-line
    flags. Credentials are validated lazily by Client (they are not
    needed for --dry-run)."""
    root = _parse_root(path)
    for key in root:
        if key not in CONFIG_KEYS:
            raise ConfigError('config key "%s" is not supported; the config holds only base_url and secret' % key)

    base_url = _req_string(root, "base_url").strip().rstrip("/")
    parts = urlsplit(base_url)
    if (parts.scheme not in ("http", "https") or not parts.netloc
            or parts.path or parts.query or parts.fragment):
        raise ConfigError('config key "base_url" must be scheme://host[:port] with no path, '
                          'e.g. "https://api.example.com"')

    secret = _opt_object(root, "secret")
    return Config(
        base_url=base_url,
        host=parts.netloc,
        api_key=_opt_string(secret, "api_key").strip(),
        api_secret=_opt_string(secret, "api_secret").strip(),
    )


# ----------------------------------------------------------------------- validation

_AMOUNT_RE = re.compile(r"(\d+(\.\d*)?|\.\d+)")


def validate_amount(value, name):
    """A positive decimal string with no sign, exponent, or thousands separators
    (the API's Amount format). Returns the value unchanged."""
    s = str(value).strip()
    if not _AMOUNT_RE.fullmatch(s):
        raise ConfigError(name + " must be a plain decimal such as 0.25 (got %r)" % s)
    try:
        if Decimal(s) <= 0:
            raise ConfigError(name + " must be greater than zero")
    except InvalidOperation:
        raise ConfigError(name + " is not a valid decimal (got %r)" % s)
    return s


def normalize_quote_side(value):
    """BUY | SELL | TWO_WAY (case-insensitive; 'twoway' and '2way' accepted). Empty -> TWO_WAY."""
    s = str(value or "").strip().upper().replace("-", "_")
    if not s:
        return "TWO_WAY"
    if s in ("TWOWAY", "2WAY", "TWO_WAY"):
        return "TWO_WAY"
    if s in ("BUY", "SELL"):
        return s
    raise ConfigError("side must be one of BUY, SELL, TWO_WAY (got %r)" % value)


def normalize_execution_side(value):
    s = str(value or "").strip().upper()
    if s in EXECUTION_SIDES:
        return s
    raise ConfigError("execution side must be BUY or SELL (got %r)" % value)


def validate_quote_id(value):
    s = str(value or "").strip()
    if not s.isdigit() or int(s) <= 0:
        raise ConfigError("quote id must be a positive integer string (got %r)" % value)
    return s


def validate_quote_params(symbol, quantity, side="", currency=""):
    """Validate and normalize the quote-request flags (--symbol, --quantity, --side,
    --currency). Returns (symbol, quantity, side, currency); side defaults to TWO_WAY
    and currency may be "" (the API then assumes the base leg)."""
    symbol = str(symbol or "").strip()
    if not symbol:
        raise ConfigError("--symbol is required")
    quantity = validate_amount(quantity, "--quantity")
    side = normalize_quote_side(side)
    currency = str(currency or "").strip().upper()
    return symbol, quantity, side, currency


def resolve_execution_side(quote_side, requested=""):
    """Pick the execution side for a quote. A one-sided quote can only be executed on
    its own side (an explicit `requested` side that disagrees is an error). A TWO_WAY
    quote executes on `requested` (the flow scripts' --side), else BUY."""
    quote_side = normalize_quote_side(quote_side)
    if quote_side in EXECUTION_SIDES:
        if requested and normalize_execution_side(requested) != quote_side:
            raise ConfigError("quote is %s-only; it cannot be executed as %s"
                              % (quote_side, normalize_execution_side(requested)))
        return quote_side
    return normalize_execution_side(requested or "BUY")


def new_quote_request_id():
    return "rfq-" + uuid.uuid4().hex


def new_cl_ord_id():
    return "exec-" + uuid.uuid4().hex


# ----------------------------------------------------------------------------- auth

def utc_timestamp():
    """ISO-8601 UTC with millisecond precision, e.g. 2026-01-02T03:04:05.678Z."""
    now = datetime.now(timezone.utc)
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + ("%03dZ" % (now.microsecond // 1000))


def signing_payload(method, timestamp, host, path, query="", body=b""):
    """The exact bytes the server signs: METHOD\\nTS\\nHOST\\nPATH[\\nQUERY][\\nBODY]."""
    parts = [method.upper(), timestamp, host, path]
    if query:
        parts.append(query)
    payload = "\n".join(parts).encode("utf-8")
    if body:
        payload += b"\n" + body
    return payload


def sign(api_secret, payload):
    """HMAC-SHA256 over the payload with the API secret, URL-safe base64 ('=' kept)."""
    mac = hmac.new(api_secret.encode("ascii"), payload, hashlib.sha256).digest()
    return base64.urlsafe_b64encode(mac).decode("ascii")


def compact_json(value):
    """Canonical request body: no whitespace, UTF-8 preserved (not escaped)."""
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False).encode("utf-8")


def mask_key(key):
    if len(key) <= 8:
        return key[:2] + "..."
    return key[:4] + "..." + key[-4:]


# --------------------------------------------------------------------------- output

def say(msg):
    """One plain line saying what is happening."""
    print(msg, flush=True)


def step(msg):
    """say() preceded by a blank line: opens a new stage of a script's output."""
    print("\n" + msg, flush=True)


def error(msg):
    print("Error: " + msg, flush=True)


def show_json(data):
    """Pretty-print a request or response body."""
    print(json.dumps(data, indent=2, ensure_ascii=False), flush=True)


def humanize(b):
    """Render a signing payload on one line: newlines shown as '\\n'."""
    return b.decode("utf-8", "replace").replace("\n", "\\n")


# --------------------------------------------------------------------------- decode

def parse_timestamp(s):
    """API timestamps are yyyy-MM-ddTHH:mm:ss.fffZ; returns an aware datetime or None."""
    if not isinstance(s, str) or not s.endswith("Z"):
        return None
    for fmt in ("%Y-%m-%dT%H:%M:%S.%f", "%Y-%m-%dT%H:%M:%S"):
        try:
            return datetime.strptime(s[:-1], fmt).replace(tzinfo=timezone.utc)
        except ValueError:
            continue
    return None


def seconds_until(s):
    ts = parse_timestamp(s)
    if ts is None:
        return None
    return (ts - datetime.now(timezone.utc)).total_seconds()


def expiry_note(expires_at):
    """'2.9 s left on the quote', 'quote expired 1.2 s ago', or '' if expires_at is unusable."""
    remaining = seconds_until(expires_at)
    if remaining is None:
        return ""
    if remaining >= 0:
        return "%.1f s left on the quote" % remaining
    return "quote expired %.1f s ago" % -remaining


def price_leg(execution_side):
    """The quote price an execution trades at: BUY hits the ask, SELL hits the bid."""
    return "ask" if execution_side == "BUY" else "bid"


def quote_summary(q):
    """One line for a quote response, e.g. 'Quote 123: bid 111000.1 / ask 111050.3, 2.9 s left on the quote'."""
    prices = []
    if q.get("bid_price"):
        prices.append("bid %s" % q["bid_price"])
    if q.get("ask_price"):
        prices.append("ask %s" % q["ask_price"])
    note = expiry_note(q.get("expires_at"))
    return "Quote %s: %s%s" % (q.get("quote_id") or "?", " / ".join(prices) or "no prices",
                               (", " + note) if note else "")


def fill_summary(e):
    """One line for an execution response, e.g. 'Filled: BUY 0.0001 BTC/USDT at 111050.3, notional 11.11'."""
    g = lambda k: e.get(k) or "?"
    return "%s: %s %s %s at %s, notional %s (order_id %s, exec_id %s)" % (
        g("ord_status"), g("side"), g("quantity"), g("symbol"), g("price"), g("notional"),
        g("order_id"), g("exec_id"))


def error_code(status, data):
    """Best-effort error code from any of the API's error body shapes:
      endpoint errors        {"error": "QUOTE_EXPIRED", "quote_id": ...}
      authentication errors  {"detail": {"error": {"message": "AUTHENTICATION_FAILED", ...}}}
      problem details        {"title": ..., "status": 401, "detail": "..."}"""
    if isinstance(data, dict):
        v = data.get("error")
        if isinstance(v, str) and v:
            return v
        detail = data.get("detail")
        if isinstance(detail, dict):
            err = detail.get("error")
            if isinstance(err, dict) and isinstance(err.get("message"), str) and err["message"]:
                return err["message"]
        if isinstance(detail, str) and detail:
            return detail
        if isinstance(data.get("title"), str) and data["title"]:
            return data["title"]
    return "HTTP %d" % status


# ------------------------------------------------------------------------------ http

class ApiError(Exception):
    """Non-2xx response. `code` is the decoded error code, `body` the parsed JSON (or None)."""

    def __init__(self, status, code, body=None, retry_after=None):
        super().__init__("HTTP %d %s" % (status, code))
        self.status = status
        self.code = code
        self.body = body
        self.retry_after = retry_after


class TransportError(Exception):
    """DNS/TCP/TLS/timeout failure: no HTTP response was received."""


def _env_proxy(scheme, host):
    """(proxy_host, proxy_port) from HTTP_PROXY/HTTPS_PROXY (honoring NO_PROXY) for this
    target, or None when no proxy applies."""
    url = getproxies().get(scheme)
    if not url or proxy_bypass(host):
        return None
    p = urlsplit(url if "://" in url else "http://" + url)
    if not p.hostname:
        return None
    return p.hostname, p.port or (443 if p.scheme == "https" else 80)


class Client:
    """Signs and sends RFQ REST requests over a single keep-alive http.client connection.
    With dry_run=True it logs what would be sent and returns None instead of talking
    to the network."""

    def __init__(self, cfg, dry_run=False, verbose=False, use_env_proxy=False):
        if not dry_run and (not cfg.api_key or not cfg.api_secret):
            raise ConfigError('config "secret" must provide "api_key" and "api_secret"')
        self.cfg = cfg
        self.dry_run = dry_run
        self.verbose = verbose
        parts = urlsplit(cfg.base_url)
        self._secure = parts.scheme == "https"
        self._host = parts.hostname
        self._port = parts.port or (443 if self._secure else 80)
        # Proxies from HTTP(S)_PROXY are ignored unless opted in, so a stray
        # environment variable cannot silently reroute signed requests.
        self._proxy = _env_proxy(parts.scheme, self._host) if use_env_proxy else None
        self._conn = None

    # --- endpoints ---------------------------------------------------------------

    def request_quote(self, symbol, quantity, side, currency="", quote_request_id=""):
        payload = {
            "quote_request_id": quote_request_id or new_quote_request_id(),
            "symbol": symbol,
            "quantity": quantity,  # decimal string; the API preserves precision
            "side": side,
        }
        if currency:
            payload["currency"] = currency
        return self.call("POST", QUOTE_REQUEST_PATH, payload)

    def get_quote(self, quote_id):
        return self.call("GET", QUOTE_LOOKUP_PATH.format(quote_id=validate_quote_id(quote_id)))

    def execute_quote(self, quote_id, side, cl_ord_id=""):
        payload = {"cl_ord_id": cl_ord_id or new_cl_ord_id(), "side": normalize_execution_side(side)}
        return self.call("POST", EXECUTE_QUOTE_PATH.format(quote_id=validate_quote_id(quote_id)), payload)

    # --- transport ---------------------------------------------------------------

    def close(self):
        if self._conn is not None:
            try:
                self._conn.close()
            except OSError:
                pass
            self._conn = None

    def _open(self):
        kwargs = {"timeout": self.cfg.timeout}
        cls = http.client.HTTPSConnection if self._secure else http.client.HTTPConnection
        if self._proxy:
            conn = cls(self._proxy[0], self._proxy[1], **kwargs)
            conn.set_tunnel(self._host, self._port)  # HTTP CONNECT through the proxy
        else:
            conn = cls(self._host, self._port, **kwargs)
        return conn

    def _connection(self):
        """Reuse the open connection unless the server has since closed it."""
        conn = self._conn
        if conn is not None and conn.sock is not None:
            readable, _, _ = select.select([conn.sock], [], [], 0)
            if readable:  # EOF (or unexpected bytes) from the peer: do not reuse
                self.close()
        if self._conn is None:
            self._conn = self._open()
        return self._conn

    def call(self, method, path, payload=None):
        """Sign and send one request, printing it and the response; returns the decoded
        JSON object, raises ApiError on a non-2xx status. Dry run: print and return None."""
        body = compact_json(payload) if payload is not None else b""
        url = self.cfg.base_url + path
        say("%s %s" % (method, url))
        if payload is not None:
            show_json(payload)  # the same JSON goes on the wire without the whitespace

        if self.dry_run:
            say("(dry run: not sent)")
            return None

        timestamp = utc_timestamp()
        payload_bytes = signing_payload(method, timestamp, self.cfg.host, path, "", body)
        headers = {
            "Host": self.cfg.host,  # sent verbatim so it always equals the signed HOST line
            "Accept": "application/json",
            API_KEY_HEADER: self.cfg.api_key,
            TIMESTAMP_HEADER: timestamp,
            SIGNATURE_HEADER: sign(self.cfg.api_secret, payload_bytes),
        }
        if body:
            headers["Content-Type"] = "application/json"
        if self.verbose:
            say("Signing payload (key %s): %s" % (mask_key(self.cfg.api_key), humanize(payload_bytes)))
            say("%s: %s" % (SIGNATURE_HEADER, headers[SIGNATURE_HEADER]))

        started = time.monotonic()
        try:
            conn = self._connection()
            conn.request(method, path, body=body or None, headers=headers)
            resp = conn.getresponse()
            status = resp.status
            text = resp.read().decode("utf-8", "replace")
            retry_after = resp.getheader("Retry-After")
            if resp.will_close:
                self.close()
        except (OSError, http.client.HTTPException) as e:  # DNS, TCP, TLS, timeout, bad HTTP
            self.close()
            raise TransportError("%s %s failed: %s" % (method, url, e))
        elapsed_ms = (time.monotonic() - started) * 1000.0

        say("HTTP %d in %.0f ms" % (status, elapsed_ms))
        text = text.strip()
        try:
            data = json.loads(text) if text else None
        except json.JSONDecodeError:
            data = None
        if data is not None:
            show_json(data)
        elif text:
            say(text)

        if not 200 <= status < 300:
            raise ApiError(status, error_code(status, data), data, retry_after)
        if not isinstance(data, dict):
            raise ApiError(status, "response body is not a JSON object", data)
        return data


# ------------------------------------------------------------------------------- cli

def add_common_args(parser):
    """--config plus the connection and diagnostic flags every script takes."""
    parser.add_argument("--config", required=True, metavar="PATH",
                        help="JSON config holding base_url and secret{api_key, api_secret}, nothing else")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the request(s) that would be sent and exit without sending")
    parser.add_argument("--verbose", action="store_true",
                        help="also print the HMAC signing payload and signature")
    parser.add_argument("--use-env-proxy", action="store_true",
                        help="honor HTTP_PROXY/HTTPS_PROXY (ignored by default)")
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT, metavar="SECONDS",
                        help="HTTP timeout per request (default: %d)" % DEFAULT_TIMEOUT)


def add_quote_request_args(parser):
    """--symbol, --quantity, --currency and --quote-request-id for POST /v1/quote_request.
    Each script adds its own --side, because what it means differs per flow."""
    parser.add_argument("--symbol", required=True,
                        help="instrument as BASE/QUOTE, e.g. BTC/USDT")
    parser.add_argument("--quantity", required=True,
                        help="positive decimal quantity, e.g. 0.25 (denominated in --currency)")
    parser.add_argument("--currency", default="",
                        help="leg of the symbol the quantity is denominated in: the base (default) "
                             "or the quote leg, e.g. USDT to size the request by notional")
    parser.add_argument("--quote-request-id", default="", metavar="ID",
                        help="your unique id for the quote request; auto-generated if omitted")


def make_client(args):
    """Load --config, apply --timeout, and build a Client; maps ConfigError to exit 2."""
    try:
        cfg = load(args.config)
        if not args.timeout > 0:  # phrased so nan is rejected too
            raise ConfigError("--timeout must be a positive number of seconds")
        cfg.timeout = float(args.timeout)
        client = Client(cfg, dry_run=args.dry_run, verbose=args.verbose,
                        use_env_proxy=args.use_env_proxy)
    except ConfigError as e:
        error(str(e))
        sys.exit(2)
    if args.dry_run:
        say("Dry run against %s: requests are printed, nothing is sent" % cfg.base_url)
    else:
        say("Using %s with key %s" % (cfg.base_url, mask_key(cfg.api_key)))
    return cfg, client


def report_api_error(e):
    """The response body was already printed by Client.call; add the decoded code."""
    s = "HTTP %d %s" % (e.status, e.code)
    if e.retry_after:
        s += " (retry after %s s)" % e.retry_after
    error(s)


def run(main):
    """Run a script's main(); exit 0 ok, 1 API/transport failure, 2 usage/config, 130 Ctrl-C."""
    try:
        code = main()
    except ApiError as e:
        report_api_error(e)
        code = 1
    except TransportError as e:
        error(str(e))
        code = 1
    except ConfigError as e:
        error(str(e))
        code = 2
    except KeyboardInterrupt:
        error("interrupted")
        code = 130
    sys.exit(code or 0)


def parser(description, epilog=None):
    return argparse.ArgumentParser(description=description, epilog=epilog,
                                   formatter_class=argparse.RawDescriptionHelpFormatter)
