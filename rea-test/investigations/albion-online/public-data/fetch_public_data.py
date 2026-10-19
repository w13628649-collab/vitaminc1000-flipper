"""Fetch one explicit public Albion API request, preserving JSON and provenance.

Standard library only. No credentials, game launch, capture, or automatic crawl.
"""
from __future__ import annotations
import argparse
import datetime as dt
import email.utils
import gzip
import hashlib
import http.client
import io
import json
from pathlib import Path
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zlib

MARKET_HOSTS = {"east": "https://east.albion-online-data.com", "west": "https://west.albion-online-data.com", "europe": "https://europe.albion-online-data.com"}
PLAYER_HOSTS = {"east": "https://gameinfo-sgp.albiononline.com", "west": "https://gameinfo.albiononline.com", "europe": "https://gameinfo-ams.albiononline.com"}

def split_values(value):
    result = [part.strip() for part in value.split(",") if part.strip()]
    if not result:
        raise ValueError("comma-separated input must contain at least one value")
    return result

def make_url(args):
    if args.mode in ("prices", "history"):
        if not args.items or not args.locations:
            raise ValueError("prices/history require --items and --locations")
        items = split_values(args.items)
        locations = split_values(args.locations)
        qualities = split_values(args.qualities)
        if any(q not in {"1", "2", "3", "4", "5"} for q in qualities):
            raise ValueError("qualities must be integers 1..5")
        quoted_items = ",".join(urllib.parse.quote(item, safe="") for item in items)
        params = {"locations": ",".join(locations), "qualities": ",".join(qualities)}
        if args.mode == "history":
            if not args.date or not args.end_date:
                raise ValueError("history requires --date and --end-date (YYYY-MM-DD)")
            begin, end = dt.date.fromisoformat(args.date), dt.date.fromisoformat(args.end_date)
            if begin > end:
                raise ValueError("--date must be on or before --end-date")
            params.update(date=str(begin), end_date=str(end), **{"time-scale": args.time_scale})
        url = f"{MARKET_HOSTS[args.server]}/api/v2/stats/{args.mode}/{quoted_items}.json?{urllib.parse.urlencode(params)}"
    elif args.mode == "player":
        if not args.player_id:
            raise ValueError("player requires --player-id from public search")
        url = f"{PLAYER_HOSTS[args.server]}/api/gameinfo/players/{urllib.parse.quote(args.player_id, safe='')}"
    else:
        if not args.query:
            raise ValueError("search requires --query")
        url = f"{PLAYER_HOSTS[args.server]}/api/gameinfo/search?{urllib.parse.urlencode({'q': args.query})}"
    if len(url.encode("ascii")) > 4096:
        raise ValueError("encoded URL exceeds AODP's 4096-character limit; reduce explicit inputs")
    return url

def backoff(headers, attempt):
    minimum = max(2, 2 ** (attempt + 1))
    retry = headers.get("Retry-After")
    if retry:
        try:
            return max(minimum, float(retry))
        except ValueError:
            try:
                deadline = email.utils.parsedate_to_datetime(retry)
                return max(minimum, (deadline - dt.datetime.now(dt.timezone.utc)).total_seconds())
            except (ValueError, TypeError):
                pass
    reset = headers.get("RateLimit-Reset")
    if reset:
        try:
            # Some deployments use a delta, others a UNIX reset timestamp.
            value = float(reset)
            seconds = value - time.time() if value > 1_000_000_000 else value
            return max(minimum, seconds + 1)
        except ValueError:
            pass
    return minimum

