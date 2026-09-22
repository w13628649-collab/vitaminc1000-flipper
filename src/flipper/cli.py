"""命令行入口。"""

from __future__ import annotations

import logging
from dataclasses import replace
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import typer
from rich.console import Console
from rich.logging import RichHandler

from .config import Config
from .catalog import ItemCatalog
from .models import Confidence
from .report import render_rejected, render_summary, render_table, silver, write_csv
from .scan import run_scan
from .storage import Storage

app = typer.Typer(
    add_completion=False,
    help="Albion 亚服同城价差扫描器 —— 按日化收益排序,带 troll 过滤。",
)
items_app = typer.Typer(help="物品目录维护(来自 ao-bin-dumps)。")
app.add_typer(items_app, name="items")

console = Console()

_CONFIDENCE_ORDER = {Confidence.LOW: 0, Confidence.MEDIUM: 1, Confidence.HIGH: 2}

TREND_LABEL = {"up": "↗ 涨", "down": "↘ 跌", "choppy": "↕ 震荡", "flat": "→ 平"}



def _setup_logging(verbose: bool) -> None:
    logging.basicConfig(
        level=logging.DEBUG if verbose else logging.INFO,
        format="%(message)s",
        handlers=[RichHandler(console=console, show_path=False, show_time=False)],
    )
    # httpx 每次请求都 INFO 一行完整 URL,几十个 item id 会把终端刷爆
    logging.getLogger("httpx").setLevel(logging.WARNING)


def _load(config_path: Path) -> Config:
    if not config_path.exists():
        console.print(f"[red]找不到配置文件 {config_path}[/red]")
        raise typer.Exit(1)
    return Config.load(config_path)


@app.command()
def scan(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c", help="配置文件路径"),
    limit: int = typer.Option(25, "--limit", "-n", help="终端表格显示条数"),
    csv_path: Optional[Path] = typer.Option(None, "--csv", help="CSV 输出路径(默认 out/ 带时间戳)"),
    min_confidence: str = typer.Option(
        "low", "--min-confidence", help="只显示不低于该置信度的机会:low/medium/high"
    ),
    city: Optional[list[str]] = typer.Option(None, "--city", help="只扫这些城市,可重复"),
    capital: Optional[int] = typer.Option(None, "--capital", help="覆盖配置里的本金"),
    max_age: Optional[float] = typer.Option(None, "--max-age", help="覆盖新鲜度上限(小时)"),
    show_rejected: bool = typer.Option(False, "--show-rejected", help="列出被过滤的候选"),
    rejected_reason: Optional[str] = typer.Option(
        None, "--rejected-reason", help="只看某一类过滤原因,如 deviation / stale"
    ),
    no_db: bool = typer.Option(False, "--no-db", help="不写 SQLite"),
    verbose: bool = typer.Option(False, "--verbose", "-v"),
) -> None:
    """扫描同城价差机会(模式 A:挂买单收货 → 挂卖单出货)。"""
    _setup_logging(verbose)
    config = _load(config_path)

    if city:
        unknown = [c for c in city if c not in config.cities]
        if unknown:
            console.print(f"[yellow]提醒:{unknown} 不在配置的 cities 里,仍会照常查询[/yellow]")
        config.cities = list(city)
    if capital is not None:
        config.capital = capital
    if max_age is not None:
        config.freshness.max_hours = max_age

    catalog = ItemCatalog.open(config.db_path)
    result = run_scan(config, catalog)

    threshold = _CONFIDENCE_ORDER[Confidence(min_confidence)]
    shown = [o for o in result.opportunities if _CONFIDENCE_ORDER[o.confidence] >= threshold]
    if len(shown) != len(result.opportunities):
        console.print(
            f"[dim]置信度过滤:{len(result.opportunities)} → {len(shown)} 条"
            f"(min-confidence={min_confidence})[/dim]"
        )

    if shown:
        render_table(replace(result, opportunities=shown), config, console, limit)
    render_summary(result, config, console)

    if show_rejected or rejected_reason:
        render_rejected(result, console, rejected_reason, limit)

    destination = csv_path or Path("out") / f"scan-{result.started_at:%Y%m%d-%H%M%S}.csv"
    write_csv(shown, destination)
    console.print(f"\n[green]CSV 已写入[/green] {destination}")

    if not no_db:
        with Storage(config.db_path) as storage:
            run_id = storage.record_scan(
                server=config.server,
                cities=config.cities,
                item_count=len(result.item_ids),
                request_count=result.request_count,
                opportunities=result.opportunities,
                config_json=config.model_dump_json(),
                started_at=result.started_at,
            )
        console.print(f"[dim]已记录到 SQLite,run_id={run_id}[/dim]")


@items_app.command("sync")
def items_sync(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
) -> None:
    """从 ao-bin-dumps 刷新物品目录缓存。"""
    _setup_logging(False)
    config = _load(config_path)
    catalog = ItemCatalog.sync(config.db_path)
    console.print(f"[green]已更新[/green] {len(catalog)} 个物品 → {config.db_path}")


