from __future__ import annotations

from datetime import datetime, timezone

import pytest

from flipper.history import aggregate
from flipper.models import HistorySeries

NOW = datetime(2026, 9, 21, 6, 0, tzinfo=timezone.utc)


def series(points: list[tuple[str, int, int]]) -> HistorySeries:
    return HistorySeries(
        location="Lymhurst",
        item_id="T5_CLOTH",
        quality=1,
        data=[{"timestamp": ts, "item_count": qty, "avg_price": price} for ts, qty, price in points],
    )


def test_剔除未走完的当天():
    """当天的点量必然偏低、价格可能是单笔异常值,拿它算日均会系统性失真。"""
    s = series(
        [
            ("2026-09-19T00:00:00", 1000, 300),
            ("2026-09-20T00:00:00", 1000, 300),
            ("2026-09-21T00:00:00", 3, 9000),  # 今天,只有一笔离谱成交
        ]
    )
    stats = aggregate([s], NOW, baseline_days=7, history_days=30)[("T5_CLOTH", "Lymhurst")]
    assert stats.avg_price_7d == pytest.approx(300)
    assert stats.daily_volume_qty == pytest.approx(1000)


def test_均价按成交量加权():
    s = series(
        [
            ("2026-09-19T00:00:00", 9000, 100),
            ("2026-09-20T00:00:00", 1000, 1100),
        ]
    )
    stats = aggregate([s], NOW, baseline_days=7, history_days=30)[("T5_CLOTH", "Lymhurst")]
    # 算术平均会给出 600,把一个零星成交日和一个万笔成交日等权
    assert stats.avg_price_7d == pytest.approx(200)


def test_日均分母是有数据的天数而不是窗口长度():
    """AODP 缺失日几乎总是"那天没人上传",不是"那天没成交"。"""
    s = series([("2026-09-19T00:00:00", 1000, 300), ("2026-09-20T00:00:00", 2000, 300)])
    stats = aggregate([s], NOW, baseline_days=7, history_days=30)[("T5_CLOTH", "Lymhurst")]
    assert stats.days_with_data_7d == 2
    assert stats.daily_volume_qty == pytest.approx(1500)  # 不是 3000/7


def test_同一物品城市的多条series会合并():
    a = series([("2026-09-19T00:00:00", 1000, 300)])
    b = series([("2026-09-20T00:00:00", 1000, 300)])
    stats = aggregate([a, b], NOW, baseline_days=7, history_days=30)
    assert stats[("T5_CLOTH", "Lymhurst")].days_with_data_7d == 2


def test_7日和30日口径分开():
    s = series(
        [
            ("2026-09-01T00:00:00", 1000, 100),  # 只在 30 日窗口里
            ("2026-09-19T00:00:00", 1000, 300),
            ("2026-09-20T00:00:00", 1000, 300),
        ]
    )
    stats = aggregate([s], NOW, baseline_days=7, history_days=30)[("T5_CLOTH", "Lymhurst")]
    assert stats.avg_price_7d == pytest.approx(300)
    assert stats.avg_price_30d == pytest.approx(700 / 3)