def wait_backoff(seconds):
    # Keep individual waits short so a caller can poll/interrupt a running command.
    deadline = time.monotonic() + seconds
    while (remaining := deadline - time.monotonic()) > 0:
        time.sleep(min(30, remaining))

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["prices", "history", "player", "search"])
    parser.add_argument("--server", choices=MARKET_HOSTS, default="east")
    parser.add_argument("--items", help="comma-separated UniqueName values, e.g. T4_BAG,T4_BAG@1")
    parser.add_argument("--locations", help="explicit comma-separated market IDs or AODP names")
    parser.add_argument("--qualities", default="1", help="comma-separated values 1..5; default 1")
    parser.add_argument("--date")
    parser.add_argument("--end-date")
    parser.add_argument("--time-scale", choices=["1", "6", "24"], default="24")
    parser.add_argument("--player-id")
    parser.add_argument("--query")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--overwrite", action="store_true")
    args = parser.parse_args()
    try:
        url = make_url(args)
    except ValueError as exc:
        parser.error(str(exc))
    output = args.output.expanduser().resolve()
    metadata_path = output.with_name(output.name + ".metadata.json")
    if not args.overwrite and (output.exists() or metadata_path.exists()):
        parser.error("output/metadata already exist; select a new output or explicitly use --overwrite")
    metadata = {"url": url, "server": args.server, "mode": args.mode,
                "requested_output": str(output),
                "request_started_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
                "scope": "one explicit public API request; community observations or public GameInfo profile",
                "missing_data_semantics": "prices 0 and date 0001-01-01 are missing observations, never interpreted as free",
                "attempts": []}
    req = urllib.request.Request(url, headers={"Accept": "application/json", "Accept-Encoding": "gzip", "User-Agent": "AlbionPublicResearch/1.0"})
    data = None
    for attempt in range(4):
        # Each process makes <=1 attempt per 2 seconds. Do not run parallel crawls.
        time.sleep(2)
        try:
            with urllib.request.urlopen(req, timeout=35) as response:
                metadata.update(http_status=response.status, final_url=response.url,
                                response_received_utc=dt.datetime.now(dt.timezone.utc).isoformat(),
                                response_headers=dict(response.headers.items()))
                metadata["attempts"].append({"status": response.status})
                data = response.read(8_000_001)
                if len(data) > 8_000_000:
                    raise ValueError("response exceeds bounded 8 MB HTTP payload")
                if response.headers.get("Content-Encoding", "").lower() == "gzip":
                    with gzip.GzipFile(fileobj=io.BytesIO(data)) as decoded:
                        data = decoded.read(20_000_001)
                if len(data) > 20_000_000:
                    raise ValueError("decoded response exceeds bounded 20 MB")
                json.loads(data.decode("utf-8-sig"))
                metadata["retrieved_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
                break
        except urllib.error.HTTPError as exc:
            metadata["attempts"].append({"status": exc.code, "headers": dict(exc.headers.items())})
            metadata.update(http_status=exc.code, error=str(exc))
            if exc.code == 429 and attempt < 3:
                delay = backoff(exc.headers, attempt)
                if delay > 300:
                    metadata["error"] = "HTTP 429 requires more than the 300-second automatic retry budget; retry later"
                    metadata["retry_after_seconds"] = delay
                    data = None
                    break
                print(f"HTTP 429; retry after {delay:.0f} seconds", file=sys.stderr)
                wait_backoff(delay)
                continue
            data = None
            break
        except (ValueError, UnicodeError, OSError, EOFError, http.client.HTTPException, zlib.error) as exc:
            metadata["error"] = str(exc)
            data = None
            break
    output.parent.mkdir(parents=True, exist_ok=True)
    if data is not None:
        metadata.pop("error", None)
        metadata.update(result_status="success", output_written=True,
                        bytes=len(data), sha256=hashlib.sha256(data).hexdigest(),
                        hash_scope="JSON HTTP entity after Content-Encoding decompression; payload unmodified")
        output.write_bytes(data)
    else:
        # A failed overwrite must not pair an old payload with new provenance.
        # Keep the prior output and receipt, and record this failure separately.
        metadata_path = output.with_name(output.name + f".failure-{uuid.uuid4().hex}.metadata.json")
        metadata.update(result_status="failed", output_written=False)
    metadata_path.write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    if data is None:
        print(f"Request failed; provenance saved to {metadata_path}", file=sys.stderr)
        return 1
    print(f"Saved {output} and {metadata_path}; missing prices remain missing.")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
