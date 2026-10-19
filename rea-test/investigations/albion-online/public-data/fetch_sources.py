"""Save bounded public research evidence; does not launch/capture the game."""
from __future__ import annotations
import concurrent.futures
import datetime as dt
import gzip
import hashlib
import io
import json
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent
SERVER = "5f8fb43a60a39e4a04d4fb313ad01a161e032ef5"
CLIENT = "c6edd9456bec4657755c25f173e8ebc7a31a17f7"
DUMPS = "1a469e53de028c5f85cdd80505f10cbd0a2515cf"
DUMPER = "e73a181eafd0db785f889aba8dbe7c69f83b71ad"

def fetch(entry):
    relative, url, scope = entry
    record = {"id": "PUB-" + relative.replace("/", "-"), "path": relative,
              "url": url, "scope": scope,
              "retrieved_utc": dt.datetime.now(dt.timezone.utc).isoformat()}
    req = urllib.request.Request(url, headers={"User-Agent": "AlbionPublicResearch/1.0", "Accept-Encoding": "gzip"})
    try:
        with urllib.request.urlopen(req, timeout=35) as response:
            data = response.read(3_000_001)
            if len(data) > 3_000_000:
                raise ValueError("research evidence exceeded 3 MB bound")
            if response.headers.get("Content-Encoding", "").lower() == "gzip":
                with gzip.GzipFile(fileobj=io.BytesIO(data)) as decoded:
                    data = decoded.read(3_000_001)
            if len(data) > 3_000_000:
                raise ValueError("decoded evidence exceeded 3 MB bound")
            record.update(http_status=response.status, final_url=response.url,
                          content_type=response.headers.get("Content-Type"),
                          etag=response.headers.get("ETag"),
                          last_modified=response.headers.get("Last-Modified"),
                          bytes=len(data), sha256=hashlib.sha256(data).hexdigest(),
                          hash_scope="HTTP entity after Content-Encoding decompression")
            path = ROOT / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    except urllib.error.HTTPError as exc:
        record.update(http_status=exc.code, error=str(exc))
    except Exception as exc:
        record.update(error=repr(exc))
    return record

