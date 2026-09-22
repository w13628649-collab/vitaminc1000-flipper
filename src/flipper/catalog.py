"""物品目录的 SQLite 存储。

固定不变的东西——物品名、市场分类树、物品图标——全部入库,跟扫描记录同一个
`flipper.db`。好处是备份只要拷一个文件,而不是一个 JSON 加一整个 icons 目录。

数据来源(`sync` 时拉一次):
- `formatted/items.json`(24MB)官方中英文名 + 完整的附魔变体 ID
- 根目录 `items.json`(17MB)市场分类树、tier、品质档数,**不含附魔变体**
- `data/categories.json`(随包 6KB)分类的中文名,来自 90MB 的 localization.json,
  极少变动,不值得让每个人 sync 时都下一遍
"""

from __future__ import annotations

import json
import math
import re
import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import httpx

ITEMS_JSON_URL = "https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/formatted/items.json"
RAW_ITEMS_URL = "https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/items.json"
ICON_URL = "https://render.albiononline.com/v1/item/{item_id}.png"
CATEGORIES_SEED = Path(__file__).parent / "data" / "categories.json"

SCHEMA_VERSION = 3

SCHEMA = """
CREATE TABLE IF NOT EXISTS items (
    item_id     TEXT PRIMARY KEY,
    name_zh     TEXT NOT NULL DEFAULT '',
    name_en     TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    subcategory TEXT NOT NULL DEFAULT '',
    family      TEXT NOT NULL DEFAULT '',
    tier        INTEGER NOT NULL DEFAULT 0,
    enchantment INTEGER NOT NULL DEFAULT 0,
    max_quality INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_items_browse
    ON items(category, subcategory, tier, enchantment);
CREATE INDEX IF NOT EXISTS idx_items_family ON items(family);

CREATE TABLE IF NOT EXISTS categories (
    id       TEXT NOT NULL,
    parent   TEXT NOT NULL DEFAULT '',
    label_zh TEXT NOT NULL DEFAULT '',
    label_en TEXT NOT NULL DEFAULT '',
    sort     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, parent)
);

-- 图标是二进制且永不变化,天生适合当 BLOB 存
CREATE TABLE IF NOT EXISTS icons (
    item_id    TEXT PRIMARY KEY,
    png        BLOB,
    fetched_at TEXT NOT NULL,
    missing    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS catalog_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
"""

_TIER_RE = re.compile(r"^T(\d)_")
# 物品族 = 去掉等级前缀、附魔后缀和材料的 _LEVELn 中缀之后剩下的骨架。
# T5_2H_FIRESTAFF@2 和 T4_2H_FIRESTAFF 是同一族;
# T4_CLOTH_LEVEL1@1 和 T4_CLOTH 也是同一族(材料的附魔是独立条目)。
_FAMILY_STRIP = re.compile(r"^T\d+_|_LEVEL\d+|@\d+")
_BRACE_RE = re.compile(r"\{([^{}]*)\}")


def family_of(item_id: str) -> str:
    return _FAMILY_STRIP.sub("", item_id)


def longest_common_suffix(names: list[str], min_ratio: float = 0.7) -> str:
    """一族里各等级的名字只差一个前缀:新手级/学徒级/老手级/专家级……

    取公共后缀就能得到族名,不用硬编码那张等级前缀表——游戏加了新等级或者改了
    叫法,这里不用跟着改。

    **只要求多数成员满足**,不是全部:火焰法杖那一族里混着一件叫「Vendetta之怒」
    的特殊武器,按全员取交集会把后缀打成空字符串。
    """
    uniq = sorted(set(n for n in names if n))
    if not uniq:
        return ""
    if len(uniq) == 1:
        return uniq[0]

    need = max(2, math.ceil(len(uniq) * min_ratio))
    best = ""
    for source in uniq:
        # 从最长的后缀往短了试,第一个够票的就是这个名字能贡献的最长后缀
        for start in range(len(source)):
            suffix = source[start:]
            if len(suffix) <= len(best):
                break
            if sum(1 for n in uniq if n.endswith(suffix)) >= need:
                best = suffix
                break
    # 中文等级都是"X级",英文是"X's",公共后缀会把这个尾巴带进来
    return best.lstrip("级's的 ")


def family_label(members: list[Item]) -> str:
    """给一族物品起个名。

    装备各等级只差一个前缀(老手级/专家级/大师级……),最长公共后缀正好是族名。
    材料不吃这套:细布 / 精布 / 华布 的公共后缀只剩一个"布"字,金属锭更是只剩
    "条"。退化成一两个字就改用这族里最低等级那件的全名——"钢条"比"条"认得出来。
    """
    names = [i.name_zh or i.name_en for i in members if (i.name_zh or i.name_en)]
    if not names:
        return members[0].item_id if members else ""
    label = longest_common_suffix(names)
    if len(label) >= 2:
        return label
    lowest = min(members, key=lambda i: (i.tier, i.enchantment, i.item_id))
    return lowest.name_zh or lowest.name_en or label


