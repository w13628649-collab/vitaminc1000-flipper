"""Resolve map-related ResourceManager paths against local SerializedFile inventory."""
import collections
import hashlib
import json
from pathlib import Path

base = Path(__file__).resolve().parent / "unity-assets"
inventory = json.loads((base / "unity-inventory.json").read_text(encoding="utf-8"))
externals = {entry["path"]: {e["file_id"]: str(Path(entry["path"]).parent / Path(e["path"]).name)
                           for e in entry["externals"]} for entry in inventory["files"]}
selected = []
for text in (base / "unity-resource-paths.jsonl").open(encoding="utf-8"):
    resource = json.loads(text)
    if any(k in resource["resource_path"].lower() for k in ("clustermap/", "minimapgen/", "worldmap/")):
        target = resource["source"] if resource["file_id"] == 0 else externals.get(resource["source"], {}).get(resource["file_id"])
        selected.append({**resource, "target_file": target})
wanted = {(r["target_file"], r["target_path_id"]) for r in selected}
objects = {}
classcounts = collections.Counter()
errors = collections.Counter()
for text in (base / "unity-objects.jsonl").open(encoding="utf-8"):
    obj = json.loads(text)
    classcounts[str(obj["class_id"])] += 1
    for field in ("name_diagnostic", "resource_diagnostic"):
        errors[field] += field in obj
    key = (obj["source"], obj["path_id"])
    if key in wanted:
        objects[key] = obj
for r in selected:
    r["object"] = objects.get((r["target_file"], r["target_path_id"]))
(base / "map-assets.json").write_text(json.dumps(selected, ensure_ascii=False, indent=2), encoding="utf-8")
summary = {"selected_serialized_files": inventory["selected_files"],
           "inventoried_files": inventory["inventoried_files"],
           "file_failures": inventory["failures"], "objects": sum(classcounts.values()),
           "class_counts": classcounts, "object_parse_diagnostics": errors,
           "map_resource_paths": len(selected), "resolved_map_resource_paths": sum(r["object"] is not None for r in selected),
           "map_texture_paths": sum(r["object"] is not None and r["object"]["class_id"] == 28 for r in selected),
           "custom_mesh_slices": inventory["custom_meshes"]["mesh_slices"]}
(base / "SUMMARY.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
files = [{"path": p.name, "bytes": p.stat().st_size,
          "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in sorted(base.iterdir())
         if p.is_file() and p.name != "FILES.json"]
(base / "FILES.json").write_text(json.dumps(files, indent=2), encoding="utf-8")
print(json.dumps(summary, indent=2))
