"""销量排行榜。

历史数据增量存进 `history_daily` 表,排行榜从库里聚合,不是每次都打 API。
这样做还有个附带好处:**AODP 的 history 只给最近一段,自己存就能越攒越长**——
跑上几个月,手里就有一份上游给不了的长历史。

三个口径,和 history.py 保持一致:

1. **丢掉当天那个点。** 还没走完的一天量必然偏低。
2. **日均的分母是"窗口内有数据的天数"**,不是窗口长度。AODP 是众包数据,
   某天缺失几乎总是"那天没人上传"而不是"那天没成交"。
3. **均价按成交量加权。** 算术平均会让零星成交日和万笔成交日等权。

一个绕不开的限制:**`item_count` 只统计卖单成交**,买单那边的成交量拿不到。
所以这里的销量是偏低的一侧,真实流动性只会更高。
"""

from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from datetime import date, datetime, timedelta, timezone
from pathlib import Path
from statistics import median

from .catalog import ItemCatalog
from .models import HistorySeries

ALL_CITIES = "__all__"

SCHEMA = """
CREATE TABLE IF NOT EXISTS history_daily (
    item_id    TEXT NOT NULL,
    city       TEXT NOT NULL,
    quality    INTEGER NOT NULL,
    day        TEXT NOT NULL,
    item_count INTEGER NOT NULL,
    avg_price  INTEGER NOT NULL,
    PRIMARY KEY (item_id, city, quality, day)
);
CREATE INDEX IF NOT EXISTS idx_hist_day ON history_daily(day);
CREATE INDEX IF NOT EXISTS idx_hist_item ON history_daily(item_id, city, quality);
"""


@dataclass
class RankRow:
    item_id: str
    item_name: str
    city: str
    quality: int
    daily_qty: float
    """日均成交件数(只含卖单成交)。"""
    daily_silver: float
    """日均流水 = 日均件数 × 加权均价。跟本金可比的量纲。"""
    total_qty: int
    avg_price: float
    price_min: int
    price_max: int
    price_median: float
    volatility: float
    """(最高 - 最低) / 中位。价格摆得越宽,价差机会越多,风险也越大。"""
    trend_pct: float
    """最小二乘拟合出的窗口内涨跌幅(%)。首尾相减会被单日异常值带偏,回归不会。"""
    trend_fit: float
    """拟合优度 R²。低就说明是来回震荡,那条斜线没有意义。"""
    trend: str
    """up / down / choppy / flat —— 拿涨跌幅和 R² 一起判的。"""
    series: list[list]
    """[[天序号, 均价], …],给前端画折线。缺数据的日子直接不出现。"""
    days_with_data: int
    last_day: str


def linear_trend(points: list[tuple[int, float]]) -> tuple[float, float]:
    """最小二乘拟合,返回 (窗口内涨跌幅, R²)。

    直接拿首尾两天相减很容易被单日异常值带偏——某天有人挂了个离谱均价,
    整条趋势就反了。回归用上全部点,稳得多。R² 则回答"这条斜线值不值得信":
    低于 0.3 基本就是来回震荡,方向没有意义。
    """
    n = len(points)
    if n < 3:
        return 0.0, 0.0
    mean_x = sum(x for x, _ in points) / n
    mean_y = sum(y for _, y in points) / n
    if mean_y <= 0:
        return 0.0, 0.0

    sxx = sum((x - mean_x) ** 2 for x, _ in points)
    if sxx == 0:
        return 0.0, 0.0
    sxy = sum((x - mean_x) * (y - mean_y) for x, y in points)
    slope = sxy / sxx

    min_x = min(x for x, _ in points)
    span = max(x for x, _ in points) - min_x
    # 基准取拟合直线的**起点**,不是窗口均值。用均值当分母时,
    # 一条从 300 跌到 10 的线会算出 -187%,跌幅超过 100% 说不通。
    fitted_start = mean_y + slope * (min_x - mean_x)
    base = fitted_start if fitted_start > 0 else mean_y
    change = slope * span / base * 100

    ss_tot = sum((y - mean_y) ** 2 for _, y in points)
    if ss_tot == 0:
        return 0.0, 1.0
    ss_res = sum((y - (mean_y + slope * (x - mean_x))) ** 2 for x, y in points)
    return change, max(0.0, 1 - ss_res / ss_tot)


def classify_trend(change_pct: float, fit: float, *, flat_band: float = 4.0) -> str:
    if abs(change_pct) < flat_band:
        return "flat"
    # 斜率再陡,拟合不好也只是噪声
    if fit < 0.3:
        return "choppy"
    return "up" if change_pct > 0 else "down"


def connect(db_path: Path) -> sqlite3.Connection:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    conn.executescript(SCHEMA)
    return conn


def store_history(db_path: Path, series_list: list[HistorySeries]) -> int:
    """写入(或覆盖)日线。同一天重复抓到以最新一次为准。"""
    rows = [
        (
            series.item_id,
            series.location,
            series.quality,
            point.timestamp.date().isoformat(),
            point.item_count,
            point.avg_price,
        )
        for series in series_list
        for point in series.data
        if point.timestamp is not None
    ]
    if not rows:
        return 0
    conn = connect(db_path)
    try:
        with conn:
            conn.executemany(
                "INSERT OR REPLACE INTO history_daily"
                " (item_id, city, quality, day, item_count, avg_price) VALUES (?,?,?,?,?,?)",
                rows,
            )
        return len(rows)
    finally:
        conn.close()