@dataclass(frozen=True)
class Item:
    item_id: str
    name_zh: str
    name_en: str
    category: str = ""
    subcategory: str = ""
    family: str = ""
    tier: int = 0
    enchantment: int = 0
    max_quality: int = 1

    @property
    def base_id(self) -> str:
        """剥掉附魔后缀。附魔变体不在原始 dump 里,分类要靠本体查。"""
        return self.item_id.partition("@")[0]

    @property
    def display_name(self) -> str:
        """中文优先。dump 里附魔物品和本体同名,补回 .N 以免列表里分不清。"""
        base = self.name_zh or self.name_en or self.item_id
        return f"{base}.{self.enchantment}" if self.enchantment else base

    def matches(self, needle: str) -> bool:
        return (
            needle in self.name_zh.lower()
            or needle in self.name_en.lower()
            or needle in self.item_id.lower()
        )


def expand_braces(pattern: str) -> list[str]:
    """T{4,5}_{ORE,FIBER} → [T4_ORE, T4_FIBER, T5_ORE, T5_FIBER]"""
    match = _BRACE_RE.search(pattern)
    if not match:
        return [pattern]
    head, tail = pattern[: match.start()], pattern[match.end() :]
    results: list[str] = []
    for option in match.group(1).split(","):
        results.extend(expand_braces(f"{head}{option.strip()}{tail}"))
    return results