@items_app.command("list")
def items_list(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
) -> None:
    """展开配置里的 patterns,看看到底会扫哪些物品。"""
    _setup_logging(False)
    config = _load(config_path)
    catalog = ItemCatalog.open(config.db_path)
    resolved, missing = catalog.resolve(config.items.patterns, config.items.exclude)
    for item_id in resolved:
        console.print(f"  {item_id:<36}{catalog.name_of(item_id)}")
    console.print(f"\n[bold]{len(resolved)}[/bold] 个物品将被扫描")
    if missing:
        console.print(f"[red]{len(missing)} 个 ID 在目录中不存在:[/red] {', '.join(missing)}")


@app.command()
def check(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
) -> None:
    """只测数据覆盖率:每个城市有多少物品的报价是新鲜的。

    亚服的真瓶颈是众包数据覆盖率,不是算法。先跑这个心里有数。
    """
    _setup_logging(False)
    config = _load(config_path)
    catalog = ItemCatalog.open(config.db_path)
    item_ids, _ = catalog.resolve(config.items.patterns, config.items.exclude)

    from rich.table import Table

    from .api import AodpClient
    from .scan import coverage_by_city

    now = datetime.now(timezone.utc)
    with AodpClient(config.base_url, config.api) as client:
        prices = client.fetch_prices(item_ids, config.cities, config.qualities)

    table = Table(title=f"{config.server} 数据覆盖率 — {len(item_ids)} 个物品")
    table.add_column("城市")
    table.add_column("有数据", justify="right")
    table.add_column("< 2h", justify="right")
    table.add_column("< 6h", justify="right")
    table.add_column("< 24h", justify="right")
    table.add_column("中位数据龄", justify="right")
    for row in coverage_by_city(prices, config.cities, now):
        if not row.with_data:
            table.add_row(row.city, "0", "-", "-", "-", "-")
            continue
        table.add_row(
            row.city,
            str(row.with_data),
            str(row.within_2h),
            str(row.within_6h),
            str(row.within_24h),
            f"{row.median_age_hours:.1f}h",
        )
    console.print(table)


@app.command("history")
def history_pull(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
    category: str = typer.Option("", "--category", help="只抓某个大类,如 crafting / weapons"),
    subcategory: str = typer.Option("", "--subcategory", help="只抓某个子类"),
    days: int = typer.Option(30, "--days", help="往回抓多少天"),
    qualities: str = typer.Option("1", "--qualities", help="逗号分隔,如 1,2,3"),
    verbose: bool = typer.Option(False, "--verbose", "-v"),
) -> None:
    """抓成交历史入库,供排行榜用。

    AODP 的 history 只给最近一段,自己存就能越攒越长 —— 跑上几个月,
    手里就有一份上游给不了的长历史。重复抓同一天会覆盖,不会重复累加。
    """
    from .api import AodpClient
    from .ranking import coverage, store_history

    _setup_logging(verbose)
    config = _load(config_path)
    catalog = ItemCatalog.open(config.db_path)

    if category or subcategory:
        item_ids = [
            i.item_id
            for i in catalog.browse(category=category, subcategory=subcategory, limit=10**6)
        ]
        scope = f"{category or '全部'}/{subcategory or '全部'}"
    else:
        item_ids, _ = catalog.resolve(config.items.patterns, config.items.exclude)
        scope = "config.yaml 里监控的物品"

    if not item_ids:
        console.print("[red]没有匹配的物品[/red]")
        raise typer.Exit(1)

    cities = list(config.cities)
    if "Black Market" not in cities:
        cities.append("Black Market")
    quality_list = [int(q) for q in qualities.split(",") if q.strip()]

    console.print(f"抓取 {scope}:{len(item_ids)} 个物品 × {len(cities)} 城 × 品质 {quality_list}")
    with AodpClient(config.base_url, config.api) as client:
        series = client.fetch_history(item_ids, cities, quality_list, days=days)
        requests = client.request_count

    written = store_history(config.db_path, series)
    stat = coverage(config.db_path)
    console.print(
        f"[green]写入 {written} 条日线[/green]({requests} 次 API 请求)"
    )
    console.print(
        f"  库里现有 {stat['rows']} 行 / {stat['items']} 个物品 / "
        f"{stat['days']} 天({stat['first_day']} 至 {stat['last_day']})"
    )


