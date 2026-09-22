"""本地 web 界面。

只绑 127.0.0.1:扫描结果是本机的 SQLite 和实时 AODP 调用,没有对外服务的理由。
这也正好落在 SBI 对第三方工具的判断标准里——数据在网页上呈现,不是游戏内覆盖层。
"""

from __future__ import annotations

import asyncio
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException
from fastapi.responses import FileResponse, Response
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

from .api import AodpClient
from .catalog import ItemCatalog, load_icon
from .config import Config
from .economics import unit_economics
from .history import aggregate_by_quality
from .ranking import ALL_CITIES, coverage as history_coverage, rank as rank_rows, store_history
from .catalog import ItemCatalog
from .models import Confidence
from .report import REJECT_LABELS
from .scan import ScanResult, run_scan
from .storage import Storage

STATIC_DIR = Path(__file__).parent / "static"
_ICON_ID_OK = set("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_@")


class PullRequest(BaseModel):
    """抓历史入库。范围大的时候会慢,所以做成显式动作而不是自动触发。"""

    category: str = ""
    subcategory: str = ""
    days: int = 30
    qualities: list[int] = [1]


class ScanRequest(BaseModel):
    """临时覆盖配置里的阈值,不落盘。调阈值看结果怎么变,是这个界面的主要用法。"""

    max_age_hours: float | None = None
    capital: int | None = None
    cities: list[str] | None = None
    buy_order_setup_fee: bool | None = None
    absorb_ratio: float | None = None
    min_daily_volume_silver: int | None = None


