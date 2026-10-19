"""Read the completed local map/item catalog without loading huge placements."""

import argparse
import json
from pathlib import Path
import sys

sys.stdout.reconfigure(encoding="utf-8")

ROOT = Path(__file__).resolve().parent
CATALOG = ROOT / "static-data" / "catalogs"
parser = argparse.ArgumentParser(description=__doc__)
sub = parser.add_subparsers(dest="mode", required=True)
world = sub.add_parser("cluster", help="show exact static cluster definition")
world.add_argument("id")
items = sub.add_parser("item", help="literal case-insensitive item ID/name search")
items.add_argument("query")
items.add_argument("--limit", type=int, default=20)
sub.add_parser("markets", help="show static city to market cluster mapping")
args = parser.parse_args()

if args.mode == "markets":
    value = json.loads((CATALOG / "market-locations.json").read_text(encoding="utf-8"))
    print(json.dumps(value["city_name_mapping"], ensure_ascii=False, indent=2))
else:
    filename = "map-clusters.jsonl" if args.mode == "cluster" else "market-items.jsonl"
    matched = 0
    with (CATALOG / filename).open(encoding="utf-8") as stream:
        for line in stream:
            row = json.loads(line)
            if args.mode == "cluster":
                identity = row.get("cluster_id", row.get("id", row.get("attributes", {}).get("id")))
                if identity != args.id:
                    continue
            elif args.query.casefold() not in json.dumps(row, ensure_ascii=False).casefold():
                continue
            print(json.dumps(row, ensure_ascii=False, indent=2))
            matched += 1
            if args.mode == "cluster" or matched >= args.limit:
                break
    if not matched:
        raise SystemExit("No matching catalog record")