def coverage(db_path: Path) -> dict:
    """库里现在攒了多少历史。"""
    conn = connect(db_path)
    try:
        row = conn.execute(
            "SELECT COUNT(*) AS rows, COUNT(DISTINCT item_id) AS items,"
            " COUNT(DISTINCT day) AS days, MIN(day) AS first_day, MAX(day) AS last_day"
            " FROM history_daily"
        ).fetchone()
        return dict(row) if row else {}
    finally:
        conn.close()


def rank(
    db_path: Path,
    catalog: ItemCatalog,
    *,
    window_days: int = 7,
    city: str = ALL_CITIES,
    quality: int = 1,
    category: str = "",
    subcategory: str = "",
    limit: int = 50,
    sort_by: str = "daily_silver",
    descending: bool = True,
    now: datetime | None = None,
) -> list[RankRow]:
    """按销量排。默认看日均流水 —— 销量优先的策略下,这个比件数更有可比性。"""
    now = now or datetime.now(timezone.utc)
    today = now.date()
    # 当天那根还没走完,剔掉;窗口从昨天往前数
    last_full_day = today - timedelta(days=1)
    first_day = last_full_day - timedelta(days=window_days - 1)

    wanted: set[str] | None = None
    if category or subcategory:
        wanted = {
            item.item_id
            for item in catalog.browse(category=category, subcategory=subcategory, limit=10**6)
        }

    conn = connect(db_path)
    try:
        sql = (
            "SELECT item_id, city, quality, day, item_count, avg_price FROM history_daily"
            " WHERE day BETWEEN ? AND ? AND quality = ?"
        )
        params: list = [first_day.isoformat(), last_full_day.isoformat(), quality]
        if city != ALL_CITIES:
            sql += " AND city = ?"
            params.append(city)
        rows = conn.execute(sql, params).fetchall()
    finally:
        conn.close()

    # 全服口径把各城合并成一行;单城口径按 (物品, 城市) 分开
    buckets: dict[tuple[str, str], list[sqlite3.Row]] = {}
    for row in rows:
        if wanted is not None and row["item_id"] not in wanted:
            continue
        key = (row["item_id"], "全服" if city == ALL_CITIES else row["city"])
        buckets.setdefault(key, []).append(row)

    ranked: list[RankRow] = []
    for (item_id, label), group in buckets.items():
        total_qty = sum(r["item_count"] for r in group)
        if total_qty <= 0:
            continue
        days = len({r["day"] for r in group})
        prices = [r["avg_price"] for r in group if r["avg_price"] > 0]
        if not prices or not days:
            continue

        weighted = sum(r["avg_price"] * r["item_count"] for r in group) / total_qty
        daily_qty = total_qty / days
        mid = median(prices)

        # 全服口径下同一天有多城数据,先按量加权成一个日均价再谈趋势
        per_day: dict[str, tuple[int, int]] = {}
        for r in group:
            qty, amount = per_day.get(r["day"], (0, 0))
            per_day[r["day"]] = (qty + r["item_count"], amount + r["avg_price"] * r["item_count"])
        daily_price = {
            d: amount / qty for d, (qty, amount) in per_day.items() if qty > 0
        }
        ordered = sorted(daily_price.items())
        origin = date.fromisoformat(ordered[0][0])
        curve = [((date.fromisoformat(d) - origin).days, price) for d, price in ordered]
        change, fit = linear_trend(curve)
        ranked.append(
            RankRow(
                item_id=item_id,
                item_name=catalog.name_of(item_id),
                city=label,
                quality=quality,
                daily_qty=daily_qty,
                daily_silver=daily_qty * weighted,
                total_qty=total_qty,
                avg_price=weighted,
                price_min=min(prices),
                price_max=max(prices),
                price_median=mid,
                volatility=(max(prices) - min(prices)) / mid if mid else 0.0,
                trend_pct=change,
                trend_fit=fit,
                trend=classify_trend(change, fit),
                series=[[x, round(y)] for x, y in curve],
                days_with_data=days,
                last_day=max(r["day"] for r in group),
            )
        )

    keys = {
        "daily_silver": lambda r: r.daily_silver,
        "daily_qty": lambda r: r.daily_qty,
        "volatility": lambda r: r.volatility,
        "avg_price": lambda r: r.avg_price,
        "total_qty": lambda r: r.total_qty,
        "price_min": lambda r: r.price_min,
        "price_max": lambda r: r.price_max,
        "price_median": lambda r: r.price_median,
        "trend_pct": lambda r: r.trend_pct,
        "days_with_data": lambda r: r.days_with_data,
        "item_name": lambda r: r.item_name,
    }
    ranked.sort(key=keys.get(sort_by, keys["daily_silver"]), reverse=descending)
    return ranked[:limit]


def cities_in_db(db_path: Path) -> list[str]:
    conn = connect(db_path)
    try:
        return [r[0] for r in conn.execute("SELECT DISTINCT city FROM history_daily ORDER BY city")]
    finally:
        conn.close()
