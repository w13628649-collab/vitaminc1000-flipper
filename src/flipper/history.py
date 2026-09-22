"""成交历史聚合:算出日均成交量和基准均价。

两个口径决策,会直接影响排序结果,写在这里备查:

1. **丢掉当天那个点。** time-scale=24 的最后一个点是还没走完的今天,
   量必然偏低,拿它算日均会系统性低估流动性。

2. **日均的分母是"窗口内有数据的天数",不是窗口长度。**
   AODP 是众包数据,某天缺失几乎总是"那天没人上传"而不是"那天没成交"。
   按窗口长度平均会把覆盖率问题误算成流动性问题。代价是样本少时噪声大,
   所以用 min_days_with_data 兜底。
"""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta

from .models import HistoryPoint, HistorySeries


@dataclass
class MarketStats:
    item_id: str
    city: str
    avg_price_7d: float
    avg_price_30d: float
    daily_volume_qty: float
    """7 日口径的日均成交件数。"""
    daily_volume_qty_30d: float
    daily_volume_silver: float
    """日均成交件数 × 7 日均价。这才是跟本金可比的量纲。"""
    days_with_data_7d: int
    days_with_data_30d: int
    last_point: datetime | None

    def history_age_days(self, now: datetime) -> float | None:
        if self.last_point is None:
            return None
        return (now - self.last_point).total_seconds() / 86400.0


def _window(points: list[HistoryPoint], now: datetime, days: int) -> list[HistoryPoint]:
    """取最近 days 个完整日。当天未走完,剔除。"""
    today = now.date()
    cutoff = now - timedelta(days=days)
    return [p for p in points if p.timestamp >= cutoff and p.timestamp.date() < today]


def _weighted_avg_price(points: list[HistoryPoint]) -> float:
    """按成交量加权的均价。简单算术平均会让一个零星成交日和一个万笔成交日等权。"""
    total_qty = sum(p.item_count for p in points)
    if total_qty <= 0:
        return 0.0
    return sum(p.avg_price * p.item_count for p in points) / total_qty


def aggregate(
    series_list: list[HistorySeries], now: datetime, *, baseline_days: int, history_days: int
) -> dict[tuple[str, str], MarketStats]:
    """按 (item_id, city) 聚合。AODP 同一组合可能返回多条 series(按质量拆),合并。"""
    merged: dict[tuple[str, str], list[HistoryPoint]] = defaultdict(list)
    for series in series_list:
        merged[(series.item_id, series.location)].extend(series.data)

    stats: dict[tuple[str, str], MarketStats] = {}
    for (item_id, city), points in merged.items():
        points = [p for p in points if p.timestamp is not None]
        if not points:
            continue
        points.sort(key=lambda p: p.timestamp)

        short = _window(points, now, baseline_days)
        long = _window(points, now, history_days)

        days_short = len({p.timestamp.date() for p in short})
        days_long = len({p.timestamp.date() for p in long})

        qty_short = sum(p.item_count for p in short) / days_short if days_short else 0.0
        qty_long = sum(p.item_count for p in long) / days_long if days_long else 0.0
        avg_short = _weighted_avg_price(short)
        avg_long = _weighted_avg_price(long)

        stats[(item_id, city)] = MarketStats(
            item_id=item_id,
            city=city,
            avg_price_7d=avg_short,
            avg_price_30d=avg_long,
            daily_volume_qty=qty_short,
            daily_volume_qty_30d=qty_long,
            daily_volume_silver=qty_short * avg_short,
            days_with_data_7d=days_short,
            days_with_data_30d=days_long,
            last_point=points[-1].timestamp,
        )
    return stats


def aggregate_by_quality(
    series_list: list[HistorySeries], now: datetime, *, baseline_days: int
) -> dict[tuple[str, str, int], MarketStats]:
    """按 (item_id, city, quality) 聚合。

    查价界面要按品质分开看 —— 卓越品质的成交量和普通品质差一个数量级,
    混在一起的均价对哪一档都不准。
    """
    merged: dict[tuple[str, str, int], list[HistoryPoint]] = defaultdict(list)
    for series in series_list:
        merged[(series.item_id, series.location, series.quality)].extend(series.data)

    stats: dict[tuple[str, str, int], MarketStats] = {}
    for (item_id, city, quality), points in merged.items():
        points = sorted((p for p in points if p.timestamp is not None), key=lambda p: p.timestamp)
        if not points:
            continue
        window = _window(points, now, baseline_days)
        days = len({p.timestamp.date() for p in window})
        qty = sum(p.item_count for p in window) / days if days else 0.0
        avg = _weighted_avg_price(window)
        stats[(item_id, city, quality)] = MarketStats(
            item_id=item_id,
            city=city,
            avg_price_7d=avg,
            avg_price_30d=avg,
            daily_volume_qty=qty,
            daily_volume_qty_30d=qty,
            daily_volume_silver=qty * avg,
            days_with_data_7d=days,
            days_with_data_30d=days,
            last_point=points[-1].timestamp,
        )
    return stats
