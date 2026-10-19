"""Build bounded static catalogs from the local decrypted XML.

Raw decoded XML remains authoritative. Catalogs preserve attributes as strings,
layer choices and dynamic flags instead of claiming observed live object state.
"""
from __future__ import annotations
import argparse
import collections
import hashlib
import json
import math
from pathlib import Path
import xml.etree.ElementTree as ET


def dump(path, obj):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding="utf-8")


def line(obj):
    return json.dumps(obj, ensure_ascii=False, separators=(",", ":")) + "\n"


def tree(e):
    result = {"tag": e.tag, "attributes": dict(e.attrib)}
    if list(e):
        result["children"] = [tree(c) for c in e]
    if e.text and e.text.strip():
        result["text"] = e.text.strip()
    return result


def occurrences(e, context=()):
    for c in e:
        if c.tag in ("layer", "layergroup"):
            yield from occurrences(c, context + ((c.tag, dict(c.attrib)),))
        elif c.tag in ("tile", "compoundtile"):
            yield {"tag": c.tag, "attributes": dict(c.attrib),
                   "layer_context": list(context),
                   "children": [tree(x) for x in c]}
        else:
            yield from occurrences(c, context)


def floats(text, n=None):
    result = [float(x) for x in text.split()]
    if n is not None and len(result) != n:
        raise ValueError(f"expected {n} coordinates: {text}")
    return result


def localizations(src, out):
    translated = {}
    count = 0
    with (out / "localization-en-zh.jsonl").open("w", encoding="utf-8") as dest:
        for _, e in ET.iterparse(src / "localization.xml", events=("end",)):
            if e.tag == "tu":
                names = {}
                for tuv in e.findall("tuv"):
                    lang = tuv.get("{http://www.w3.org/XML/1998/namespace}lang")
                    if lang in ("EN-US", "ZH-CN", "ZH-TW"):
                        names[lang] = "".join(tuv.itertext()).strip()
                key = e.get("tuid")
                dest.write(line({"variable": key, "texts": names}))
                translated[key] = names
                count += 1
                e.clear()
    return translated, count


def item_catalog(src, out, translations):
    root = ET.parse(src / "items.xml").getroot()
    records, journals = [], []
    direct = 0

    def add(e, name, level, locname=None, desc=None, variant="base"):
        records.append({"dumper_index": len(records) + 1,
                        "item_id": name, "base_id": e.get("uniquename"),
                        "kind": e.tag, "enchantment_level": level,
                        "variant": variant,
                        "attributes": dict(e.attrib),
                        "name_variable": locname,
                        "localized_names": translations.get(locname, {}),
                        "description_variable": desc})

    for e in root:
        name = e.get("uniquename")
        if not name:
            continue
        direct += 1
        level = e.get("enchantmentlevel", "0")
        display = e.get("descvariable0", "@ITEMS_" + name)
        if "ARTEFACT" in name:
            display = "@ITEMS_" + name
        desc = e.get("descriptionlocatag", "@ITEMS_" + name + "_DESC")
        add(e, name + ("@" + level if level != "0" else ""), level, display, desc)
        if e.tag == "journalitem":
            journals.append(e)
        for enc in e.findall("enchantments/enchantment"):
            level = enc.get("enchantmentlevel")
            add(e, name + "@" + level, level,
                e.get("descvariable0", "@ITEMS_" + name), desc, "enchantment")
    for e in journals:
        for suffix in ("_EMPTY", "_FULL"):
            name = e.get("uniquename") + suffix
            add(e, name, "0", "@ITEMS_" + name, "@ITEMS_" + name + "_DESC", "journal")
    with (out / "market-items.jsonl").open("w", encoding="utf-8") as dest:
        for rec in records:
            dest.write(line(rec))
    dump(out / "market-item-categories.json", tree(root.find("shopcategories")))
    return {"item_definitions": direct, "expanded_item_ids": len(records),
            "unique_item_ids": len({r["item_id"] for r in records}),
            "items_without_selected_localization": sum(not r["localized_names"] for r in records),
            "dumper_index_semantics": "ao-data ItemExtractor order; network numeric mapping unverified"}


