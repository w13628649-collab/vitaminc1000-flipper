"""销量排行的聚合口径。

这些口径决定榜单顺序,错了整张表就是错的。
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from flipper.catalog import Item, ItemCatalog
from flipper.models import HistorySeries
from flipper.ranking import ALL_CITIES, coverage, rank, store_history

NOW = datetime(2026, 9, 21, 6, 0, tzinfo=timezone.utc)

CATALOG = ItemCatalog(
    Path("/nonexistent.db"),
    {
        i.item_id: i
        for i in (
            Item("T5_CLOTH", "精布", "Ornate Cloth", "crafting", "refinedresources", "CLOTH", 5),
            Item("T5_ORE", "钛矿石", "Titanium Ore", "crafting", "resources", "ORE", 5),
        )
    },
    [],
)


def day(offset: int) -> str:
    return (NOW.date() - timedelta(days=offset)).isoformat() + "T00:00:00"


def series(item_id: str, city: str, points: list[tuple[int, int, int]]) -> HistorySeries:
    """points = [(几天前, 件数, 均价)]"""
    return HistorySeries(
        location=city,
        item_id=item_id,
        quality=1,
        data=[
            {"timestamp": day(ago), "item_count": qty, "avg_price": price}
            for ago, qty, price in points
        ],
    )


@pytest.fixture
def db(tmp_path: Path) -> Path:
    return tmp_path / "t.db"


def test_剔除还没走完的当天(db):
    store_history(db, [series("T5_CLOTH", "Lymhurst", [
        (1, 1000, 300), (2, 1000, 300), (0, 3, 9000),  # offset 0 就是今天
    ])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    assert row.daily_qty == pytest.approx(1000)
    assert row.price_max == 300  # 今天那个 9000 的离谱均价不参与


def test_日均分母是有数据的天数不是窗口长度(db):
    """AODP 缺失某天几乎总是"那天没人上传",不是"那天没成交"。"""
    store_history(db, [series("T5_CLOTH", "Lymhurst", [(1, 1000, 300), (2, 2000, 300)])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    assert row.days_with_data == 2
    assert row.daily_qty == pytest.approx(1500)  # 不是 3000/7


def test_均价按成交量加权(db):
    store_history(db, [series("T5_CLOTH", "Lymhurst", [(1, 9000, 100), (2, 1000, 1100)])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    # 算术平均会给出 600,把零星成交日和万笔成交日等权
    assert row.avg_price == pytest.approx(200)


def test_全服口径把各城合并成一行(db):
    store_history(db, [
        series("T5_CLOTH", "Lymhurst", [(1, 1000, 300)]),
        series("T5_CLOTH", "Martlock", [(1, 3000, 300)]),
    ])
    rows = rank(db, CATALOG, window_days=7, city=ALL_CITIES, now=NOW)
    assert len(rows) == 1
    assert rows[0].city == "全服"
    assert rows[0].total_qty == 4000

    per_city = rank(db, CATALOG, window_days=7, city="Lymhurst", now=NOW)
    assert len(per_city) == 1
    assert (per_city[0].city, per_city[0].total_qty) == ("Lymhurst", 1000)


def test_默认按日均流水排而不是件数(db):
    """一天 5 千件、单价 3 万的 T8 材料,和一天 15 万件、单价 23 银的,不是一个生意。"""
    store_history(db, [
        series("T5_CLOTH", "Lymhurst", [(1, 150_000, 23)]),   # 件数高,流水 345 万
        series("T5_ORE", "Lymhurst", [(1, 5_000, 30_000)]),   # 件数低,流水 1.5 亿
    ])
    assert [r.item_id for r in rank(db, CATALOG, window_days=7, now=NOW)] == [
        "T5_ORE", "T5_CLOTH"
    ]
    assert [r.item_id for r in rank(db, CATALOG, window_days=7, sort_by="daily_qty", now=NOW)] == [
        "T5_CLOTH", "T5_ORE"
    ]


def test_波动率和价格区间(db):
    store_history(db, [series("T5_CLOTH", "Lymhurst", [
        (1, 100, 100), (2, 100, 200), (3, 100, 300),
    ])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    assert (row.price_min, row.price_median, row.price_max) == (100, 200, 300)
    assert row.volatility == pytest.approx(1.0)  # (300-100)/200


def test_窗口之外的日线不参与(db):
    store_history(db, [series("T5_CLOTH", "Lymhurst", [(1, 1000, 300), (20, 9999, 300)])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    assert row.total_qty == 1000


def test_按分类筛选(db):
    store_history(db, [
        series("T5_CLOTH", "Lymhurst", [(1, 1000, 300)]),
        series("T5_ORE", "Lymhurst", [(1, 1000, 300)]),
    ])
    rows = rank(db, CATALOG, window_days=7, subcategory="refinedresources", now=NOW)
    assert [r.item_id for r in rows] == ["T5_CLOTH"]


def test_重复抓同一天是覆盖不是累加(db):
    """history 会被反复抓,同一天必须以最新一次为准,否则销量会翻倍。"""
    store_history(db, [series("T5_CLOTH", "Lymhurst", [(1, 1000, 300)])])
    store_history(db, [series("T5_CLOTH", "Lymhurst", [(1, 1200, 310)])])
    row = rank(db, CATALOG, window_days=7, now=NOW)[0]
    assert row.total_qty == 1200
    assert coverage(db)["rows"] == 1


def test_趋势用回归而不是首尾相减(db):
    """某天有人挂了个离谱均价,首尾相减整条趋势就反了,回归不会。"""
    from flipper.ranking import linear_trend

    # 整体一路上涨,但最后一天砸了个低价
    change, fit = linear_trend([(i, 100 + i * 10) for i in range(9)] + [(9, 60)])
    assert change > 0  # 首尾相减会得出 -40%
    assert fit < 0.9   # 那个离群点把拟合拉差了


def test_跌幅不会超过百分之百(db):
    """基准取拟合直线的起点。用窗口均值当分母,300 跌到 10 会算出 -187%。"""
    from flipper.ranking import linear_trend

    change, _ = linear_trend([(i, 300 - i * 29) for i in range(11)])
    assert -100 < change < -90


def test_震荡不按方向判(db):
    """斜率再陡,拟合不好也只是噪声。"""
    from flipper.ranking import classify_trend, linear_trend

    change, fit = linear_trend([(i, 150 + (40 if i % 2 else -40)) for i in range(10)])
    assert fit < 0.3
    assert classify_trend(change, fit) == "choppy"
    assert classify_trend(30.0, 0.9) == "up"
    assert classify_trend(-30.0, 0.9) == "down"
    assert classify_trend(1.0, 0.99) == "flat"  # 涨跌太小,方向没意义


def test_排序可以升序也可以降序(db):
    store_history(db, [
        series("T5_CLOTH", "Lymhurst", [(1, 1000, 100)]),
        series("T5_ORE", "Lymhurst", [(1, 1000, 500)]),
    ])
    high_first = rank(db, CATALOG, window_days=7, sort_by="avg_price", now=NOW)
    low_first = rank(db, CATALOG, window_days=7, sort_by="avg_price", descending=False, now=NOW)
    assert [r.item_id for r in high_first] == ["T5_ORE", "T5_CLOTH"]
    assert [r.item_id for r in low_first] == ["T5_CLOTH", "T5_ORE"]


def test_全服口径下同一天多城先按量加权再谈趋势(db):
    """不然两城价格差一截时,折线会在两个水平之间来回跳,看着像震荡。"""
    store_history(db, [
        series("T5_CLOTH", "Lymhurst", [(3, 1000, 100), (2, 1000, 100), (1, 1000, 100)]),
        series("T5_CLOTH", "Martlock", [(3, 1000, 300), (2, 1000, 300), (1, 1000, 300)]),
    ])
    row = rank(db, CATALOG, window_days=7, city=ALL_CITIES, now=NOW)[0]
    assert [p[1] for p in row.series] == [200, 200, 200]  # 每天都是加权后的 200
    assert row.trend == "flat"