class ItemCatalog:
    """物品目录。数据在 SQLite,搜索和浏览在内存里做——一万一千条,几 MB 而已。"""

    def __init__(self, db_path: Path, items: dict[str, Item], categories: list[dict]) -> None:
        self.db_path = db_path
        self._items = items
        self._categories = categories

    def __len__(self) -> int:
        return len(self._items)

    def __iter__(self):
        return iter(self._items.values())

    def get(self, item_id: str) -> Item | None:
        return self._items.get(item_id)

    def name_of(self, item_id: str) -> str:
        item = self._items.get(item_id)
        return item.display_name if item else item_id

    def en_name_of(self, item_id: str) -> str:
        item = self._items.get(item_id)
        return item.name_en if item and item.name_en else item_id

    # ── 打开 / 同步 ─────────────────────────────────────────────

    @staticmethod
    def connect(db_path: Path) -> sqlite3.Connection:
        db_path.parent.mkdir(parents=True, exist_ok=True)
        conn = sqlite3.connect(db_path)
        conn.row_factory = sqlite3.Row
        conn.executescript(SCHEMA)
        return conn

    @classmethod
    def open(cls, db_path: Path, *, auto_sync: bool = True) -> ItemCatalog:
        """从库里读目录。库是空的就先同步一次。"""
        conn = cls.connect(db_path)
        try:
            version = conn.execute(
                "SELECT value FROM catalog_meta WHERE key = 'schema_version'"
            ).fetchone()
            count = conn.execute("SELECT COUNT(*) FROM items").fetchone()[0]
            stale = not count or not version or int(version[0]) != SCHEMA_VERSION
            if stale:
                if not auto_sync:
                    raise RuntimeError("物品目录还没同步过,先跑 flipper items sync")
                cls._write(conn, fetch_catalog())
            return cls._load(conn, db_path)
        finally:
            conn.close()

    @classmethod
    def sync(cls, db_path: Path) -> ItemCatalog:
        conn = cls.connect(db_path)
        try:
            cls._write(conn, fetch_catalog())
            return cls._load(conn, db_path)
        finally:
            conn.close()

    @classmethod
    def _load(cls, conn: sqlite3.Connection, db_path: Path) -> ItemCatalog:
        items = {
            row["item_id"]: Item(
                item_id=row["item_id"],
                name_zh=row["name_zh"],
                name_en=row["name_en"],
                category=row["category"],
                subcategory=row["subcategory"],
                family=row["family"],
                tier=row["tier"],
                enchantment=row["enchantment"],
                max_quality=row["max_quality"],
            )
            for row in conn.execute("SELECT * FROM items")
        }
        categories = [dict(r) for r in conn.execute("SELECT * FROM categories ORDER BY sort")]
        return cls(db_path, items, categories)

    @staticmethod
    def _write(conn: sqlite3.Connection, payload: dict) -> None:
        with conn:
            conn.execute("DELETE FROM items")
            conn.execute("DELETE FROM categories")
            conn.executemany(
                "INSERT INTO items (item_id, name_zh, name_en, category, subcategory,"
                " family, tier, enchantment, max_quality) VALUES (?,?,?,?,?,?,?,?,?)",
                payload["items"],
            )
            conn.executemany(
                "INSERT INTO categories (id, parent, label_zh, label_en, sort)"
                " VALUES (?,?,?,?,?)",
                payload["categories"],
            )
            conn.executemany(
                "INSERT OR REPLACE INTO catalog_meta (key, value) VALUES (?, ?)",
                [
                    ("schema_version", str(SCHEMA_VERSION)),
                    ("synced_at", datetime.now(timezone.utc).isoformat()),
                    ("item_count", str(len(payload["items"]))),
                ],
            )

    def synced_at(self) -> str:
        conn = self.connect(self.db_path)
        try:
            row = conn.execute(
                "SELECT value FROM catalog_meta WHERE key = 'synced_at'"
            ).fetchone()
            return row[0] if row else ""
        finally:
            conn.close()

    # ── 查询 ────────────────────────────────────────────────────

    def search(self, query: str, limit: int = 30) -> list[Item]:
        """中文名、英文名、物品 ID 都能搜。前缀匹配排在子串匹配前面。"""
        needle = query.strip().lower()
        if not needle:
            return []
        exact: list[Item] = []
        prefix: list[Item] = []
        rest: list[Item] = []
        for item in self._items.values():
            if not item.matches(needle):
                continue
            if needle in (item.name_zh.lower(), item.name_en.lower(), item.item_id.lower()):
                exact.append(item)
            elif (
                item.name_zh.lower().startswith(needle)
                or item.name_en.lower().startswith(needle)
                or item.item_id.lower().startswith(needle)
            ):
                prefix.append(item)
            else:
                rest.append(item)
        order = lambda i: (i.tier, i.enchantment, i.item_id)  # noqa: E731
        return (sorted(exact, key=order) + sorted(prefix, key=order) + sorted(rest, key=order))[
            :limit
        ]

    def browse(
        self,
        *,
        category: str = "",
        subcategory: str = "",
        family: str = "",
        tier: int = 0,
        enchantment: int = -1,
        limit: int = 300,
    ) -> list[Item]:
        """按市场分类翻物品,对应游戏里那几个下拉框。"""
        picked = [
            item
            for item in self._items.values()
            if (not category or item.category == category)
            and (not subcategory or item.subcategory == subcategory)
            and (not family or item.family == family)
            and (not tier or item.tier == tier)
            and (enchantment < 0 or item.enchantment == enchantment)
        ]
        picked.sort(key=lambda i: (i.tier, i.enchantment, i.name_zh or i.name_en, i.item_id))
        return picked[:limit]

    def resolve(
        self, patterns: list[str], exclude: list[str] | None = None
    ) -> tuple[list[str], list[str]]:
        """展开模式并对目录校验。

        返回 (命中的 ID, 目录里不存在的 ID)。不存在的通常意味着写错了 tier
        或者游戏改了命名,调用方应该把它报出来而不是静默丢掉。
        """
        excluded = set(exclude or [])
        resolved: list[str] = []
        missing: list[str] = []
        seen: set[str] = set()
        for pattern in patterns:
            for candidate in expand_braces(pattern):
                if candidate in excluded or candidate in seen:
                    continue
                seen.add(candidate)
                if candidate in self._items:
                    resolved.append(candidate)
                else:
                    missing.append(candidate)
        return resolved, missing

    def menu_tree(self) -> list[dict]:
        """三级菜单用的完整树:大类 → 子类 → 物品族。

        一次全给前端,悬停展开就不用再等网络了。
        """
        labels = {(c["id"], c["parent"]): c for c in self._categories}

        def label_of(cid: str, parent: str = "") -> str:
            row = labels.get((cid, parent)) or labels.get((cid, ""))
            if not row:
                return cid
            return row["label_zh"] or row["label_en"] or cid

        grouped: dict[str, dict[str, dict[str, list[Item]]]] = {}
        for item in self._items.values():
            if not item.category:
                continue
            grouped.setdefault(item.category, {}).setdefault(item.subcategory, {}).setdefault(
                item.family, []
            ).append(item)

        order = {c["id"]: c["sort"] for c in self._categories if not c["parent"]}
        sub_order = {(c["parent"], c["id"]): c["sort"] for c in self._categories if c["parent"]}

        tree = []
        for cid, subs in sorted(grouped.items(), key=lambda kv: order.get(kv[0], 9999)):
            sub_nodes = []
            for sid, families in sorted(
                subs.items(), key=lambda kv: sub_order.get((cid, kv[0]), 9999)
            ):
                family_nodes = [
                    {
                        "key": fkey,
                        "label": family_label(members),
                        "count": len(members),
                        "tiers": sorted({i.tier for i in members}),
                    }
                    for fkey, members in families.items()
                ]
                family_nodes.sort(key=lambda f: (min(f["tiers"] or [0]), f["label"]))
                sub_nodes.append(
                    {
                        "id": sid,
                        "label": label_of(sid, cid),
                        "count": sum(len(m) for m in families.values()),
                        "families": family_nodes,
                    }
                )
            tree.append(
                {
                    "id": cid,
                    "label": label_of(cid),
                    "count": sum(n["count"] for n in sub_nodes),
                    "subs": sub_nodes,
                }
            )
        return tree


