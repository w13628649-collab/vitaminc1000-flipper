"""交易经济学:一单赚多少,一天能做多少。

核心立场:排序主键是**日化绝对收益**,不是单笔利润率。
利润率 30% 但一天只能做 3 件,不如利润率 5% 但一天能做 2000 件。
"""

from __future__ import annotations

import math
from dataclasses import dataclass

from .config import EconomicsConfig, SizingConfig


@dataclass(frozen=True)
class UnitEconomics:
    my_bid: int
    my_ask: int
    cost_per_unit: float
    revenue_per_unit: float
    profit_per_unit: float
    margin: float


def unit_economics(buy_price: int, sell_price: int, cfg: EconomicsConfig) -> UnitEconomics:
    """模式 A:挂买单收货 → 挂卖单出货。

    挂在现价上只能跟人并排排队,得等前面的人先成交。要真的排到队首,
    买单得高 outbid_silver,卖单得低 undercut_silver —— 这一步的成本很小,
    但不算进去就会系统性高估所有机会。
    """
    my_bid = buy_price + cfg.outbid_silver
    my_ask = sell_price - cfg.undercut_silver

    buy_fee_multiplier = 1 + cfg.setup_fee if cfg.buy_order_setup_fee else 1.0
    cost = my_bid * buy_fee_multiplier
    revenue = my_ask * (1 - cfg.market_tax - cfg.setup_fee)
    profit = revenue - cost

    return UnitEconomics(
        my_bid=my_bid,
        my_ask=my_ask,
        cost_per_unit=cost,
        revenue_per_unit=revenue,
        profit_per_unit=profit,
        margin=profit / cost if cost > 0 else 0.0,
    )


@dataclass(frozen=True)
class Sizing:
    absorbable_qty: float
    qty: int
    capital_used: float
    daily_profit: float
    daily_roi: float
    capital_roi: float
    capital_bound: bool
    """True = 本金是瓶颈(市场还能吃更多);False = 市场深度是瓶颈。"""


def size_position(
    unit: UnitEconomics,
    daily_volume_qty: float,
    capital: int,
    cfg: SizingConfig,
) -> Sizing:
    """算"我能吃下多少而不砸价",再用本金截断。

    absorb_ratio 默认 0.20 是拍脑袋的保守值。第二阶段用实盘成交率记录
    反过来校准它 —— 那才是这个项目相对现成工具的长期价值所在。
    """
    absorbable = daily_volume_qty * cfg.absorb_ratio
    max_by_capital = capital / unit.cost_per_unit if unit.cost_per_unit > 0 else 0.0
    qty = int(math.floor(min(absorbable, max_by_capital)))

    capital_used = qty * unit.cost_per_unit
    daily_profit = unit.profit_per_unit * qty

    return Sizing(
        absorbable_qty=absorbable,
        qty=qty,
        capital_used=capital_used,
        daily_profit=daily_profit,
        # 同城假设一天一轮,所以 daily_roi 数值上就等于单笔 margin。
        daily_roi=unit.margin,
        # 这条才真正回答"这个机会对我这 1000 万有多大意义":
        # margin 再高,只吃得下 3 件也就是三瓜两枣。
        capital_roi=daily_profit / capital if capital > 0 else 0.0,
        capital_bound=max_by_capital < absorbable,
    )
