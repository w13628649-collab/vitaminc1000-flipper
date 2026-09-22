"""输出:终端表格 + CSV。

硬性要求:**不能只给价差,必须带 data_age_hours**。
AODP 是众共上传的数据,亚服覆盖率差,一条"12 小时前的完美价差"毫无意义。
用户要能自己判断这条值不值得信,所以新鲜度和置信度跟价格同等显眼。
"""

from __future__ import annotations

import csv
from pathlib import Path

from rich.console import Console
from rich.table import Table

from .config import Config
from .models import Confidence, Opportunity
from .scan import ScanResult

CSV_FIELDS = [
    "item_id",
    "item_name",
    "city",
    "buy_price",
    "sell_price",
    "my_bid",
    "my_ask",
    "margin_pct",
    "daily_volume_silver",
    "absorbable_qty",
    "qty",
    "capital_used",
    "daily_profit",
    "daily_roi_pct",
    "capital_roi_pct",
    "profit_per_unit",
    "avg_price_7d",
    "avg_price_30d",
    "buy_age_hours",
    "sell_age_hours",
    "data_age_hours",
    "confidence",
    "notes",
]

_CONFIDENCE_STYLE = {
    Confidence.HIGH: "bold green",
    Confidence.MEDIUM: "yellow",
    Confidence.LOW: "red",
}

REJECT_LABELS = {
    "one_sided": "单边数据",
    "no_timestamp": "无时间戳",
    "stale": "数据过期",
    "no_history": "无成交历史",
    "stale_history": "历史过期",
    "no_baseline": "无基准均价",
    "thin_history": "历史样本不足",
    "deviation": "偏离均价(troll)",
    "low_volume": "流水不足",
    "crossed_book": "交叉盘(快照不同步)",
    "unprofitable": "税后不赚钱",
    "implausible_margin": "利润率不真实(troll)",
    "too_thin": "可吃量 < 1 件",
}


def silver(value: float) -> str:
    """银币缩写。市场数字动辄百万,原始位数读不了。"""
    if abs(value) >= 1_000_000:
        return f"{value / 1_000_000:.2f}M"
    if abs(value) >= 1_000:
        return f"{value / 1_000:.1f}k"
    return f"{value:.0f}"


# 城市名占宽太多,表格里用缩写;CSV 里保留全名
CITY_SHORT = {
    "Thetford": "Thet",
    "Fort Sterling": "FtSt",
    "Lymhurst": "Lymh",
    "Martlock": "Mart",
    "Bridgewatch": "Bridge",
    "Caerleon": "Caer",
    "Brecilien": "Brec",
    "Black Market": "BM",
}


def render_table(result: ScanResult, config: Config, console: Console, limit: int = 25) -> None:
    table = Table(
        title=f"同城价差机会 — {config.server} — 按日化收益排序(本金 {silver(config.capital)} 银)",
        title_style="bold",
        pad_edge=False,
    )
    table.add_column("#", justify="right", style="dim", width=3)
    table.add_column("物品", overflow="fold")
    table.add_column("城", no_wrap=True)
    # 这两个才是要去游戏里执行的数字:挂买单挂多少、挂卖单挂多少
    table.add_column("挂买→挂卖", justify="right", style="cyan", no_wrap=True)
    table.add_column("毛利", justify="right", no_wrap=True)
    table.add_column("日流水", justify="right", no_wrap=True)
    table.add_column("可吃量", justify="right", no_wrap=True)
    table.add_column("占用", justify="right", no_wrap=True)
    table.add_column("日收益", justify="right", style="bold", no_wrap=True)
    table.add_column("龄/置信", justify="right", no_wrap=True)

    for index, opp in enumerate(result.opportunities[:limit], start=1):
        style = _CONFIDENCE_STYLE[opp.confidence]
        table.add_row(
            str(index),
            opp.item_name,
            CITY_SHORT.get(opp.city, opp.city),
            f"{opp.my_bid:,}→{opp.my_ask:,}",
            f"{opp.margin:.1%}",
            silver(opp.daily_volume_silver),
            f"{opp.qty:,}",
            silver(opp.capital_used),
            f"{silver(opp.daily_profit)} ({opp.capital_roi:.0%})",
            f"[{style}]{opp.data_age_hours:.1f}h {opp.confidence.value}[/]",
        )

    console.print(table)
    console.print(
        "[dim]日收益括号内是占总本金的回报率。毛利是单笔税后毛利率(同城一天一轮时"
        "即 daily_roi)。[/dim]"
    )
    if len(result.opportunities) > limit:
        console.print(f"[dim]另有 {len(result.opportunities) - limit} 条未显示,完整结果见 CSV[/dim]")