@app.command()
def rank(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
    window: int = typer.Option(7, "--window", "-w", help="统计窗口(天)"),
    city: str = typer.Option("all", "--city", help="城市名,或 all 看全服合计"),
    quality: int = typer.Option(1, "--quality", "-q"),
    category: str = typer.Option("", "--category"),
    subcategory: str = typer.Option("", "--subcategory"),
    sort_by: str = typer.Option(
        "daily_silver", "--sort",
        help="daily_silver / daily_qty / volatility / avg_price / trend_pct",
    ),
    ascending: bool = typer.Option(False, "--asc", help="改成升序"),
    limit: int = typer.Option(30, "--limit", "-n"),
) -> None:
    """销量排行榜。读库,不打 API —— 先跑 `flipper history` 把数据抓进来。"""
    from rich.table import Table

    from .ranking import ALL_CITIES, coverage, rank as rank_rows

    _setup_logging(False)
    config = _load(config_path)
    catalog = ItemCatalog.open(config.db_path)

    stat = coverage(config.db_path)
    if not stat.get("rows"):
        console.print("[yellow]库里还没有成交历史。先跑 `flipper history` 抓一次。[/yellow]")
        raise typer.Exit(1)

    rows = rank_rows(
        config.db_path,
        catalog,
        window_days=window,
        city=ALL_CITIES if city in ("all", "全服") else city,
        quality=quality,
        category=category,
        subcategory=subcategory,
        limit=limit,
        sort_by=sort_by,
        descending=not ascending,
    )
    if not rows:
        console.print("[yellow]窗口内没有数据。换个窗口或先抓历史。[/yellow]")
        raise typer.Exit(1)

    scope = "全服合计" if city in ("all", "全服") else city
    table = Table(title=f"销量排行 — 最近 {window} 天 — {scope} — 品质 {quality}")
    table.add_column("#", justify="right", style="dim", width=3)
    table.add_column("物品")
    if city not in ("all", "全服"):
        table.add_column("城市")
    table.add_column("日均件数", justify="right")
    table.add_column("日均流水", justify="right", style="bold")
    table.add_column("均价", justify="right")
    table.add_column("最低", justify="right")
    table.add_column("中位", justify="right")
    table.add_column("最高", justify="right")
    table.add_column("波动", justify="right")
    table.add_column("趋势", justify="right")
    table.add_column("天数", justify="right", style="dim")

    for index, row in enumerate(rows, start=1):
        cells = [str(index), row.item_name]
        if city not in ("all", "全服"):
            cells.append(row.city)
        cells += [
            f"{row.daily_qty:,.0f}",
            silver(row.daily_silver),
            f"{row.avg_price:,.0f}",
            f"{row.price_min:,}",
            f"{row.price_median:,.0f}",
            f"{row.price_max:,}",
            f"{row.volatility:.0%}",
            f"{TREND_LABEL[row.trend]} {row.trend_pct:+.0f}%",
            str(row.days_with_data),
        ]
        table.add_row(*cells)
    console.print(table)
    console.print(
        "[dim]item_count 只统计卖单成交,买单那边拿不到 —— 这里的销量是偏低的一侧。"
        f"库里现有 {stat['days']} 天历史({stat['first_day']} 至 {stat['last_day']})。[/dim]"
    )


@app.command()
def serve(
    config_path: Path = typer.Option(Path("config.yaml"), "--config", "-c"),
    host: str = typer.Option("127.0.0.1", "--host", help="默认只绑本机;从别的机器访问要写 0.0.0.0"),
    port: int = typer.Option(8420, "--port", "-p"),
    reload: bool = typer.Option(False, "--reload", help="改代码自动重载(开发用)"),
    open_browser: bool = typer.Option(True, "--open/--no-open", help="启动后自动打开浏览器"),
) -> None:
    """起本地 web 界面。"""
    import os

    import uvicorn

    from .web import create_app

    _setup_logging(False)
    _load(config_path)  # 先验一遍配置,别等浏览器打开才报错
    # reload 模式下 uvicorn 在子进程里重建 app,拿不到这里的闭包,只能走环境变量
    os.environ["FLIPPER_CONFIG"] = str(config_path.resolve())

    if open_browser:
        import threading
        import webbrowser

        # uvicorn 还要一会儿才 listen,早开浏览器会撞上 connection refused
        threading.Timer(
            1.8, lambda: webbrowser.open(f"http://127.0.0.1:{port}")
        ).start()

    console.print(f"[green]界面已启动[/green] http://127.0.0.1:{port}")
    if host in ("0.0.0.0", "::"):
        console.print(f"[green]同网段也可访问[/green] http://{_lan_ip()}:{port}")
        console.print("[dim]没有任何鉴权,同网段的人都能打开并触发扫描[/dim]")
    else:
        console.print(
            f"[dim]只绑了 {host}。要从别的机器打开,用 --host 0.0.0.0 重启[/dim]"
        )
    console.print("[dim]关掉这个窗口就停止服务[/dim]")
    try:
        uvicorn.run(
            create_app(config_path) if not reload else "flipper.web:app_from_env",
            host=host,
            port=port,
            reload=reload,
            factory=reload,
            log_level="warning",
        )
    except OSError as exc:
        console.print(f"[red]端口 {port} 起不来:[/red] {exc}")
        console.print(f"[yellow]换个端口试试:flipper serve --port {port + 1}[/yellow]")
        raise typer.Exit(1) from exc


def _lan_ip() -> str:
    """取本机在局域网里的地址。连一个不可达的地址只为让内核选出口网卡,不发包。"""
    import socket

    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        try:
            sock.connect(("10.255.255.255", 1))
            return str(sock.getsockname()[0])
        except OSError:
            return "127.0.0.1"


if __name__ == "__main__":
    app()
