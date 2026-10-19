"""Decode the explicitly selected Albion GameData into a separate output tree.

Algorithm is ported from ao-data/albiondata-bin-dumper BinaryDecrypter.cs.
The original game installation is opened for reading only. This tool deliberately
retains every *.bin, including profanity and unusual/non-XML payloads.
"""
from __future__ import annotations

import argparse
import collections
import datetime
import gzip
import hashlib
import json
from pathlib import Path
import sys
import xml.etree.ElementTree as ET

sys.path.insert(0, str(Path(__file__).resolve().parent / "dependencies"))
from Crypto.Cipher import DES
from Crypto.Util.Padding import unpad

KEY = bytes([48, 239, 114, 71, 66, 242, 4, 50])
IV = bytes([14, 166, 220, 137, 219, 237, 220, 79])


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--input", required=True, type=Path)
    ap.add_argument("--output", required=True, type=Path)
    ap.add_argument("--overwrite", action="store_true", help="replace prior extracted outputs only")
    args = ap.parse_args()
    src, out = args.input.resolve(strict=True), args.output.resolve()
    if out == src or src in out.parents or out in src.parents:
        ap.error("input and output trees must be disjoint")
    if out.exists() and any(out.iterdir()) and not args.overwrite:
        ap.error("output already has files; select a new directory or --overwrite")
    out.mkdir(parents=True, exist_ok=True)
    manifest = []
    counts = collections.Counter()
    start = datetime.datetime.now(datetime.timezone.utc).isoformat()
    for index, path in enumerate(sorted(src.rglob("*.bin")), 1):
        relative = path.relative_to(src)
        record = {"input": str(path), "relative": relative.as_posix()}
        stage = "read"
        try:
            encoded = path.read_bytes()
            record.update(input_bytes=len(encoded), input_sha256=digest(encoded))
            stage = "DES-CBC/PKCS7"
            compressed = unpad(DES.new(KEY, DES.MODE_CBC, IV).decrypt(encoded), DES.block_size)
            stage = "gzip"
            decoded = gzip.decompress(compressed)
            record.update(output_bytes=len(decoded), output_sha256=digest(decoded))
            output = out / relative.with_suffix(".xml")
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(decoded)
            record["output"] = str(output)
            stage = "XML"
            try:
                root = ET.fromstring(decoded)
                record.update(status="decoded_xml", root_tag=root.tag,
                              elements=sum(1 for _ in root.iter()))
            except ET.ParseError as exc:
                record.update(status="decoded_not_xml", error=str(exc))
        except Exception as exc:
            record.update(status="failed", stage=stage,
                          error=f"{type(exc).__name__}: {exc}")
        counts[record["status"]] += 1
        manifest.append(record)
        if index % 500 == 0:
            print(json.dumps({"files": index, "counts": counts}), flush=True)
    meta = {"source": str(src), "output": str(out), "utc_started": start,
            "utc_finished": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "algorithm": "DES-CBC/PKCS7 then GZip",
            "dumper_repository": "https://github.com/ao-data/albiondata-bin-dumper",
            "dumper_commit": "e73a181eafd0db785f889aba8dbe7c69f83b71ad",
            "tool": "extract_game_data.py", "total_files": len(manifest), "counts": counts,
            "files": manifest}
    (out.parent / "extraction-manifest.json").write_text(
        json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({k: v for k, v in meta.items() if k != "files"}, ensure_ascii=False, indent=2))
    return 1 if counts["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