def catalog_definitions(src, out):
    names = ("harvestables", "mobs", "resourceprefabmappings", "resourcedistpresets",
             "randomspawnbehaviors", "dynamictemplates", "dynamicclusters",
             "templatemetadata", "minimapgendata", "worldsettings",
             "worldsettings_asia", "worldsettings_europe", "buildings",
             "treasuredistribution", "tunnelsystem", "mists", "outlandsteleportationportals")
    counts = {}
    for name in names:
        root = ET.parse(src / (name + ".xml")).getroot()
        with (out / (name + ".jsonl")).open("w", encoding="utf-8") as dest:
            for i, child in enumerate(root):
                dest.write(line({"source": name + ".xml", "ordinal": i, **tree(child)}))
        counts[name] = {"root": root.tag, "direct_children": len(root),
                        "element_counts": dict(collections.Counter(e.tag for e in root.iter()))}
    return counts


def template_catalog(src, out):
    stats = []
    names = collections.defaultdict(list)
    by_name = collections.defaultdict(list)
    tile_count = 0
    with (out / "template-placements.jsonl").open("w", encoding="utf-8") as dest:
        for p in sorted((src / "templates").rglob("*.xml")):
            root = ET.parse(p).getroot()
            name = p.name.removesuffix(".template.xml")
            key = p.relative_to(src).as_posix()
            occ = list(occurrences(root.find("tiles"))) if root.find("tiles") is not None else []
            stat = {"source": key, "name": name, "variant": p.parent.name,
                    "attributes": dict(root.attrib), "placements": len(occ),
                    "layer_ids": [dict(e.attrib) for e in root.iter("layer")],
                    "criticalscenemembers": [tree(e) for e in root.findall("criticalscenemembers")]}
            stats.append(stat)
            names[name].append(key)
            # Template payload stays per variant; name-only resolution is often ambiguous.
            by_name[name].append((stat, occ))
            for entry in occ:
                dest.write(line({"source": key, **entry}))
            tile_count += len(occ)
    dump(out / "template-index.json", {"templates": stats,
         "duplicate_names": {k: v for k, v in names.items() if len(v) > 1}})
    return by_name, {"template_files": len(stats), "template_placements": tile_count,
                     "ambiguous_template_names": sum(len(v) > 1 for v in names.values())}


def tile_catalog(src, out):
    root = ET.parse(src / "cluster/tiles.xml").getroot()
    semantics = {}
    childcounts = collections.Counter()
    with (out / "tile-definitions.jsonl").open("w", encoding="utf-8") as dest:
        for e in root.findall("tile"):
            dest.write(line(tree(e)))
            if list(e):
                semantics[e.get("name")] = [tree(c) for c in e]
                childcounts.update(c.tag for c in e)
    with (out / "compound-tile-placements.jsonl").open("w", encoding="utf-8") as dest:
        for e in root.findall("compoundtile"):
            dest.write(line({"attributes": dict(e.attrib), "placements": list(occurrences(e))}))
    dump(out / "tile-semantics.json", {"tiles": semantics, "child_counts": childcounts})
    with (out / "tile-geometry-tables.jsonl").open("w", encoding="utf-8") as dest:
        for table in root:
            if table.tag in ("tile", "compoundtile"):
                continue
            for ordinal, child in enumerate(table):
                dest.write(line({"table": table.tag, "ordinal": ordinal, **tree(child)}))
    return semantics, {"attributes": dict(root.attrib),
                       "tile_definitions": len(root.findall("tile")),
                       "compound_tile_definitions": len(root.findall("compoundtile")),
                       "tiles_with_gameplay_children": len(semantics),
                       "child_counts": dict(childcounts)}


