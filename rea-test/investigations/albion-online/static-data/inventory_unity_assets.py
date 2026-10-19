"""Offline inventory of Unity SerializedFile v22 metadata and selected names.

This bounded reader does not load the game or all texture pixels. Format layout
is based on Perfare/AssetStudio SerializedFile.cs. Unsupported versions and
invalid ranges produce diagnostics. Custom mesh slices are inventoried from TOC.
"""
import argparse
import collections
import hashlib
import json
from pathlib import Path
import struct


CLASS_NAMES = {1: "GameObject", 21: "Material", 28: "Texture2D", 43: "Mesh",
               48: "Shader", 49: "TextAsset", 83: "AudioClip", 114: "MonoBehaviour",
               115: "MonoScript", 129: "PlayerSettings", 142: "AssetBundle",
               147: "ResourceManager", 156: "TerrainData", 213: "Sprite"}
NAMED = {21, 28, 43, 48, 49, 83, 115, 142, 156, 213}


def line(obj):
    return json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n"


class Reader:
    def __init__(self, f, endian="<"):
        self.f, self.endian = f, endian

    def read(self, n):
        b = self.f.read(n)
        if len(b) != n:
            raise EOFError(f"short read for {n} bytes at {self.f.tell()}")
        return b

    def val(self, fmt):
        return struct.unpack(self.endian + fmt, self.read(struct.calcsize(fmt)))[0]

    def skip(self, n):
        if n < 0:
            raise ValueError("negative skip")
        self.f.seek(n, 1)

    def align(self):
        self.f.seek((self.f.tell() + 3) & ~3)

    def z(self):
        out = bytearray()
        while (b := self.read(1)) != b"\0":
            out.extend(b)
            if len(out) > 16384:
                raise ValueError("unterminated metadata string")
        return out.decode("utf-8", errors="replace")

    def string(self):
        n = self.val("i")
        if n < 0 or n > 1000000:
            raise ValueError(f"invalid string size {n}")
        value = self.read(n).decode("utf-8", errors="strict")
        self.align()
        return value


def type_metadata(r, tree_enabled):
    cls = r.val("i")
    r.val("B")
    script_index = r.val("h")
    if cls == 114:
        r.skip(16)
    r.skip(16)
    if tree_enabled:
        nodes, strings = r.val("i"), r.val("i")
        if nodes < 0 or strings < 0 or nodes > 1000000 or strings > 100000000:
            raise ValueError("invalid TypeTree sizes")
        r.skip(nodes * 32 + strings)
        deps = r.val("i")
        if deps < 0 or deps > 1000000:
            raise ValueError("invalid TypeTree dependencies")
        r.skip(deps * 4)
    return {"class_id": cls, "script_type_index": script_index}


def inventory(path, dest, resources):
    with path.open("rb") as f:
        header = Reader(f, ">")
        old_meta, old_size, version, old_offset = [header.val("I") for _ in range(4)]
        endian = header.val("B")
        header.skip(3)
        if version != 22:
            raise ValueError(f"only serialized version 22 supported, found {version}")
        meta, size, offset, unknown = header.val("I"), header.val("Q"), header.val("Q"), header.val("Q")
        if size != path.stat().st_size or offset > size:
            raise ValueError("header file size/data offset mismatch")
        r = Reader(f, ">" if endian else "<")
        unity, platform, enabled = r.z(), r.val("i"), r.val("B")
        typecount = r.val("i")
        if typecount < 0 or typecount > 100000:
            raise ValueError("invalid type count")
        types = [type_metadata(r, enabled) for _ in range(typecount)]
        count = r.val("i")
        if count < 0 or count > 10000000:
            raise ValueError("invalid object count")
        objects = []
        for _ in range(count):
            r.align()
            pathid, start, n, tid = r.val("q"), r.val("q"), r.val("I"), r.val("i")
            if tid < 0 or tid >= len(types) or start < 0 or offset + start + n > size:
                raise ValueError("invalid object table range/type")
            objects.append((pathid, offset + start, n, tid))
        table_end = f.tell()
        scriptcount = r.val("i")
        if not 0 <= scriptcount <= 10000000:
            raise ValueError("invalid script count")
        for _ in range(scriptcount):
            r.val("i")
            r.align()
            r.val("q")
        externalcount = r.val("i")
        if not 0 <= externalcount <= 1000000:
            raise ValueError("invalid external count")
        externals = []
        for ordinal in range(externalcount):
            r.z()
            guid = r.read(16).hex()
            kind, pathname = r.val("i"), r.z()
            externals.append({"file_id": ordinal + 1, "guid": guid, "type": kind, "path": pathname})
        classes = collections.Counter()
        names_found = 0
        for pathid, start, n, tid in objects:
            cls = types[tid]["class_id"]
            classes[str(cls)] += 1
            record = {"source": str(path), "path_id": pathid, "byte_start": start,
                      "byte_size": n, "type_id": tid, "class_id": cls,
                      "class_name": CLASS_NAMES.get(cls, "other")}
            if cls in NAMED:
                try:
                    f.seek(start)
                    name = r.string()
                    if f.tell() > start + n:
                        raise ValueError("name extends past object")
                    record["name"] = name
                    names_found += 1
                    if cls == 28:
                        # Local Unity 6000.3 payload observed: one 4-byte field after
                        # aligned m_Name, followed by width/height/image-size/etc.
                        # Legacy AssetStudio Texture base adds bool fields and is
                        # four bytes longer, producing demonstrably invalid sizes.
                        r.skip(4)
                        tex = {
                            "width": r.val("i"), "height": r.val("i"),
                            "complete_image_bytes": r.val("i"), "mips_stripped": r.val("i"),
                            "texture_format_id": r.val("i"), "mip_count": r.val("i")}
                        tex["layout_status"] = "local observed fixed prefix; no TypeTree semantic decode"
                        if not 0 < tex["width"] <= 65536 or not 0 < tex["height"] <= 65536:
                            raise ValueError("implausible texture dimensions")
                        record["candidate_texture_header"] = tex
                        f.seek(start)
                        payload = r.read(n)
                        for pos in range(12, len(payload) - 4):
                            length = struct.unpack_from(r.endian + "i", payload, pos)[0]
                            if not 0 < length < 4096 or pos + 4 + length > len(payload):
                                continue
                            rawpath = payload[pos + 4:pos + 4 + length]
                            if rawpath == (path.name + ".resS").encode("utf-8") and all(x == 0 for x in payload[pos + 4 + length:]):
                                stream_offset, stream_size = struct.unpack_from(r.endian + "QI", payload, pos - 12)
                                sourcepath = rawpath.decode("utf-8")
                                streamfile = path.parent / Path(sourcepath).name
                                valid = streamfile.is_file() and stream_offset + stream_size <= streamfile.stat().st_size
                                record["candidate_texture_stream"] = {"path": str(streamfile), "offset": stream_offset,
                                    "size": stream_size, "range_valid": valid, "payload_field_offset": pos - 12,
                                    "layout_status": "observed final StreamingInfo-like tuple and file-range validation"}
                                break
                except Exception as exc:
                    record["name_diagnostic"] = str(exc)
            if cls == 147:
                try:
                    f.seek(start)
                    containercount = r.val("i")
                    if not 0 <= containercount <= 10000000:
                        raise ValueError("invalid ResourceManager count")
                    for _ in range(containercount):
                        name, fileid, targetid = r.string(), r.val("i"), r.val("q")
                        resources.write(line({"source": str(path), "resource_path": name,
                                              "file_id": fileid, "target_path_id": targetid}))
                    if f.tell() > start + n:
                        raise ValueError("ResourceManager extends past object")
                    record["resource_count"] = containercount
                except Exception as exc:
                    record["resource_diagnostic"] = str(exc)
            dest.write(line(record))
        f.seek(0)
        metadata_hash = hashlib.sha256(f.read(offset)).hexdigest()
        result = {"path": str(path), "bytes": size, "version": version, "unity_version": unity,
                "metadata_bytes": meta, "data_offset": offset, "platform": platform,
                "type_tree_enabled": bool(enabled), "type_count": typecount,
                "object_count": count, "class_counts": dict(classes), "names_read": names_found,
                "object_table_end": table_end, "metadata_prefix_sha256": metadata_hash}
        result["externals"] = externals
        return result