def render_summary(result: ScanResult, config: Config, console: Console) -> None:
    opportunities = result.opportunities
    console.print()
    console.print(f"[bold]扫描摘要[/bold]  {result.started_at.strftime('%Y-%m-%d %H:%M')} UTC")
    console.print(
        f"  物品 {len(result.item_ids)} 个 × 城市 {len(config.cities)} 个"
        f" → {result.price_rows} 条报价,{result.request_count} 次 API 请求"
    )
    if result.missing_item_ids:
        console.print(
            f"  [yellow]配置里 {len(result.missing_item_ids)} 个 ID 在 items.txt 中不存在:[/yellow] "
            + ", ".join(result.missing_item_ids[:8])
        )

    if opportunities:
        by_confidence = {level: 0 for level in Confidence}
        for opp in opportunities:
            by_confidence[opp.confidence] += 1
        console.print(
            f"  机会 {len(opportunities)} 条 — "
            f"[bold green]high {by_confidence[Confidence.HIGH]}[/] / "
            f"[yellow]medium {by_confidence[Confidence.MEDIUM]}[/] / "
            f"[red]low {by_confidence[Confidence.LOW]}[/]"
        )
        ages = [o.data_age_hours for o in opportunities]
        console.print(
            f"  数据新鲜度:最新 {min(ages):.1f}h / 中位 {sorted(ages)[len(ages) // 2]:.1f}h"
            f" / 最旧 {max(ages):.1f}h"
        )
    else:
        console.print("  [yellow]没有任何机会通过过滤。[/yellow]")

    if result.reject_counts:
        console.print("  [dim]过滤掉的候选(按原因):[/dim]")
        for reason, count in result.reject_counts.most_common():
            label = REJECT_LABELS.get(reason, reason)
            console.print(f"    [dim]{label:<22}{count:>6}[/dim]")

    _render_diagnosis(result, config, console)

    friction = config.economics.round_trip_friction
    buy_fee_note = "含挂买单 2.5%(保守,待游戏内确认)" if config.economics.buy_order_setup_fee else "不含挂买单手续费"
    console.print(
        f"  [dim]往返摩擦 {friction:.1%} —— {buy_fee_note};"
        f"价差需显著高于这个数才有意义[/dim]"
    )


def render_rejected(result: ScanResult, console: Console, reason: str | None, limit: int) -> None:
    rows = [r for r in result.rejected if reason is None or r.reason == reason]
    if not rows:
        console.print("[dim]没有匹配的被过滤记录[/dim]")
        return
    table = Table(title=f"被过滤的候选({len(rows)} 条)")
    table.add_column("物品")
    table.add_column("城市")
    table.add_column("原因")
    table.add_column("详情")
    for row in rows[:limit]:
        table.add_row(row.item_id, row.city, REJECT_LABELS.get(row.reason, row.reason), row.detail)
    console.print(table)
    if len(rows) > limit:
        console.print(f"[dim]另有 {len(rows) - limit} 条未显示[/dim]")


def write_csv(opportunities: list[Opportunity], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8-sig") as handle:
        writer = csv.DictWriter(handle, fieldnames=CSV_FIELDS)
        writer.writeheader()
        for opp in opportunities:
            writer.writerow(
                {
                    "item_id": opp.item_id,
                    "item_name": opp.item_name,
                    "city": opp.city,
                    "buy_price": opp.buy_price,
                    "sell_price": opp.sell_price,
                    "my_bid": opp.my_bid,
                    "my_ask": opp.my_ask,
                    "margin_pct": round(opp.margin * 100, 2),
                    "daily_volume_silver": round(opp.daily_volume_silver),
                    "absorbable_qty": round(opp.absorbable_qty, 1),
                    "qty": opp.qty,
                    "capital_used": round(opp.capital_used),
                    "daily_profit": round(opp.daily_profit),
                    "daily_roi_pct": round(opp.daily_roi * 100, 2),
                    "capital_roi_pct": round(opp.capital_roi * 100, 2),
                    "profit_per_unit": round(opp.profit_per_unit, 2),
                    "avg_price_7d": round(opp.avg_price_7d, 1),
                    "avg_price_30d": round(opp.avg_price_30d, 1),
                    "buy_age_hours": round(opp.buy_age_hours or 0, 2),
                    "sell_age_hours": round(opp.sell_age_hours or 0, 2),
                    "data_age_hours": round(opp.data_age_hours, 2),
                    "confidence": opp.confidence.value,
                    "notes": "; ".join(opp.notes),
                }
            )


def _render_diagnosis(result: ScanResult, config: Config, console: Console) -> None:
    """结果少的时候,说清楚是数据的问题还是阈值的问题。

    亚服的真瓶颈是众包数据覆盖率——必须有人在游戏里翻到那一页市场,价格才会上传。
    不解释清楚,用户会以为工具坏了,然后去放宽 troll 阈值,那正是最危险的动作。
    """
    counts = result.reject_counts
    total = sum(counts.values())
    if not total:
        return

    coverage_reasons = counts["one_sided"] + counts["stale"] + counts["no_timestamp"]
    hints: list[str] = []

    if coverage_reasons / total > 0.5:
        hints.append(
            f"{coverage_reasons}/{total} 条候选死在数据覆盖率上(单边/过期),不是价差不存在,"
            "而是没人上传。跑 `flipper check` 看各城覆盖率,或自己开 albiondata-client 抓包补数据。"
        )
    if counts["stale"] and not result.opportunities:
        hints.append(
            f"可以试 `--max-age {config.freshness.max_hours * 2:.0f}` 放宽新鲜度看看有什么,"
            "但那些价格实盘八成已经变了,只当参考。"
        )
    troll_blocked = counts["deviation"] + counts["implausible_margin"] + counts["crossed_book"]
    if troll_blocked:
        hints.append(
            f"{troll_blocked} 条被 troll 过滤拦下。这些正是天真计算器会报成天大机会的那些,"
            "用 `--show-rejected --rejected-reason deviation` 看具体是什么。"
        )

    for hint in hints:
        console.print(f"  [cyan]提示:[/cyan] {hint}")
