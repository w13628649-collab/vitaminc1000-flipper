from __future__ import annotations

import pytest

from flipper.config import EconomicsConfig, SizingConfig
from flipper.economics import size_position, unit_economics


def test_摩擦成本随挂买单手续费开关变化():
    assert EconomicsConfig(buy_order_setup_fee=True).round_trip_friction == pytest.approx(0.09)
    assert EconomicsConfig(buy_order_setup_fee=False).round_trip_friction == pytest.approx(0.065)
    # 非会员市场税 8%
    assert EconomicsConfig(market_tax=0.08).round_trip_friction == pytest.approx(0.13)


def test_价差低于摩擦时利润为负():
    cfg = EconomicsConfig()
    unit = unit_economics(buy_price=1000, sell_price=1080, cfg=cfg)  # 8% < 9%
    assert unit.profit_per_unit < 0


def test_挂单要压过现价():
    cfg = EconomicsConfig(outbid_silver=1, undercut_silver=1)
    unit = unit_economics(buy_price=1000, sell_price=1200, cfg=cfg)
    assert (unit.my_bid, unit.my_ask) == (1001, 1199)


def test_不计挂买单手续费时成本更低():
    with_fee = unit_economics(1000, 1200, EconomicsConfig(buy_order_setup_fee=True))
    without = unit_economics(1000, 1200, EconomicsConfig(buy_order_setup_fee=False))
    assert without.cost_per_unit < with_fee.cost_per_unit
    assert without.profit_per_unit > with_fee.profit_per_unit


def test_市场深度是瓶颈时按可吃量截断():
    unit = unit_economics(1000, 1200, EconomicsConfig())
    # 日成交 100 件 → 20% = 20 件,远小于 1000 万本金买得起的量
    sizing = size_position(unit, daily_volume_qty=100, capital=10_000_000, cfg=SizingConfig())
    assert sizing.qty == 20
    assert sizing.capital_bound is False


def test_本金是瓶颈时按本金截断():
    unit = unit_economics(1000, 1200, EconomicsConfig())
    sizing = size_position(unit, daily_volume_qty=1_000_000, capital=10_000_000, cfg=SizingConfig())
    assert sizing.qty == int(10_000_000 // unit.cost_per_unit)
    assert sizing.capital_bound is True
    assert sizing.capital_used <= 10_000_000


def test_本金回报率才区分得开薄利多销和厚利少量():
    """SPEC 的核心主张:利润率 30% 但一天只能做 3 件,不如 5% 但能做 2000 件。"""
    cfg = SizingConfig()
    厚利 = unit_economics(1000, 1450, EconomicsConfig())  # 高毛利
    薄利 = unit_economics(1000, 1120, EconomicsConfig())  # 低毛利

    厚利仓位 = size_position(厚利, daily_volume_qty=15, capital=10_000_000, cfg=cfg)
    薄利仓位 = size_position(薄利, daily_volume_qty=50_000, capital=10_000_000, cfg=cfg)

    assert 厚利.margin > 薄利.margin
    # 但日化绝对收益反过来 —— 这就是为什么排序主键是 daily_profit
    assert 薄利仓位.daily_profit > 厚利仓位.daily_profit
    assert 薄利仓位.capital_roi > 厚利仓位.capital_roi


def test_同城一天一轮时日化收益率等于单笔毛利率():
    unit = unit_economics(1000, 1200, EconomicsConfig())
    sizing = size_position(unit, daily_volume_qty=1000, capital=10_000_000, cfg=SizingConfig())
    assert sizing.daily_roi == pytest.approx(unit.margin)
