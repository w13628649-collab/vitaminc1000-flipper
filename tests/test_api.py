from __future__ import annotations

import time

from flipper.api import SlidingWindowLimiter, chunk_by_url_length


def test_分批不超出URL预算():
    ids = [f"T{i}_ARMOR_CLOTH_SET1" for i in range(200)]
    prefix, suffix, limit = 60, 80, 500
    batches = list(
        chunk_by_url_length(ids, prefix_len=prefix, suffix_len=suffix, max_url_length=limit)
    )
    assert sum(len(b) for b in batches) == len(ids)
    assert [i for b in batches for i in b] == ids  # 不丢不乱序
    for batch in batches:
        assert prefix + len(",".join(batch)) + suffix <= limit


def test_单个超长id仍单独成批而不是被丢掉():
    ids = ["X" * 400, "T4_CLOTH"]
    batches = list(chunk_by_url_length(ids, prefix_len=50, suffix_len=50, max_url_length=200))
    assert [i for b in batches for i in b] == ids


def test_物品少时只发一个请求():
    ids = [f"T{i}_CLOTH" for i in range(66)]
    batches = list(chunk_by_url_length(ids, prefix_len=60, suffix_len=120, max_url_length=3500))
    assert len(batches) == 1


def test_限流器在超额时阻塞():
    limiter = SlidingWindowLimiter(per_minute=1000, per_5min=2)
    start = time.monotonic()
    limiter.acquire()
    limiter.acquire()
    assert time.monotonic() - start < 0.1
    # 第三次必须等 5 分钟窗口滑动 —— 这里不真等,只确认它算出了等待
    limiter._limits = [(0.2, 1000), (0.3, 2)]
    limiter.acquire()
    assert time.monotonic() - start >= 0.25
