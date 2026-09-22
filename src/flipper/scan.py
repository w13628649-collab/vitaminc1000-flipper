"""扫描编排:拉价 → 拉历史 → 过滤 → 算账 → 排序。"""

from __future__ import annotations

import logging
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone

from statistics import median

from .api import AodpClient
from .config import Config
from .filters import evaluate
from .history import aggregate
from .catalog import ItemCatalog
from .models import Opportunity, PriceRecord, RejectedRow

log = logging.getLogger(__name__)


@dataclass
class CityCoverage:
    """某城有多少物品有报价、有多新。亚服的真瓶颈是覆盖率,不是算法。"""

    city: str
    with_data: int
    within_2h: int
    within_6h: int
    within_24h: int
    within_threshold: int
    """落在当前 max_freshness_hours 内的报价点数 —— 界面上那根条按这个画。"""
    median_age_hours: float | None


def coverage_by_city(
    prices: list[PriceRecord], cities: list[str], now: datetime, threshold_hours: float = 6.0
) -> list[CityCoverage]:
    ages: dict[str, list[float]] = {city: [] for city in cities}
    for record in prices:
        bucket = ages.setdefault(record.city, [])
        for field in ("sell_price_min_date", "buy_price_max_date"):
            age = record.age_hours(field, now)
            if age is not None:
                bucket.append(age)
    return [
        CityCoverage(
            city=city,
            with_data=len(values),
            within_2h=sum(1 for a in values if a < 2),
            within_6h=sum(1 for a in values if a < 6),
            within_24h=sum(1 for a in values if a < 24),
            within_threshold=sum(1 for a in values if a < threshold_hours),
            median_age_hours=median(values) if values else None,
        )
        for city, values in ages.items()
    ]


@dataclass
class ScanResult:
    started_at: datetime
    item_ids: list[str]
    missing_item_ids: list[str]
    opportunities: list[Opportunity]
    rejected: list[RejectedRow]
    request_count: int
    price_rows: int
    coverage: list[CityCoverage] = field(default_factory=list)
    reject_counts: Counter[str] = field(default_factory=Counter)

    @property
    def elapsed_note(self) -> str:
        return f"{self.request_count} 次请求,{self.price_rows} 条报价"


def run_scan(
    config: Config,
    catalog: ItemCatalog,
    *,
    now: datetime | None = None,
    transport: object | None = None,
) -> ScanResult:
    now = now or datetime.now(timezone.utc)
    item_ids, missing = catalog.resolve(config.items.patterns, config.items.exclude)
    if missing:
        log.warning("配置里有 %d 个 ID 在 items.txt 中不存在: %s", len(missing), ", ".join(missing[:10]))
    if not item_ids:
        raise ValueError("展开后没有任何有效物品 ID,检查 items.patterns")

    with AodpClient(config.base_url, config.api, transport=transport) as client:  # type: ignore[arg-type]
        log.info("拉取 %d 个物品 × %d 城市的当前挂单价", len(item_ids), len(config.cities))
        prices = client.fetch_prices(item_ids, config.cities, config.qualities)

        log.info("拉取 %d 天成交历史", config.sizing.history_days)
        history = client.fetch_history(
            item_ids,
            config.cities,
            config.qualities,
            days=config.sizing.history_days,
        )
        request_count = client.request_count

    stats = aggregate(
        history,
        now,
        baseline_days=config.sizing.baseline_days,
        history_days=config.sizing.history_days,
    )

    opportunities: list[Opportunity] = []
    rejected: list[RejectedRow] = []
    for record in prices:
        outcome = evaluate(record, stats.get((record.item_id, record.city)), config, catalog, now)
        if isinstance(outcome, Opportunity):
            opportunities.append(outcome)
        else:
            rejected.append(outcome)

    # 排序主键是日化**绝对**收益,不是利润率。
    # 利润率 30% 但一天只能做 3 件,不如利润率 5% 但一天能做 2000 件。
    opportunities.sort(key=lambda o: o.daily_profit, reverse=True)

    return ScanResult(
        started_at=now,
        item_ids=item_ids,
        missing_item_ids=missing,
        opportunities=opportunities,
        rejected=rejected,
        request_count=request_count,
        price_rows=len(prices),
        coverage=coverage_by_city(prices, config.cities, now, config.freshness.max_hours),
        reject_counts=Counter(r.reason for r in rejected),
    )