def main():
    sources = []
    for page in ["api", "developer", "client-faq"]:
        sources.append((f"sources/aodp/{page}.html", f"https://www.albion-online-data.com/{page}", "AODP published documentation; mutable"))
    for path in ["config/initializers/rack_attack.rb", "config/routes.rb", "app/services/market_data_service.rb", "app/services/market_history_service.rb", "app/services/market_orderbook_service.rb", "app/services/market_order_dedupe_service.rb", "app/services/market_order_processor_service.rb", "lib/location.rb", "lib/quality.rb", "app/models/market_order.rb"]:
        sources.append(("sources/aodp-server/" + path, f"https://raw.githubusercontent.com/ao-data/albiondata-server-rails/{SERVER}/{path}", "frozen AODP server source; does not prove deployment"))
    for path in ["lib/market.go", "lib/marketHistory.go", "lib/map.go", "lib/nats.go", "client/operations.go", "client/events.go", "client/operationtype_string.go", "client/eventtype_string.go", "client/operation_auction_get_offers.go", "client/operation_auction_get_requests.go", "client/operation_auction_get_item_average_stats.go", "client/operation_get_cluster_map_info.go", "client/listener.go", "client/config.go", "client/offline.go"]:
        sources.append(("sources/aodp-client/" + path, f"https://raw.githubusercontent.com/ao-data/albiondata-client/{CLIENT}/{path}", "frozen AODP client source; protocol version specific"))
    for suffix, filename in [("database-east/", "east-directory.html"), ("database/%21README.txt", "README.txt")]:
        sources.append(("sources/database/" + filename, "https://www.albion-online-data.com/" + suffix, "public database directory/restore notes; no database backup downloaded"))
    for path in ["formatted/world.json", "worldsettings_asia.xml", "minimapgendata.xml"]:
        sources.append(("sources/ao-bin-dumps/" + path, f"https://raw.githubusercontent.com/ao-data/ao-bin-dumps/{DUMPS}/{path}", "frozen community static data; local installed build takes precedence"))
    for path in ["Extractor/Extractors/BinaryDecrypter.cs", "Extractor/Extractors/BinaryDumper.cs", "CommandLine/CommandLine.csproj", "README.md"]:
        sources.append(("sources/bin-dumper/" + path, f"https://raw.githubusercontent.com/ao-data/albiondata-bin-dumper/{DUMPER}/{path}", "frozen static .bin exporter source"))
    sources.append(("sources/gameinfo/official-support-statement.html", "https://forum.albiononline.com/index.php/Thread/131017-Update-on-API-New-Endpoints/", "SBI forum statement on unsupported internal GameInfo API"))
    for path in ["cluster/world.xml", "worldsettings_asia.xml", "cluster/world_asia.xml", "cluster/world_asia_patch.xml", "worldsettings_asia_patch.xml"]:
        encoded = urllib.parse.urlencode({"path": path, "per_page": 1})
        filename = path.replace("/", "_") + "-last-commit.json"
        sources.append(("sources/ao-bin-dumps/" + filename, "https://api.github.com/repos/ao-data/ao-bin-dumps/commits?" + encoded, "path-specific last revision; repository HEAD is not every file date"))
    for repo in ["albion-network", "AlbionPacketHandler"]:
        sources.append((f"sources/player-protocol/{repo}-commit.json", f"https://api.github.com/repos/ao-data/{repo}/commits?per_page=1", "historical protocol example revision"))
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        records = list(pool.map(fetch, sources))
    for repo, paths in {"albion-network": ["Albion.Network.Example/NewCharacterEvent.cs", "Albion.Network.Example/MoveEvent.cs"], "AlbionPacketHandler": ["AlbionProcessor/Handlers/CharacterHandler.cs"]}.items():
        commit_path = ROOT / f"sources/player-protocol/{repo}-commit.json"
        if commit_path.exists():
            sha = json.loads(commit_path.read_text(encoding="utf-8"))[0]["sha"]
            for path in paths:
                records.append(fetch((f"sources/player-protocol/{repo}/" + path, f"https://raw.githubusercontent.com/ao-data/{repo}/{sha}/{path}", "historical player protocol example; not validation of 2026 wire fields")))
    api_samples = [
        ("prices.json", "https://east.albion-online-data.com/api/v2/stats/prices/T4_BAG.json?locations=Caerleon,Bridgewatch&qualities=1"),
        ("history.json", "https://east.albion-online-data.com/api/v2/stats/history/T4_BAG.json?date=2026-10-01&end_date=2026-10-12&locations=Caerleon&qualities=1&time-scale=24"),
        ("orderbook-deployment-check.json", "https://east.albion-online-data.com/api/v2/stats/orderbook/T4_BAG.json?locations=Caerleon&qualities=1"),
        ("events.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/events?limit=1&offset=0"),
        ("search.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/search?q=Albion"),
        ("player.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/players/wpDh5rpcTXyKkmDpnm7PNg"),
        ("player-kills.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/players/wpDh5rpcTXyKkmDpnm7PNg/kills?limit=1&offset=0"),
        ("player-deaths.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/players/wpDh5rpcTXyKkmDpnm7PNg/deaths?limit=1&offset=0"),
        ("guild.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/guilds/aXGLUa6yS6q3t_4vL0Fg8A"),
        ("alliance.json", "https://gameinfo-sgp.albiononline.com/api/gameinfo/alliances/bMmQv71uRom7gQeeg9JBZw"),
    ]
    for filename, url in api_samples:
        records.append(fetch(("samples/" + filename, url, "one bounded public Asia/East API response; no account credentials")))
        time.sleep(2)
    (ROOT / "sources-manifest.json").write_text(json.dumps(records, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"count": len(records), "success": sum(r.get("http_status") == 200 for r in records), "failures": [r for r in records if r.get("http_status") != 200]}, ensure_ascii=False))

if __name__ == "__main__":
    main()