def map_catalog(src, out, template_names):
    world = ET.parse(src / "cluster/world.xml").getroot()
    settings = ET.parse(src / "worldsettings_asia.xml").getroot()
    types = {e.get("name"): dict(e.attrib) for e in settings.find("clustertypes")}
    archetypes = {e.get("name"): dict(e.attrib) for e in settings.find("archetypes")}
    records, market_records, edges, missingfiles, missingtemplates = [], [], [], [], []
    counts = collections.Counter()
    byid = {}
    for cluster in world.find("clusters"):
        attrs = dict(cluster.attrib)
        byid[attrs["id"]] = attrs
        typ = types.get(attrs.get("type"), {})
        record = {"attributes": attrs, "asia_clustertype_definition": typ,
                  "asia_archetype_definition": archetypes.get(typ.get("archetype"), {}),
                  "world_sections": [tree(e) for e in cluster]}
        for exit in cluster.findall("exits/exit"):
            target = exit.get("targetid", "")
            # A value such as exit-UUID@0217 is a local schema observation.
            if "@" in target:
                edges.append({"source_cluster": attrs["id"],
                              "candidate_target_cluster": target.rsplit("@", 1)[1],
                              "attributes": dict(exit.attrib)})
        path = src / "cluster" / attrs.get("file", "")
        instances = []
        if path.is_file():
            local = ET.parse(path).getroot()
            record["cluster_layout_attributes"] = dict(local.attrib)
            for instance in local.findall("templateinstance"):
                ref = instance.get("ref")
                candidates = [stat["source"] for stat, _ in template_names.get(ref, [])]
                instances.append({"attributes": dict(instance.attrib),
                                  "active_layers": [e.get("id") for e in instance.findall("activelayer")],
                                  "template_candidates": candidates,
                                  "resolution": "one_candidate" if len(candidates) == 1 else
                                                "ambiguous_variants" if candidates else "missing_template"})
                if not candidates:
                    missingtemplates.append({"cluster": attrs["id"], "ref": ref})
            counts["layout_files_referenced"] += 1
            counts["template_instances"] += len(instances)
            record["cluster_layout_other_sections"] = [tree(e) for e in local if e.tag != "templateinstance"]
        else:
            missingfiles.append({"cluster": attrs["id"], "file": attrs.get("file"),
                                 "enabled": attrs.get("enabled")})
        record["template_instances"] = instances
        records.append(record)
        counts["cluster_definitions"] += 1
        counts["enabled_definitions"] += attrs.get("enabled") == "true"
        counts["exits"] += len(cluster.findall("exits/exit"))
        counts["minimap_markers"] += len(cluster.findall("minimapmarkers/marker"))
        counts["worldmap_markers"] += len(cluster.findall("worldmapmarkers/marker"))
        counts["marketplace_definitions"] += len(cluster.findall("marketplaces/marketplace"))
        for market in cluster.findall("marketplaces/marketplace"):
            marker_candidates = [dict(e.attrib) for e in cluster.findall("minimapmarkers/marker")
                                 if "market" in e.get("type", "").lower()]
            market_records.append({"cluster_id": attrs["id"], "displayname": attrs.get("displayname"),
                                   "enabled": attrs.get("enabled"), "cluster_type": attrs.get("type"),
                                   "marketplace": dict(market.attrib), "local_market_marker_candidates": marker_candidates})
    for edge in edges:
        edge["target_exists_in_local_world"] = edge["candidate_target_cluster"] in byid
    city_names = ("Thetford", "Lymhurst", "Bridgewatch", "Martlock", "Fort Sterling", "Caerleon", "Brecilien")
    cities = []
    for name in city_names:
        cities.append({"name": name,
                       "city_cluster_ids": [a["id"] for a in byid.values() if a.get("displayname") == name],
                       "market_cluster_candidates": [r for r in market_records
                            if r["displayname"] == name + " Market" or (name == "Caerleon" and r["marketplace"].get("type") == "BLACKMARKET")]})
    with (out / "map-clusters.jsonl").open("w", encoding="utf-8") as dest:
        for rec in records:
            dest.write(line(rec))
    dump(out / "map-exits.json", edges)
    dump(out / "market-locations.json", {"city_name_mapping": cities, "all_static_marketplaces": market_records,
         "note": "Definition/template identities are not a proof of a currently running market; AODP API city parameter mapping is external."})
    diagnostics = {"missing_layout_files": missingfiles, "missing_template_refs": missingtemplates,
                   "exits_without_existing_target": [e for e in edges if not e["target_exists_in_local_world"]],
                   "unmatched_asia_clustertypes": sorted({r["attributes"].get("type") for r in records
                                          if not r["asia_clustertype_definition"]})}
    dump(out / "map-diagnostics.json", diagnostics)
    with (out / "world-other-sections.jsonl").open("w", encoding="utf-8") as dest:
        for section in world:
            if section.tag == "clusters":
                continue
            for ordinal, entry in enumerate(section):
                dest.write(line({"section": section.tag, "ordinal": ordinal, **tree(entry)}))
    dump(out / "map-manifest.json", {"source": "local exact build 1.32.030.345319",
         "region_selection": "Local world.xml + local worldsettings_asia.xml; effective live Asia selection unverified",
         "world_attributes": dict(world.attrib), "counts": counts,
         "missing_layout_files": len(missingfiles), "missing_template_refs": len(missingtemplates),
         "local_world_sections": [{"tag": e.tag, "entries": len(e)} for e in world if e.tag != "clusters"],
         "outputs": ["map-clusters.jsonl", "map-exits.json", "template-index.json", "template-placements.jsonl",
                     "tile-definitions.jsonl", "compound-tile-placements.jsonl", "tile-semantics.json", "market-locations.json"]})
    return counts, records, diagnostics


