#!/usr/bin/env python3
"""从 REA 调查工作区重新生成本目录下的两份产物。

用法:
    python3 docs/client-data/extract.py <rea-test 解压后的路径>

为什么要留这个脚本:两份产物是从 2.5 GB 的调查工作区里蒸馏出来的,
那个工作区不在仓库里(也不该在)。客户端升版之后重新跑一遍调查,
再跑这个脚本就能把码表和配方更新到新版本,不用重新摸索一遍抽法。

产物:
    photon-codes.json   客户端声明的 Photon 操作码 / 事件码全表
    recipes.jsonl.gz    可制造物品的配方(输入、数量、专注、产出、制造线)
"""
import collections
import gzip
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).parent

# items.xml 里一个可制造物品长这样(简化):
#   <simpleitem uniquename="T5_CLOTH" craftingcategory="fiber" tier="5" …>
#     <craftingrequirements silver="0" craftingfocus="94" amountcrafted="1">
#       <craftresource uniquename="T5_FIBER" count="3"/>
#       <craftresource uniquename="T4_CLOTH" count="1"/>
#
# 顶层标签有十几种(simpleitem / weapon / equipmentitem / mount …),
# 结构一致,统一扫一遍。不用 XML 解析器是因为这份文件 10.8 MB、
# 层级很深,正则按顶层标签切块反而更稳也更快。
ITEM_KINDS = (
    "simpleitem|equipmentitem|weapon|consumableitem|farmableitem|"
    "furnitureitem|journalitem|mount|trackingitem|labourercontract|crystalleagueitem"
)
ITEM_RE = re.compile(rf"<({ITEM_KINDS})\b([^>]*)>(.*?)</\1>", re.S)
REQ_RE = re.compile(r"<craftingrequirements\b([^>]*)>(.*?)</craftingrequirements>", re.S)
RES_RE = re.compile(r"<craftresource\b([^>]*)/?>")
ATTR_RE = re.compile(r'(\w[\w\-]*)="([^"]*)"')


def extract_recipes(items_xml: pathlib.Path) -> list[dict]:
    text = items_xml.read_text(encoding="utf-8", errors="replace")
    out = []
    for m in ITEM_RE.finditer(text):
        kind, head, body = m.group(1), m.group(2), m.group(3)
        if "craftingrequirements" not in body:
            continue
        head_attrs = dict(ATTR_RE.findall(head))

        recipes = []
        for r in REQ_RE.finditer(body):
            ra = dict(ATTR_RE.findall(r.group(1)))
            inputs = [
                {"id": c["uniquename"], "n": c.get("count", "1"),
                 "ench": c.get("enchantmentlevel", "0")}
                for c in (dict(ATTR_RE.findall(x)) for x in RES_RE.findall(r.group(2)))
                if c.get("uniquename")
            ]
            if not inputs:
                continue
            recipes.append({
                "silver": ra.get("silver", "0"),
                "focus": ra.get("craftingfocus", "0"),
                "time": ra.get("time", ""),
                "amount": ra.get("amountcrafted", "1"),
                "inputs": inputs,
            })
        if not recipes:
            continue

        out.append({
            "item_id": head_attrs.get("uniquename", ""),
            "kind": kind,
            "tier": head_attrs.get("tier", ""),
            "ench": head_attrs.get("enchantmentlevel", "0"),
            # craftcategory 决定这件货属于哪条精炼/制造线,
            # 也就决定了哪座城有那 40% / 15% 的专属加成
            "craft_category": head_attrs.get("craftingcategory", ""),
            "shop": head_attrs.get("shopcategory", ""),
            "shop_sub": head_attrs.get("shopsubcategory1", ""),
            "recipes": recipes,
        })
    return out


def extract_codes(constants_json: pathlib.Path) -> dict:
    # 这两个枚举就是客户端自己的码表。调查脚本 protocol_attributes.py
    # 也是读它们来给消息类型配码的,我们直接取源头
    data = json.loads(constants_json.read_text(encoding="utf-8-sig"))["constants"]
    tables = {"OperationCodes": {}, "EventCodes": {}}
    for entry in data:
        for short in tables:
            if entry["type"] == f"Albion.Common.Photon.{short}":
                tables[short].setdefault(entry["value"], []).append(entry["name"])
    return tables


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    root = pathlib.Path(sys.argv[1])
    ao = root / "investigations" / "albion-online"
    items_xml = ao / "static-data" / "decoded" / "items.xml"
    constants = ao / "metadata" / "results" / "all-field-constants.json"
    for p in (items_xml, constants):
        if not p.exists():
            print(f"找不到 {p}", file=sys.stderr)
            return 1

    recipes = extract_recipes(items_xml)
    blob = "\n".join(json.dumps(r, ensure_ascii=False, separators=(",", ":")) for r in recipes)
    with gzip.open(HERE / "recipes.jsonl.gz", "wt", encoding="utf-8", compresslevel=9) as f:
        f.write(blob)

    tables = extract_codes(constants)
    version = "unknown"
    manifest = ao / "deliverable-manifest.json"
    if manifest.exists():
        try:
            version = json.loads(manifest.read_text(encoding="utf-8-sig")).get(
                "client_version", version)
        except Exception:
            pass
    (HERE / "photon-codes.json").write_text(json.dumps({
        "_source": "Albion 客户端的 IL2CPP 元数据(metadata v39),"
                   "取自 Albion.Common.Photon.OperationCodes / EventCodes 两个枚举",
        "_note": "这是客户端里**声明**的码表,不代表服务端一定会发;"
                 "同一个码可能对应多个名字(枚举别名)",
        "client_version": version,
        "operations": {str(k): v for k, v in sorted(tables["OperationCodes"].items())},
        "events": {str(k): v for k, v in sorted(tables["EventCodes"].items())},
    }, ensure_ascii=False, indent=1), encoding="utf-8")

    kinds = collections.Counter(r["kind"] for r in recipes)
    print(f"配方:{len(recipes)} 个可制造物品,"
          f"{sum(len(r['recipes']) for r in recipes)} 条配方 {dict(kinds.most_common(4))}")
    print(f"码表:操作 {len(tables['OperationCodes'])} 个,事件 {len(tables['EventCodes'])} 个")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