def _serialize(result: ScanResult, config: Config) -> dict[str, Any]:
    ages = [o.data_age_hours for o in result.opportunities]
    by_confidence = {level.value: 0 for level in Confidence}
    for opp in result.opportunities:
        by_confidence[opp.confidence.value] += 1

    return {
        "scanned_at": result.started_at.isoformat(),
        "opportunities": [
            {**o.model_dump(mode="json"), "notes": o.notes} for o in result.opportunities
        ],
        "coverage": [
            {
                "city": c.city,
                "with_data": c.with_data,
                "within_2h": c.within_2h,
                "within_6h": c.within_6h,
                "within_24h": c.within_24h,
                "within_threshold": c.within_threshold,
                "median_age_hours": c.median_age_hours,
            }
            for c in result.coverage
        ],
        "rejected": [
            {"reason": reason, "label": REJECT_LABELS.get(reason, reason), "count": count}
            for reason, count in result.reject_counts.most_common()
        ],
        "missing_item_ids": result.missing_item_ids,
        "summary": {
            "item_count": len(result.item_ids),
            "city_count": len(config.cities),
            "price_rows": result.price_rows,
            "request_count": result.request_count,
            "rejected_total": sum(result.reject_counts.values()),
            "by_confidence": by_confidence,
            "age_min": min(ages) if ages else None,
            "age_median": sorted(ages)[len(ages) // 2] if ages else None,
            "age_max": max(ages) if ages else None,
        },
        "effective": {
            "server": config.server,
            "cities": config.cities,
            "capital": config.capital,
            "max_age_hours": config.freshness.max_hours,
            "high_confidence_hours": config.freshness.high_confidence_hours,
            "friction": config.economics.round_trip_friction,
            "max_margin": config.filters.max_margin,
            "buy_order_setup_fee": config.economics.buy_order_setup_fee,
            "market_tax": config.economics.market_tax,
            "absorb_ratio": config.sizing.absorb_ratio,
            "min_daily_volume_silver": config.filters.min_daily_volume_silver,
            "deviation": [config.filters.deviation_min, config.filters.deviation_max],
        },
    }


def create_app(config_path: Path) -> FastAPI:
    app = FastAPI(title="albion-flipper", docs_url=None, redoc_url=None)
    # AODP 有限流,同时只允许跑一次扫描
    scan_lock = asyncio.Lock()
    cache: dict[str, Any] = {}

    def _config() -> Config:
        # 每次重读,改 config.yaml 不用重启
        return Config.load(config_path)

    @app.get("/")
    async def index() -> FileResponse:
        return FileResponse(STATIC_DIR / "index.html")

    @app.get("/api/state")
    async def state() -> dict[str, Any]:
        """首屏:有缓存给缓存,没有就只给配置,让界面先画出来。"""
        config = _config()
        if cache.get("payload"):
            return {"ready": True, **cache["payload"]}
        return {
            "ready": False,
            "effective": _serialize(
                ScanResult(
                    started_at=datetime.now(timezone.utc),
                    item_ids=[],
                    missing_item_ids=[],
                    opportunities=[],
                    rejected=[],
                    request_count=0,
                    price_rows=0,
                ),
                config,
            )["effective"],
        }

    @app.post("/api/scan")
    async def scan(request: ScanRequest) -> dict[str, Any]:
        config = _config()
        if request.max_age_hours is not None:
            config.freshness.max_hours = request.max_age_hours
        if request.capital is not None:
            config.capital = request.capital
        if request.cities:
            config.cities = request.cities
        if request.buy_order_setup_fee is not None:
            config.economics.buy_order_setup_fee = request.buy_order_setup_fee
        if request.absorb_ratio is not None:
            config.sizing.absorb_ratio = request.absorb_ratio
        if request.min_daily_volume_silver is not None:
            config.filters.min_daily_volume_silver = request.min_daily_volume_silver

        async with scan_lock:
            catalog = ItemCatalog.open(config.db_path)
            result = await asyncio.to_thread(run_scan, config, catalog)

            with Storage(config.db_path) as storage:
                storage.record_scan(
                    server=config.server,
                    cities=config.cities,
                    item_count=len(result.item_ids),
                    request_count=result.request_count,
                    opportunities=result.opportunities,
                    config_json=config.model_dump_json(),
                    started_at=result.started_at,
                )

            payload = _serialize(result, config)
            cache["payload"] = payload
            return {"ready": True, **payload}

    QUALITY_NAMES = {1: "普通", 2: "优秀", 3: "杰出", 4: "卓越", 5: "大师"}

    def _item_row(item) -> dict[str, Any]:
        return {
            "item_id": item.item_id,
            "name_zh": item.name_zh,
            "name_en": item.name_en,
            "tier": item.tier,
            "enchantment": item.enchantment,
            "max_quality": item.max_quality,
            "category": item.category,
            "subcategory": item.subcategory,
            "family": item.family,
            "display_name": item.display_name,
        }

    @app.get("/api/icon/{item_id}.png")
    async def icon(item_id: str) -> Response:
        if not item_id or set(item_id) - _ICON_ID_OK:
            raise HTTPException(status_code=400, detail="非法物品 ID")
        png = await asyncio.to_thread(load_icon, _config().db_path, item_id)
        if png is None:
            raise HTTPException(status_code=404, detail="没有这个图标")
        return Response(
            png, media_type="image/png",
            headers={"Cache-Control": "public, max-age=604800"},
        )

    @app.get("/api/categories")
    async def categories() -> dict[str, Any]:
        """三级菜单的完整树:大类 → 子类 → 物品族。一次给全,悬停展开不用等网络。"""
        catalog = ItemCatalog.open(_config().db_path)
        return {
            "tree": catalog.menu_tree(),
            "tiers": list(range(1, 9)),
            "enchantments": [0, 1, 2, 3, 4],
            "qualities": [{"value": q, "label": QUALITY_NAMES[q]} for q in (1, 2, 3, 4, 5)],
        }

    @app.get("/api/items/browse")
    async def item_browse(
        category: str = "",
        subcategory: str = "",
        family: str = "",
        tier: int = 0,
        enchantment: int = -1,
        limit: int = 300,
    ) -> list[dict[str, Any]]:
        catalog = ItemCatalog.open(_config().db_path)
        return [
            _item_row(item)
            for item in catalog.browse(
                category=category,
                subcategory=subcategory,
                family=family,
                tier=tier,
                enchantment=enchantment,
                limit=limit,
            )
        ]

    @app.get("/api/items/search")
    async def item_search(q: str, limit: int = 30) -> list[dict[str, Any]]:
        catalog = ItemCatalog.open(_config().db_path)
        return [_item_row(item) for item in catalog.search(q, limit)]

    @app.get("/api/prices/{item_id}")
    async def prices(item_id: str) -> dict[str, Any]:
        """一个物品在所有城市、所有品质下的当前挂单价。

        跟游戏内市场不同的是,游戏里一次只能看当前所在城市的一个品质,
        这里把整张表摊开。
        """
        config = _config()
        catalog = ItemCatalog.open(config.db_path)
        item = catalog.get(item_id)
        # 倒爷绕不开黑市,查价时默认带上
        cities = list(config.cities)
        if "Black Market" not in cities:
            cities.append("Black Market")
        qualities = [1, 2, 3, 4, 5]
        now = datetime.now(timezone.utc)

        def fetch() -> tuple[list, list]:
            with AodpClient(config.base_url, config.api) as client:
                return (
                    client.fetch_prices([item_id], cities, qualities),
                    client.fetch_history(
                        [item_id], cities, qualities, days=config.sizing.baseline_days
                    ),
                )

        # 和扫描共用一把锁 —— 两边都打同一个限流配额
        async with scan_lock:
            price_rows, history_rows = await asyncio.to_thread(fetch)

        stats = aggregate_by_quality(history_rows, now, baseline_days=config.sizing.baseline_days)
        cells = []
        for row in price_rows:
            stat = stats.get((row.item_id, row.city, row.quality))
            # 同城一轮(挂买单收货 → 挂卖单出货)的税后毛利率。
            # 光给两个挂单价,人得自己心算才知道这价差够不够吃掉 9% 的摩擦。
            spread = None
            if row.sell_price_min > 0 and row.buy_price_max > 0:
                spread = unit_economics(
                    row.buy_price_max, row.sell_price_min, config.economics
                ).margin
            cells.append(
                {
                    "city": row.city,
                    "quality": row.quality,
                    "sell_price_min": row.sell_price_min or None,
                    "sell_age_hours": row.age_hours("sell_price_min_date", now),
                    "buy_price_max": row.buy_price_max or None,
                    "buy_age_hours": row.age_hours("buy_price_max_date", now),
                    "spread_pct": round(spread * 100, 1) if spread is not None else None,
                    "avg_price_7d": round(stat.avg_price_7d) if stat else None,
                    "daily_volume_qty": round(stat.daily_volume_qty) if stat else None,
                }
            )

        return {
            "item": _item_row(item) if item else {"item_id": item_id, "display_name": item_id,
                                                  "name_en": "", "max_quality": 5},
            "fetched_at": now.isoformat(),
            "cities": cities,
            "qualities": [{"value": q, "label": QUALITY_NAMES[q]} for q in qualities],
            "cells": cells,
            "max_age_hours": config.freshness.max_hours,
            "high_confidence_hours": config.freshness.high_confidence_hours,
            "friction": config.economics.round_trip_friction,
            "max_margin": config.filters.max_margin,
        }

    @app.get("/api/history/coverage")
    async def history_cov() -> dict[str, Any]:
        config = _config()
        stat = await asyncio.to_thread(history_coverage, config.db_path)
        return {
            "rows": stat.get("rows") or 0,
            "items": stat.get("items") or 0,
            "days": stat.get("days") or 0,
            "first_day": stat.get("first_day"),
            "last_day": stat.get("last_day"),
        }

    @app.post("/api/history/pull")
    async def history_pull(request: PullRequest) -> dict[str, Any]:
        config = _config()
        catalog = ItemCatalog.open(config.db_path)
        if request.category or request.subcategory:
            item_ids = [
                i.item_id
                for i in catalog.browse(
                    category=request.category, subcategory=request.subcategory, limit=10**6
                )
            ]
        else:
            item_ids, _ = catalog.resolve(config.items.patterns, config.items.exclude)
        if not item_ids:
            raise HTTPException(status_code=400, detail="没有匹配的物品")

        cities = list(config.cities)
        if "Black Market" not in cities:
            cities.append("Black Market")

        def pull() -> tuple[int, int]:
            with AodpClient(config.base_url, config.api) as client:
                series = client.fetch_history(
                    item_ids, cities, request.qualities, days=request.days
                )
                return store_history(config.db_path, series), client.request_count

        async with scan_lock:
            written, requests = await asyncio.to_thread(pull)

        stat = await asyncio.to_thread(history_coverage, config.db_path)
        return {
            "written": written,
            "requests": requests,
            "item_count": len(item_ids),
            "coverage": {k: stat.get(k) for k in ("rows", "items", "days", "first_day", "last_day")},
        }

    @app.get("/api/ranking")
    async def ranking(
        window: int = 7,
        city: str = "all",
        quality: int = 1,
        category: str = "",
        subcategory: str = "",
        sort: str = "daily_silver",
        order: str = "desc",
        limit: int = 60,
    ) -> dict[str, Any]:
        config = _config()
        catalog = ItemCatalog.open(config.db_path)
        rows = await asyncio.to_thread(
            rank_rows,
            config.db_path,
            catalog,
            window_days=window,
            city=ALL_CITIES if city in ("all", "全服") else city,
            quality=quality,
            category=category,
            subcategory=subcategory,
            limit=limit,
            sort_by=sort,
            descending=order != "asc",
        )
        stat = await asyncio.to_thread(history_coverage, config.db_path)
        return {
            "rows": [
                {
                    "item_id": r.item_id,
                    "item_name": r.item_name,
                    "city": r.city,
                    "quality": r.quality,
                    "daily_qty": round(r.daily_qty),
                    "daily_silver": round(r.daily_silver),
                    "total_qty": r.total_qty,
                    "avg_price": round(r.avg_price),
                    "price_min": r.price_min,
                    "price_max": r.price_max,
                    "price_median": round(r.price_median),
                    "volatility": round(r.volatility * 100, 1),
                    "trend_pct": round(r.trend_pct, 1),
                    "trend_fit": round(r.trend_fit, 2),
                    "trend": r.trend,
                    "series": r.series,
                    "days_with_data": r.days_with_data,
                    "last_day": r.last_day,
                }
                for r in rows
            ],
            "window": window,
            "sort": sort,
            "order": order,
            "city": city,
            "quality": quality,
            "cities": list(config.cities) + ["Black Market"],
            "coverage": {k: stat.get(k) for k in ("rows", "items", "days", "first_day", "last_day")},
        }

    @app.get("/api/runs")
    async def runs(limit: int = 30) -> list[dict[str, Any]]:
        config = _config()
        with Storage(config.db_path) as storage:
            rows = storage.conn.execute(
                "SELECT id, started_at, item_count, opportunity_count FROM scan_runs"
                " ORDER BY id DESC LIMIT ?",
                (limit,),
            ).fetchall()
            return [dict(row) for row in rows]

    if STATIC_DIR.exists():
        app.mount("/static", StaticFiles(directory=STATIC_DIR), name="static")
    return app


def app_from_env() -> FastAPI:
    """给 uvicorn --reload 用的工厂。"""
    return create_app(Path(os.environ.get("FLIPPER_CONFIG", "config.yaml")))