def sample_positions(out, records, templates, semantics):
    cluster = next(r for r in records if r["attributes"]["id"] == "0217")
    samples = []
    for inst in cluster["template_instances"]:
        ref = inst["attributes"]["ref"]
        dynamic = inst["attributes"].get("dyn") == "true"
        active = set(inst["active_layers"])
        translation = floats(inst["attributes"].get("pos", "0 0 0"), 3)
        radians = math.radians(float(inst["attributes"].get("rot", "0")))
        for stat, occ in templates.get(ref, []):
            for entry in occ:
                a = entry["attributes"]
                if a.get("name") not in semantics or "pos" not in a:
                    continue
                if any(ctx[0] == "layer" and ctx[1].get("id") not in active for ctx in entry["layer_context"]):
                    continue
                point = floats(a["pos"], 3)
                # Standard Unity positive Y rotation; runtime/application convention not verified.
                guess = [translation[0] + point[0] * math.cos(radians) + point[2] * math.sin(radians),
                         translation[1] + point[1],
                         translation[2] - point[0] * math.sin(radians) + point[2] * math.cos(radians)]
                samples.append({"cluster_id": "0217", "cluster_name": cluster["attributes"]["displayname"],
                                "instance": inst, "template_variant": stat["source"],
                                "tile_placement": entry, "tile_semantics": semantics[a["name"]],
                                "candidate_position_xyz": [round(x, 6) for x in guess],
                                "dynamic_instance": dynamic,
                                "position_status": "inferred static candidate; Y rotation convention, variant and live activation unverified"})
                if len(samples) >= 30:
                    break
            if len(samples) >= 30:
                break
        if len(samples) >= 30:
            break
    dump(out / "placement-example-0217.json", {"rotation_equation": "x'=cos(theta)*x+sin(theta)*z+tx; z'=-sin(theta)*x+cos(theta)*z+tz; y'=y+ty",
         "selection": "First 30 direct tile gameplay placements matching explicitly selected layers, or unlayered tiles. Compound recursion not flattened.",
         "candidates": samples})
    return len(samples)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--input", required=True, type=Path)
    ap.add_argument("--output", required=True, type=Path)
    args = ap.parse_args()
    src, out = args.input.resolve(), args.output.resolve()
    if out == src or src in out.parents or out in src.parents:
        ap.error("input and output must be disjoint")
    out.mkdir(parents=True, exist_ok=True)
    translations, loccount = localizations(src, out)
    items = item_catalog(src, out, translations)
    del translations
    defs = catalog_definitions(src, out)
    templates, tstats = template_catalog(src, out)
    semantics, tilestats = tile_catalog(src, out)
    mstats, records, diagnostics = map_catalog(src, out, templates)
    samplecount = sample_positions(out, records, templates, semantics)
    summary = {"localization_variables": loccount, "items": items, "templates": tstats,
               "tiles": tilestats, "map": mstats, "definitions": defs,
               "placement_example_candidates": samplecount,
               "diagnostics": {k: len(v) for k, v in diagnostics.items()}}
    dump(out / "catalog-summary.json", summary)
    # Save hashes after all catalog writes, avoiding a self-referential hash.
    dump(out / "catalog-files.json", [{"path": p.relative_to(out).as_posix(), "bytes": p.stat().st_size,
                                       "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
                                      for p in sorted(out.iterdir()) if p.is_file() and p.name != "catalog-files.json"])
    print(json.dumps({k: v for k, v in summary.items() if k != "definitions"}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
