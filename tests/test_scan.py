"""端到端管道测试:假的 AODP 响应 → 排序后的机会列表。"""

from __future__ import annotations

import json
from pathlib import Path
from datetime import datetime, timedelta, timezone

import httpx

from flipper.config import Config
from flipper.catalog import Item, ItemCatalog
from flipper.scan import run_scan

NOW = datetime(2026, 9, 21, 12, 0, tzinfo=timezone.utc)
FRESH = (NOW - timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%S")

CATALOG = ItemCatalog(
    Path("/nonexistent.db"),
    {
        i.item_id: i
        for i in (
            Item("T5_CLOTH", "精布", "Ornate Cloth"),
            Item("T5_WOOD", "杉木", "Cedar Logs"),
            Item("T5_ORE", "钛矿石", "Titanium Ore"),
        )
    },
    [],
)

# 厚利少量 vs 薄利多销 vs troll 挂单
PRICES = [
    {"item_id": "T5_CLOTH", "city": "Lymhurst", "quality": 1,
     "sell_price_min": 1450, "sell_price_min_date": FRESH,
     "buy_price_max": 1000, "buy_price_max_date": FRESH},
    {"item_id": "T5_WOOD", "city": "Lymhurst", "quality": 1,
     "sell_price_min": 1120, "sell_price_min_date": FRESH,
     "buy_price_max": 1000, "buy_price_max_date": FRESH},
    {"item_id": "T5_ORE", "city": "Lymhurst", "quality": 1,
     "sell_price_min": 40_000, "sell_price_min_date": FRESH,   # 1 件货挂 40 倍价
     "buy_price_max": 1000, "buy_price_max_date": FRESH},
]


def _history(item_id: str, qty: int, price: int) -> dict:
    days = [(NOW - timedelta(days=n)).strftime("%Y-%m-%dT00:00:00") for n in range(1, 6)]
    return {
        "location": "Lymhurst",
        "item_id": item_id,
        "quality": 1,
        "data": [{"timestamp": d, "item_count": qty, "avg_price": price} for d in days],
    }


HISTORY = [
    _history("T5_CLOTH", 600, 1200),     # 日均 600 件 → 可吃 120 件
    _history("T5_WOOD", 50_000, 1050),   # 日均 5 万件 → 可吃 1 万件
    _history("T5_ORE", 50_000, 1050),
]


def handler(request: httpx.Request) -> httpx.Response:
    payload = HISTORY if "/history/" in request.url.path else PRICES
    return httpx.Response(200, json=json.loads(json.dumps(payload)))


def run() -> list:
    config = Config(cities=["Lymhurst"], capital=10_000_000, items={"patterns": ["T5_CLOTH"]})
    result = run_scan(
        config, CATALOG, now=NOW, transport=httpx.MockTransport(handler)
    )
    return result


def test_排序主键是日化绝对收益而不是利润率():
    result = run()
    assert [o.item_id for o in result.opportunities] == ["T5_WOOD", "T5_CLOTH"]
    薄利, 厚利 = result.opportunities
    # 薄利那条毛利率更低,却排在前面 —— 因为一天能做的量大得多
    assert 薄利.margin < 厚利.margin
    assert 薄利.daily_profit > 厚利.daily_profit


def test_troll挂单没有进主榜():
    result = run()
    assert "T5_ORE" not in [o.item_id for o in result.opportunities]
    assert result.reject_counts["deviation"] == 1


def test_每个机会都带得出数据新鲜度():
    for opp in run().opportunities:
        assert opp.data_age_hours == 1.0
        assert opp.buy_age_hours is not None and opp.sell_age_hours is not None


def test_请求被批量合并():
    result = run()
    # 3 个物品 × 2 个端点,批量后应该只有 2 次请求
    assert result.request_count == 2
