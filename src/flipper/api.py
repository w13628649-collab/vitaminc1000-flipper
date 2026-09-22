"""AODP REST 客户端。

三件事必须做对,否则要么被限流拉黑,要么把 URL 撑爆:
1. 限流是两条线 —— 180/分钟 和 300/5 分钟。后者等效 60/分钟,才是真正的约束。
2. URL 上限 4096 字符,item id 必须批量塞进去并按实际编码长度分批。
3. 带 gzip,响应体不小。
"""

from __future__ import annotations

import logging
import time
from collections import deque
from collections.abc import Iterable, Iterator
from urllib.parse import quote

import httpx

from .config import ApiConfig
from .models import HistorySeries, PriceRecord

log = logging.getLogger(__name__)


class SlidingWindowLimiter:
    """双窗口令牌桶。两条限制都满足才放行。"""

    def __init__(self, per_minute: int, per_5min: int) -> None:
        self._limits = [(60.0, per_minute), (300.0, per_5min)]
        self._hits: deque[float] = deque()

    def acquire(self) -> None:
        while True:
            now = time.monotonic()
            # 只需保留最长窗口内的记录
            longest = max(window for window, _ in self._limits)
            while self._hits and now - self._hits[0] > longest:
                self._hits.popleft()

            wait = 0.0
            for window, limit in self._limits:
                count = sum(1 for hit in self._hits if now - hit <= window)
                if count >= limit:
                    # 等到窗口内最早那一次请求滑出去
                    oldest = next(hit for hit in self._hits if now - hit <= window)
                    wait = max(wait, window - (now - oldest) + 0.05)
            if wait <= 0:
                self._hits.append(now)
                return
            log.debug("限流等待 %.1fs", wait)
            time.sleep(wait)


def chunk_by_url_length(
    item_ids: Iterable[str], *, prefix_len: int, suffix_len: int, max_url_length: int
) -> Iterator[list[str]]:
    """按编码后的实际 URL 长度分批。

    逗号分隔的 id 段是唯一可变部分;单个 id 就超长时仍然单独成批
    (让服务端去拒绝,好过在这里静默丢掉一个物品)。
    """
    budget = max_url_length - prefix_len - suffix_len
    batch: list[str] = []
    length = 0
    for item_id in item_ids:
        encoded_len = len(quote(item_id, safe=""))
        extra = encoded_len + (1 if batch else 0)
        if batch and length + extra > budget:
            yield batch
            batch, length = [], 0
            extra = encoded_len
        batch.append(item_id)
        length += extra
    if batch:
        yield batch


class AodpClient:
    def __init__(
        self,
        base_url: str,
        config: ApiConfig,
        *,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.config = config
        self._limiter = SlidingWindowLimiter(config.rate_per_minute, config.rate_per_5min)
        self._client = httpx.Client(
            transport=transport,
            timeout=config.timeout_seconds,
            headers={
                "Accept-Encoding": "gzip",
                "User-Agent": "albion-flipper/0.1 (personal market scanner)",
            },
            follow_redirects=True,
        )
        self.request_count = 0

    def __enter__(self) -> AodpClient:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()

    def close(self) -> None:
        self._client.close()

    def _get(self, path: str, params: dict[str, str]) -> list[dict]:
        url = f"{self.base_url}{path}"
        delay = 1.0
        for attempt in range(self.config.max_retries):
            self._limiter.acquire()
            self.request_count += 1
            try:
                resp = self._client.get(url, params=params)
            except httpx.TransportError as exc:
                if attempt == self.config.max_retries - 1:
                    raise
                log.warning("请求失败 %s,%.1fs 后重试: %s", url, delay, exc)
                time.sleep(delay)
                delay *= 2
                continue

            if resp.status_code == 429:
                retry_after = float(resp.headers.get("Retry-After", delay))
                log.warning("被限流 429,等待 %.1fs", retry_after)
                time.sleep(retry_after)
                delay = max(delay * 2, retry_after)
                continue
            if resp.status_code >= 500:
                if attempt == self.config.max_retries - 1:
                    resp.raise_for_status()
                log.warning("服务端 %s,%.1fs 后重试", resp.status_code, delay)
                time.sleep(delay)
                delay *= 2
                continue
            resp.raise_for_status()
            return resp.json()
        raise RuntimeError(f"重试 {self.config.max_retries} 次仍失败: {url}")

    def _batched(
        self, endpoint: str, item_ids: list[str], params: dict[str, str]
    ) -> Iterator[tuple[list[str], list[dict]]]:
        prefix = f"{self.base_url}/api/v2/stats/{endpoint}/"
        suffix = ".json?" + "&".join(
            f"{key}={quote(value, safe=',')}" for key, value in sorted(params.items())
        )
        for batch in chunk_by_url_length(
            item_ids,
            prefix_len=len(prefix),
            suffix_len=len(suffix),
            max_url_length=self.config.max_url_length,
        ):
            path = f"/api/v2/stats/{endpoint}/{','.join(batch)}.json"
            yield batch, self._get(path, params)

    def fetch_prices(
        self, item_ids: list[str], cities: list[str], qualities: list[int]
    ) -> list[PriceRecord]:
        params = {
            "locations": ",".join(cities),
            "qualities": ",".join(str(q) for q in qualities),
        }
        records: list[PriceRecord] = []
        for _batch, payload in self._batched("prices", item_ids, params):
            records.extend(PriceRecord.model_validate(row) for row in payload)
        return records

    def fetch_history(
        self,
        item_ids: list[str],
        cities: list[str],
        qualities: list[int],
        *,
        days: int,
        time_scale: int = 24,
    ) -> list[HistorySeries]:
        params = {
            "locations": ",".join(cities),
            "qualities": ",".join(str(q) for q in qualities),
            "time-scale": str(time_scale),
            "date": _days_ago(days),
        }
        series: list[HistorySeries] = []
        for _batch, payload in self._batched("history", item_ids, params):
            series.extend(HistorySeries.model_validate(row) for row in payload)
        return series


def _days_ago(days: int) -> str:
    from datetime import datetime, timedelta, timezone

    return (datetime.now(timezone.utc) - timedelta(days=days)).strftime("%m-%d-%Y")
