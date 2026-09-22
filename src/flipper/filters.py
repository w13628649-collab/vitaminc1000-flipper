"""Troll 过滤 —— 自己写价差工具时最容易漏、代价最高的一环。

根本原因:AODP 的 prices 端点**不返回挂单数量**,只有价格。
市场上大量恶意挂单——1 件货、价格是正常值几十倍的卖单,或极低的买单,
专门污染数据。天真的计算器会把它们显示成天大的机会,买了就套死。

五层,全部通过才进主榜:

| 层 | 规则 |
|---|---|
| 新鲜度 | 买卖两侧时间戳距今 < max_hours |
| 双边确认 | 买价和卖价都必须存在且新鲜,单边不进主榜 |
| 偏离度 | 价格 / 7 日均价 落在 [deviation_min, deviation_max] 外 → 丢 |
| 成交量 | 日均成交件数 × 均价 < min_daily_volume_silver → 丢 |
| 毛利率上限 | margin > max_margin → 两边多半同时是 troll(SPEC 四层的缺口) |
"""

from __future__ import annotations

from datetime import datetime

from .config import Config
from .economics import size_position, unit_economics
from .history import MarketStats
from .catalog import ItemCatalog
from .models import Confidence, Opportunity, PriceRecord, RejectedRow

# 距离阈值多近算"边缘",触发 confidence 降级
_EDGE_MARGIN = 0.15
_VOLUME_EDGE_MULTIPLIER = 1.25


def evaluate(
    record: PriceRecord,
    stats: MarketStats | None,
    config: Config,
    catalog: ItemCatalog,
    now: datetime,
) -> Opportunity | RejectedRow:
    """把一条 (物品, 城市) 的市场快照判成机会或拒绝原因。"""
    def reject(reason: str, detail: str = "") -> RejectedRow:
        return RejectedRow(
            item_id=record.item_id, city=record.city, reason=reason, detail=detail
        )

    filters = config.filters
    warnings: list[str] = []
    hints: list[str] = []
    edge = False

    # ---- 第 1 层:双边存在 -------------------------------------------------
    # 价格 0 是 AODP 的"无数据"哨兵,不是"白送"。
    has_sell = record.sell_price_min > 0
    has_buy = record.buy_price_max > 0
    if not (has_sell and has_buy):
        side = "只有卖单" if has_sell else ("只有买单" if has_buy else "两侧都无数据")
        return reject("one_sided", side)

    # ---- 第 2 层:新鲜度 ---------------------------------------------------
    sell_age = record.age_hours("sell_price_min_date", now)
    buy_age = record.age_hours("buy_price_max_date", now)
    if sell_age is None or buy_age is None:
        return reject("no_timestamp", "价格有值但时间戳缺失")
    max_age = max(sell_age, buy_age)
    if max_age > config.freshness.max_hours:
        return reject("stale", f"{max_age:.1f}h > {config.freshness.max_hours}h")

    # ---- 第 3 层:历史可用 -------------------------------------------------
    if stats is None:
        return reject("no_history", "该城市没有成交历史")
    history_age = stats.history_age_days(now)
    if history_age is None or history_age > filters.max_history_gap_days:
        return reject("stale_history", f"最后成交距今 {history_age:.1f}d" if history_age else "无")
    if stats.avg_price_7d <= 0:
        return reject("no_baseline", "7 日均价为 0,无法做偏离度判断")
    if stats.days_with_data_7d < filters.min_days_with_data_7d:
        return reject(
            "thin_history", f"7 日内仅 {stats.days_with_data_7d} 天有数据"
        )

    # ---- 第 4 层:偏离度 ---------------------------------------------------
    sell_dev = record.sell_price_min / stats.avg_price_7d
    buy_dev = record.buy_price_max / stats.avg_price_7d
    for label, dev in (("卖价", sell_dev), ("买价", buy_dev)):
        if not (filters.deviation_min <= dev <= filters.deviation_max):
            return reject("deviation", f"{label}偏离 7 日均价 {dev:.2f}x")
        span = filters.deviation_max - filters.deviation_min
        if (
            dev - filters.deviation_min < span * _EDGE_MARGIN
            or filters.deviation_max - dev < span * _EDGE_MARGIN
        ):
            edge = True
            warnings.append(f"{label}偏离 {dev:.2f}x 接近阈值边缘")

    # ---- 第 5 层:成交量 ---------------------------------------------------
    if stats.daily_volume_silver < filters.min_daily_volume_silver:
        return reject(
            "low_volume", f"日流水 {stats.daily_volume_silver:,.0f} 银"
        )
    if stats.daily_volume_silver < filters.min_daily_volume_silver * _VOLUME_EDGE_MULTIPLIER:
        edge = True
        warnings.append("日流水接近下限")

    # ---- 交叉盘:买价 >= 卖价 ----------------------------------------------
    # 真实市场不会持续存在这种状态(会立刻自己成交),出现说明两侧快照
    # 来自不同时间点,是陈旧数据的强信号。
    if record.buy_price_max >= record.sell_price_min:
        detail = f"买 {record.buy_price_max} >= 卖 {record.sell_price_min}"
        if filters.reject_crossed_book:
            return reject("crossed_book", detail)
        edge = True
        warnings.append(f"交叉盘({detail}),两侧快照时间不一致")

    # ---- 利润 --------------------------------------------------------------
    unit = unit_economics(record.buy_price_max, record.sell_price_min, config.economics)
    if unit.profit_per_unit <= 0:
        return reject(
            "unprofitable",
            f"税后亏 {unit.profit_per_unit:.1f} 银/件(摩擦 {config.economics.round_trip_friction:.1%})",
        )
    if unit.margin > filters.max_margin:
        return reject("implausible_margin", f"毛利率 {unit.margin:.0%} 高得不真实")

    # ---- 吃单量 ------------------------------------------------------------
    sizing = size_position(unit, stats.daily_volume_qty, config.capital, config.sizing)
    if sizing.qty < 1:
        return reject("too_thin", f"可吃量不足 1 件({sizing.absorbable_qty:.2f})")
    if sizing.capital_bound:
        hints.append("本金是瓶颈,市场还能吃更多")

    # ---- 置信度 ------------------------------------------------------------
    if edge:
        confidence = Confidence.LOW
    elif max_age <= config.freshness.high_confidence_hours:
        confidence = Confidence.HIGH
    else:
        confidence = Confidence.MEDIUM

    return Opportunity(
        item_id=record.item_id,
        item_name=catalog.name_of(record.item_id),
        city=record.city,
        quality=record.quality,
        buy_price=record.buy_price_max,
        sell_price=record.sell_price_min,
        my_bid=unit.my_bid,
        my_ask=unit.my_ask,
        cost_per_unit=unit.cost_per_unit,
        revenue_per_unit=unit.revenue_per_unit,
        profit_per_unit=unit.profit_per_unit,
        margin=unit.margin,
        daily_volume_qty=stats.daily_volume_qty,
        daily_volume_silver=stats.daily_volume_silver,
        avg_price_7d=stats.avg_price_7d,
        avg_price_30d=stats.avg_price_30d,
        absorbable_qty=sizing.absorbable_qty,
        qty=sizing.qty,
        capital_used=sizing.capital_used,
        daily_profit=sizing.daily_profit,
        daily_roi=sizing.daily_roi,
        capital_roi=sizing.capital_roi,
        buy_age_hours=buy_age,
        sell_age_hours=sell_age,
        data_age_hours=max_age,
        confidence=confidence,
        warnings=warnings,
        hints=hints,
    )
