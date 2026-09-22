"""SQLite 快照存储。

这个阶段不需要时序数据库,一张扫描记录表就够。存历史快照的意义在于:
跑一段时间后能回答"这条机会是一直存在还是昙花一现" —— 只在某次扫描里
出现过一次的机会,多半是数据抖动而不是真实价差。
"""

from __future__ import annotations

import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

from .models import Opportunity

SCHEMA = """
CREATE TABLE IF NOT EXISTS scan_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at  TEXT NOT NULL,
    server      TEXT NOT NULL,
    cities      TEXT NOT NULL,
    item_count  INTEGER NOT NULL,
    request_count INTEGER NOT NULL,
    opportunity_count INTEGER NOT NULL,
    config_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS opportunities (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES scan_runs(id),
    item_id     TEXT NOT NULL,
    item_name   TEXT NOT NULL,
    city        TEXT NOT NULL,
    quality     INTEGER NOT NULL,
    buy_price   INTEGER NOT NULL,
    sell_price  INTEGER NOT NULL,
    margin      REAL NOT NULL,
    daily_volume_silver REAL NOT NULL,
    absorbable_qty REAL NOT NULL,
    qty         INTEGER NOT NULL,
    capital_used REAL NOT NULL,
    daily_profit REAL NOT NULL,
    daily_roi   REAL NOT NULL,
    capital_roi REAL NOT NULL,
    data_age_hours REAL NOT NULL,
    confidence  TEXT NOT NULL,
    notes       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_opp_run ON opportunities(run_id);
CREATE INDEX IF NOT EXISTS idx_opp_item ON opportunities(item_id, city);
"""


class Storage:
    def __init__(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        self.conn = sqlite3.connect(path)
        self.conn.row_factory = sqlite3.Row
        self.conn.executescript(SCHEMA)
        self.conn.commit()

    def __enter__(self) -> Storage:
        return self

    def __exit__(self, *exc: object) -> None:
        self.conn.close()

    def record_scan(
        self,
        *,
        server: str,
        cities: list[str],
        item_count: int,
        request_count: int,
        opportunities: list[Opportunity],
        config_json: str,
        started_at: datetime | None = None,
    ) -> int:
        started = (started_at or datetime.now(timezone.utc)).isoformat()
        cursor = self.conn.execute(
            "INSERT INTO scan_runs (started_at, server, cities, item_count, request_count,"
            " opportunity_count, config_json) VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                started,
                server,
                ",".join(cities),
                item_count,
                request_count,
                len(opportunities),
                config_json,
            ),
        )
        run_id = int(cursor.lastrowid or 0)
        self.conn.executemany(
            "INSERT INTO opportunities (run_id, item_id, item_name, city, quality, buy_price,"
            " sell_price, margin, daily_volume_silver, absorbable_qty, qty, capital_used,"
            " daily_profit, daily_roi, capital_roi, data_age_hours, confidence, notes)"
            " VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
            [
                (
                    run_id,
                    o.item_id,
                    o.item_name,
                    o.city,
                    o.quality,
                    o.buy_price,
                    o.sell_price,
                    o.margin,
                    o.daily_volume_silver,
                    o.absorbable_qty,
                    o.qty,
                    o.capital_used,
                    o.daily_profit,
                    o.daily_roi,
                    o.capital_roi,
                    o.data_age_hours,
                    o.confidence.value,
                    json.dumps(o.notes, ensure_ascii=False),
                )
                for o in opportunities
            ],
        )
        self.conn.commit()
        return run_id

    def recurrence(self, item_id: str, city: str, last_n_runs: int = 10) -> int:
        """这条机会在最近 N 次扫描里出现过几次。只出现一次的多半是数据抖动。"""
        rows = self.conn.execute(
            "SELECT COUNT(DISTINCT run_id) FROM opportunities WHERE item_id = ? AND city = ?"
            " AND run_id > (SELECT COALESCE(MAX(id), 0) - ? FROM scan_runs)",
            (item_id, city, last_n_runs),
        ).fetchone()
        return int(rows[0]) if rows else 0
