"""Troll 过滤的回归测试。

这些用例就是"买了就套死"的那些挂单。任何一条挂掉,主榜上就会出现假机会。
"""

from __future__ import annotations

import pytest

from flipper.filters import evaluate
from flipper.models import Confidence, Opportunity, RejectedRow

from conftest import make_price, make_stats


def reason(record, stats, config, catalog, now) -> str:
    outcome = evaluate(record, stats, config, catalog, now)
    assert isinstance(outcome, RejectedRow), f"本该被拒,却通过了:{outcome}"
    return outcome.reason


def test_正常机会通过并给出高置信度(config, catalog, now):
    outcome = evaluate(make_price(), make_stats(), config, catalog, now)
    assert isinstance(outcome, Opportunity)
    assert outcome.confidence is Confidence.HIGH
    assert outcome.daily_profit > 0
    # 挂单要压过现价才排得到队首
    assert outcome.my_bid == 1001
    assert outcome.my_ask == 1199


def test_troll_高价卖单被偏离度拦下(config, catalog, now):
    # 1 件货挂 20 倍价 —— 天真计算器会把它报成天大的机会
    record = make_price(sell=22_000)
    assert reason(record, make_stats(), config, catalog, now) == "deviation"


def test_troll_低价买单被偏离度拦下(config, catalog, now):
    record = make_price(buy=10, sell=1200)
    assert reason(record, make_stats(), config, catalog, now) == "deviation"


def test_两侧同时是troll时毛利率上限兜底(config, catalog, now):
    # 买价 0.45x、卖价 2.4x,各自都在 [0.4, 2.5] 内"合规",
    # 组合起来却是 5 倍价差。这是 SPEC 四层过滤盖不住的缺口。
    record = make_price(buy=495, sell=2640)
    assert reason(record, make_stats(), config, catalog, now) == "implausible_margin"


def test_数据过期被拦下(config, catalog, now):
    record = make_price(sell_age_h=7.0)
    assert reason(record, make_stats(), config, catalog, now) == "stale"


def test_单边数据不进主榜(config, catalog, now):
    # 价格 0 是 AODP 的"无数据"哨兵,不是"白送"
    record = make_price(buy=0)
    assert reason(record, make_stats(), config, catalog, now) == "one_sided"


def test_流水不足被拦下(config, catalog, now):
    stats = make_stats(daily_qty=100)  # 100 × 1100 = 11 万 < 50 万
    assert reason(make_price(), stats, config, catalog, now) == "low_volume"


def test_交叉盘被拦下(config, catalog, now):
    # 买价 >= 卖价 在真实市场不可能持续存在,说明两侧快照来自不同时间
    record = make_price(buy=1300, sell=1200)
    assert reason(record, make_stats(), config, catalog, now) == "crossed_book"


def test_价差吃不过摩擦时判为不赚钱(config, catalog, now):
    record = make_price(buy=1000, sell=1050)  # 5% 价差 < 9% 摩擦
    assert reason(record, make_stats(), config, catalog, now) == "unprofitable"


def test_历史样本太少被拦下(config, catalog, now):
    stats = make_stats(days=2)
    assert reason(make_price(), stats, config, catalog, now) == "thin_history"


def test_历史过期被拦下(config, catalog, now):
    stats = make_stats(history_age_days=5)
    assert reason(make_price(), stats, config, catalog, now) == "stale_history"


def test_可吃量不足一件被拦下(config, catalog, now):
    # 流水够高但单价极高 → 20% 的日成交量不足 1 件
    stats = make_stats(avg_7d=2_000_000, daily_qty=1)
    record = make_price(buy=1_800_000, sell=2_200_000)
    assert reason(record, stats, config, catalog, now) == "too_thin"


def test_数据偏旧时降为medium(config, catalog, now):
    outcome = evaluate(make_price(sell_age_h=4.0), make_stats(), config, catalog, now)
    assert isinstance(outcome, Opportunity)
    assert outcome.confidence is Confidence.MEDIUM


def test_接近偏离度边缘时降为low(config, catalog, now):
    # 卖价 2.35x,距上限 2.5 只有 0.15,在 15% 边缘带内。
    # 这个价差同时会撞上毛利率上限,这里只想测边缘降级,所以把那层放开。
    config.filters.max_margin = 5.0
    outcome = evaluate(make_price(buy=1000, sell=2585), make_stats(), config, catalog, now)
    assert isinstance(outcome, Opportunity)
    assert outcome.confidence is Confidence.LOW
    assert any("边缘" in note for note in outcome.notes)


def test_流水接近下限时降为low(config, catalog, now):
    stats = make_stats(daily_qty=500)  # 500 × 1100 = 55 万,在 50 万~62.5 万边缘带
    outcome = evaluate(make_price(), stats, config, catalog, now)
    assert isinstance(outcome, Opportunity)
    assert outcome.confidence is Confidence.LOW


def test_关闭交叉盘拦截时降级而非丢弃(config, catalog, now):
    config.filters.reject_crossed_book = False
    outcome = evaluate(make_price(buy=1190, sell=1200), make_stats(), config, catalog, now)
    # 价差吃不过摩擦,所以仍然会被判不赚钱 —— 但不是被 crossed_book 丢的
    assert isinstance(outcome, RejectedRow)
    assert outcome.reason == "unprofitable"


@pytest.mark.parametrize("missing_side", ["buy", "sell"])
def test_任一侧时间戳缺失都不进主榜(config, catalog, now, missing_side):
    record = make_price()
    setattr(record, f"{missing_side}_price_{'max' if missing_side == 'buy' else 'min'}_date", None)
    assert reason(record, make_stats(), config, catalog, now) == "no_timestamp"
