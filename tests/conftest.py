from __future__ import annotations

from pathlib import Path
from datetime import datetime, timedelta, timezone

import pytest

from flipper.config import Config
from flipper.history import MarketStats
from flipper.catalog import Item, ItemCatalog
from flipper.models import PriceRecord

NOW = datetime(2026, 9, 21, 12, 0, tzinfo=timezone.utc)


@pytest.fixture
def now() -> datetime:
    return NOW


@pytest.fixture
def catalog() -> ItemCatalog:
    return ItemCatalog(
        Path("/nonexistent.db"),
        {
            i.item_id: i
            for i in (
                Item("T5_CLOTH", "精布", "Ornate Cloth"),
                Item("T5_WOOD", "杉木", "Cedar Logs"),
                Item("T4_CLOTH", "细布", "Fine Cloth"),
            )
        },
        [],
    )


@pytest.fixture
def config() -> Config:
    return Config(
        cities=["Lymhurst"],
        capital=10_000_000,
        items={"patterns": ["T5_CLOTH"]},
    )


def make_price(
    *,
    item_id: str = "T5_CLOTH",
    city: str = "Lymhurst",
    buy: int = 1000,
    sell: int = 1200,
    buy_age_h: float = 1.0,
    sell_age_h: float = 1.0,
) -> PriceRecord:
    return PriceRecord(
        item_id=item_id,
        city=city,
        quality=1,
        sell_price_min=sell,
        sell_price_min_date=NOW - timedelta(hours=sell_age_h),
        buy_price_max=buy,
        buy_price_max_date=NOW - timedelta(hours=buy_age_h),
    )


def make_stats(
    *,
    item_id: str = "T5_CLOTH",
    city: str = "Lymhurst",
    avg_7d: float = 1100.0,
    daily_qty: float = 5000.0,
    days: int = 7,
    history_age_days: float = 0.5,
) -> MarketStats:
    return MarketStats(
        item_id=item_id,
        city=city,
        avg_price_7d=avg_7d,
        avg_price_30d=avg_7d,
        daily_volume_qty=daily_qty,
        daily_volume_qty_30d=daily_qty,
        daily_volume_silver=daily_qty * avg_7d,
        days_with_data_7d=days,
        days_with_data_30d=days,
        last_point=NOW - timedelta(days=history_age_days),
    )
