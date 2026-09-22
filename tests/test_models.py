from __future__ import annotations

from datetime import datetime, timezone

from flipper.models import parse_timestamp


def test_iso字符串按utc解析():
    assert parse_timestamp("2026-09-20T15:30:00") == datetime(
        2026, 9, 20, 15, 30, tzinfo=timezone.utc
    )


def test_空数据哨兵解析为None():
    """AODP 用 0001-01-01 表示"没有数据"。当成"很久以前"会让新鲜度判断失效。"""
    assert parse_timestamp("0001-01-01T00:00:00") is None
    assert parse_timestamp("") is None
    assert parse_timestamp(None) is None


def test_csharp_ticks():
    """REST v2 返回 ISO 字符串,NATS 流返回 ticks,两种都要认。"""
    ticks = 621_355_968_000_000_000 + 10_000_000 * 3600
    assert parse_timestamp(ticks) == datetime(1970, 1, 1, 1, 0, tzinfo=timezone.utc)


def test_带时区的输入统一转utc():
    assert parse_timestamp("2026-09-20T23:30:00+08:00") == datetime(
        2026, 9, 20, 15, 30, tzinfo=timezone.utc
    )