def fetch_catalog() -> dict:
    """拉上游两份 dump,合并成可以直接写库的行。"""
    with httpx.Client(timeout=300.0, follow_redirects=True) as client:
        headers = {"Accept-Encoding": "gzip"}
        named = client.get(ITEMS_JSON_URL, headers=headers)
        named.raise_for_status()
        raw = client.get(RAW_ITEMS_URL, headers=headers)
        raw.raise_for_status()

    meta = _parse_raw_items(raw.json())
    rows = []
    for row in named.json():
        item_id = row.get("UniqueName")
        if not item_id:
            continue
        names = row.get("LocalizedNames") or {}
        zh = (names.get("ZH-CN") or "").strip()
        en = (names.get("EN-US") or "").strip()
        if not (zh or en):
            continue  # dump 里的占位条目,没有交易价值
        category, subcategory, max_quality = meta.get(item_id.partition("@")[0], ("", "", 1))
        tier_match = _TIER_RE.match(item_id)
        _, _, suffix = item_id.partition("@")
        rows.append(
            (
                item_id,
                zh,
                en,
                category,
                subcategory,
                family_of(item_id),
                int(tier_match.group(1)) if tier_match else 0,
                int(suffix) if suffix.isdigit() else 0,
                max_quality,
            )
        )

    return {"items": rows, "categories": _category_rows()}


def _category_rows() -> list[tuple]:
    spec = json.loads(CATEGORIES_SEED.read_text(encoding="utf-8"))
    names = spec["names"]
    rows: list[tuple] = []
    for index, node in enumerate(spec["tree"]):
        cid = node["id"]
        zh, en = names.get(cid, ["", cid])
        rows.append((cid, "", zh, en, index))
        for sub_index, sid in enumerate(node["subs"]):
            szh, sen = names.get(sid, ["", sid])
            rows.append((sid, cid, szh, sen, sub_index))
    return rows


def _parse_raw_items(payload: dict) -> dict[str, tuple[str, str, int]]:
    """从原始 dump 抽出 id → (大类, 子类, 品质档数)。

    物品按类型散在 simpleitem / equipmentitem / weapon 等十几个节点里,
    结构一致,统一扫一遍。
    """
    root = payload.get("items", {})
    meta: dict[str, tuple[str, str, int]] = {}
    for key, rows in root.items():
        if key.startswith("@") or key == "shopcategories":
            continue
        if not isinstance(rows, list):
            rows = [rows]
        for row in rows:
            if not isinstance(row, dict):
                continue
            item_id = row.get("@uniquename")
            if not item_id:
                continue
            try:
                max_quality = int(row.get("@maxqualitylevel", 1))
            except (TypeError, ValueError):
                max_quality = 1
            meta[item_id] = (
                row.get("@shopcategory", ""),
                row.get("@shopsubcategory1", ""),
                max_quality,
            )
    return meta


def load_icon(db_path: Path, item_id: str) -> bytes | None:
    """图标取自库;没有就去 render 站拉一次再存进去。

    直接让浏览器打 render.albiononline.com,翻一页分类就是上百个并发请求,
    会被对面限流,列表里一半图标是空的。
    """
    conn = ItemCatalog.connect(db_path)
    try:
        row = conn.execute(
            "SELECT png, missing FROM icons WHERE item_id = ?", (item_id,)
        ).fetchone()
        if row is not None:
            return None if row["missing"] else bytes(row["png"])

        try:
            resp = httpx.get(
                ICON_URL.format(item_id=item_id), params={"size": 64}, timeout=30.0,
                follow_redirects=True,
            )
            png = resp.content if resp.status_code == 200 else None
        except httpx.HTTPError:
            return None  # 网络抖动别写进库,下次还有机会

        with conn:
            conn.execute(
                "INSERT OR REPLACE INTO icons (item_id, png, fetched_at, missing)"
                " VALUES (?, ?, ?, ?)",
                (item_id, png, datetime.now(timezone.utc).isoformat(), 0 if png else 1),
            )
        return png
    finally:
        conn.close()