def mesh_catalog(data, out):
    folder = data / "StreamingAssets/CustomMeshes"
    toc = folder / "meshes.toc"
    records, diagnostics = [], []
    for ordinal, text in enumerate(toc.read_text(encoding="utf-8").splitlines()):
        try:
            name, package, start, size = text.rsplit(":", 3)
            start, size = int(start), int(size)
            file = (folder / package).resolve()
            if file.parent != folder.resolve() or not file.is_file() or start < 0 or size < 0 or start + size > file.stat().st_size:
                raise ValueError("slice outside package")
            records.append({"name": name, "package": str(file), "offset": start,
                            "size": size, "toc_ordinal": ordinal})
        except Exception as exc:
            diagnostics.append({"ordinal": ordinal, "error": str(exc)})
    with (out / "custom-mesh-index.jsonl").open("w", encoding="utf-8") as dest:
        for record in records:
            dest.write(line(record))
    return {"toc_path": str(toc), "toc_sha256": hashlib.sha256(toc.read_bytes()).hexdigest(),
            "mesh_slices": len(records), "diagnostics": diagnostics,
            "packages": [{"name": p.name, "bytes": p.stat().st_size,
                          "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
                         for p in sorted(folder.glob("*.pmsh"))],
            "limit": "TOC offsets/ranges only; custom mesh binary layout and geometry not decoded"}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--input", required=True, type=Path)
    ap.add_argument("--output", required=True, type=Path)
    args = ap.parse_args()
    data, out = args.input.resolve(), args.output.resolve()
    if out == data or data in out.parents:
        ap.error("output must be outside target data tree")
    out.mkdir(parents=True, exist_ok=True)
    summary, failures = [], []
    with (out / "unity-objects.jsonl").open("w", encoding="utf-8") as dest, (out / "unity-resource-paths.jsonl").open("w", encoding="utf-8") as resources:
        selected = [p for p in data.iterdir() if p.is_file() and
                    (p.suffix == ".assets" or p.name.startswith("level") or p.name == "globalgamemanagers")]
        for index, p in enumerate(sorted(selected)):
            try:
                summary.append(inventory(p, dest, resources))
            except Exception as exc:
                failures.append({"path": str(p), "diagnostic": str(exc)})
            if index % 50 == 0:
                print(json.dumps({"assets_processed": index + 1, "failures": len(failures)}), flush=True)
    meshes = mesh_catalog(data, out)
    report = {"selected_files": len(selected), "inventoried_files": len(summary),
              "failures": failures, "files": summary, "custom_meshes": meshes,
              "scope": "v22 object metadata, built-in NamedObject names and ResourceManager container only; pixels and native mesh layout not decoded"}
    (out / "unity-inventory.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"selected": len(selected), "inventoried": len(summary), "failures": failures,
                      "mesh_slices": meshes["mesh_slices"]}, ensure_ascii=False))
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
