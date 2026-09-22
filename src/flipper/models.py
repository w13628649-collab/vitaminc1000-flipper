"""AODP 响应模型 + 扫描结果模型。

AODP 的时间戳不带时区,实际是 UTC。空数据用 price=0 / date=0001-01-01 表示,
这两个哨兵必须显式处理,否则 0 价会被当成"史上最低价"。
"""

from __future__ import annotations

from datetime import datetime, timezone
from enum import Enum

from pydantic import BaseModel, field_validator

# C# DateTime.Ticks 纪元偏移。REST v2 返回 ISO 字符串,NATS 流返回 ticks。
TICKS_EPOCH_OFFSET = 621_355_968_000_000_000
TICKS_PER_SECOND = 10_000_000

_EMPTY_SENTINEL = datetime(1, 1, 1, tzinfo=timezone.utc)


def parse_timestamp(value: str | int | float | datetime | None) -> datetime | None:
    """统一时间戳解析。返回 None 表示"没有数据",而不是"很久以前"。"""
    if value is None:
        return None
    if isinstance(value, datetime):
        parsed = value
    elif isinstance(value, (int, float)):
        # C# ticks
        parsed = datetime.fromtimestamp(
            (value - TICKS_EPOCH_OFFSET) / TICKS_PER_SECOND, tz=timezone.utc
        )
    else:
        text = value.strip()
        if not text:
            return None
        parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    else:
        parsed = parsed.astimezone(timezone.utc)
    return None if parsed <= _EMPTY_SENTINEL else parsed


class PriceRecord(BaseModel):
    """/api/v2/stats/prices 的一行:某物品在某城市的当前挂单价。

    注意:这个端点不返回挂单数量。"最低卖价 1000"可能只对应 1 件货,
    这是 troll 过滤必须存在的根本原因。
    """

    item_id: str
    city: str
    quality: int
    sell_price_min: int = 0
    sell_price_min_date: datetime | None = None
    sell_price_max: int = 0
    sell_price_max_date: datetime | None = None
    buy_price_min: int = 0
    buy_price_min_date: datetime | None = None
    buy_price_max: int = 0
    buy_price_max_date: datetime | None = None

    @field_validator(
        "sell_price_min_date",
        "sell_price_max_date",
        "buy_price_min_date",
        "buy_price_max_date",
        mode="before",
    )
    @classmethod
    def _parse_dates(cls, value: object) -> datetime | None:
        return parse_timestamp(value)  # type: ignore[arg-type]

    def age_hours(self, field: str, now: datetime) -> float | None:
        stamp: datetime | None = getattr(self, field)
        if stamp is None:
            return None
        return (now - stamp).total_seconds() / 3600.0


class HistoryPoint(BaseModel):
    item_count: int
    avg_price: int
    timestamp: datetime

    @field_validator("timestamp", mode="before")
    @classmethod
    def _parse_ts(cls, value: object) -> datetime | None:
        return parse_timestamp(value)  # type: ignore[arg-type]


class HistorySeries(BaseModel):
    """/api/v2/stats/history 的一组:某物品在某城市的逐日成交。

    item_count 只统计卖单成交,拿不到买单成交量 —— 实际流动性比这个数字高,
    所以用它算可吃量是偏保守的一侧。
    """

    location: str
    item_id: str
    quality: int
    data: list[HistoryPoint] = []


class Confidence(str, Enum):
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"


class Opportunity(BaseModel):
    """一条同城价差机会。"""

    item_id: str
    item_name: str
    city: str
    quality: int

    buy_price: int
    """现有最高买单价(市场快照)。"""
    sell_price: int
    """现有最低卖单价(市场快照)。"""
    my_bid: int
    """我实际要挂的买单价 = buy_price + outbid。"""
    my_ask: int
    """我实际要挂的卖单价 = sell_price - undercut。"""

    cost_per_unit: float
    revenue_per_unit: float
    profit_per_unit: float
    margin: float

    daily_volume_qty: float
    daily_volume_silver: float
    avg_price_7d: float
    avg_price_30d: float

    absorbable_qty: float
    qty: int
    capital_used: float
    daily_profit: float
    daily_roi: float
    capital_roi: float
    """daily_profit / 总本金。真正衡量"这条机会对我 1000 万有多大意义"的指标。"""

    buy_age_hours: float | None
    sell_age_hours: float | None
    data_age_hours: float
    confidence: Confidence
    warnings: list[str] = []
    """贴近过滤阈值的项。这条之所以还在榜上,是因为差一点点才被拦掉。"""
    hints: list[str] = []
    """中性信息,不影响可信度。"""

    @property
    def notes(self) -> list[str]:
        return self.warnings + self.hints

    @property
    def max_age_display(self) -> float:
        return self.data_age_hours


class RejectedRow(BaseModel):
    """被过滤掉的候选。留着是为了能回答"为什么我没看到某物品"。"""

    item_id: str
    city: str
    reason: str
    detail: str = ""
