# SPEC:Albion 价差扫描器(倒爷工具)

## 目标

在亚服(East)找出**能真正赚到钱**的买卖机会,按日化收益排序。
不是找"理论价差最大",而是找"我 1000 万本金能吃下、且真的能成交"的机会。

## 两种模式

### 模式 A:同城价差(优先实现)

同一城市内,买单最高价和卖单最低价之间的差。
挂买单收货 → 挂卖单出货。无运输,周转最快,现金流最健康。

### 模式 B:跨城搬运

A 城卖单最低价买入 → 运到 B 城 → 挂卖单。
利润大但钱锁在路上,第二阶段再做。

---

## 输入

```yaml
server: east
cities: [Thetford, Fort Sterling, Lymhurst, Martlock, Bridgewatch, Caerleon]
capital: 10_000_000          # 银币
max_freshness_hours: 6       # 超过这个时间的数据丢弃
min_daily_volume_silver: 500_000
items: # 从 items.json 筛选,首批建议见文末
```

## 处理流程

### 1. 批量拉价格

```
GET https://east.albion-online-data.com/api/v2/stats/prices/{items}.json
    ?locations={cities}&qualities=1
```

- **必须批量**:一次请求塞满 item id,受 4096 字符 URL 上限约束,自动分批
- 限流 180/分钟、300/5 分钟,加令牌桶
- 请求头带 `Accept-Encoding: gzip`

### 2. 拉历史成交量

```
GET /api/v2/stats/history/{items}.json?locations={cities}&time-scale=24
```

取最近 7 天和 30 天:
- `item_count` → 日均成交量
- `avg_price` → 用于 troll 过滤的基准

时间戳是 C# ticks,转换:`(ticks - 621355968000000000) / 10000000`

### 3. Troll 过滤(必须做,不可省略)

**背景:AODP 的 prices 端点不返回挂单数量**,只有价格。
市场上大量恶意挂单——1 件货、价格是正常值几十倍的卖单,或极低的买单,
专门污染数据。天真的计算器会把这些显示成天大的机会,买了就套死。

四层过滤,全部通过才进入候选:

| 层 | 规则 |
|---|---|
| 新鲜度 | `sell_price_min_date` / `buy_price_max_date` 距今 < `max_freshness_hours` |
| 偏离度 | 价格与该物品 7 日均价的比值在 `[0.4, 2.5]` 区间外 → 丢弃 |
| 成交量 | `日均 item_count × avg_price < min_daily_volume_silver` → 丢弃 |
| 双边确认 | 买单价和卖单价必须都新鲜。只有单边数据 → 标记为低置信度,不进主榜 |

阈值全部做成配置项,方便后续按实盘反馈调。

### 4. 利润计算

```python
MARKET_TAX = 0.04     # 有 premium
SETUP_FEE  = 0.025

# 模式 A:挂买单收货,挂卖单出货
cost     = buy_price * (1 + SETUP_FEE)
revenue  = sell_price * (1 - MARKET_TAX - SETUP_FEE)
profit_per_unit = revenue - cost
margin   = profit_per_unit / cost
```

> 挂买单是否收 SETUP_FEE 待游戏内确认。先按收费算(保守),
> 确认后改成配置项。

### 5. 排序:日化收益,不是利润率

这是整个工具的核心,别退化成"按利润率排序"。

```python
# 我能吃下多少而不砸价:保守取日成交量的 20%
absorbable_qty = daily_volume_qty * 0.20

# 本金约束
max_qty_by_capital = capital / cost

qty = min(absorbable_qty, max_qty_by_capital)

# 假设一天能完成一轮(同城)。跨城按运输时间折算
daily_profit = profit_per_unit * qty
daily_roi    = daily_profit / (cost * qty)
```

**按 `daily_profit` 排序**(绝对日收益),同时展示 `daily_roi`。

理由:利润率 30% 但一天只能做 3 件,不如利润率 5% 但一天能做 2000 件。

## 输出

CSV + 终端表格,每行:

```
item_id | item_name | city | buy_price | sell_price | margin% |
daily_volume_silver | absorbable_qty | daily_profit | daily_roi% |
data_age_hours | confidence
```

`confidence` 分三档:
- `high` — 双边数据都在 2 小时内
- `medium` — 在 6 小时内
- `low` — 单边数据,或接近过滤阈值边缘

**不要只输出结论,必须带 `data_age_hours`。** 用户要能自己判断这条值不值得信。

## 第二阶段:成交率记录

这是现成工具给不了的东西,也是这个项目长期价值所在。

手动记录(先 CLI 输入,后面再自动化):

```
挂单时间 | 物品 | 城市 | 方向(买/卖) | 挂单价 | 数量 | 成交时间 | 实际成交量
```

跑一个月后能算出:
- 每个物品的**真实成交率**和**平均成交时长**
- 扫描器预测的"可吃量"跟实际差多少 → 反过来校准 `0.20` 那个系数

## 技术选型

- Python,`httpx` + `pydantic`
- 存 SQLite 就够,这个阶段不需要 ClickHouse
- CLI 用 `typer`,表格用 `rich`
- 定时用 cron 或 `apscheduler`

## 首批监控物品建议

从高流动性、低单价的消耗品起步(适合 1000 万本金快速周转):

- T4–T6 布、锭、木板、皮革(精炼材料,全游戏成交量最大的品类)
- T4–T6 布甲三件套、火法杖(作者自己懂行情的品类)
- T4–T6 纤维、矿石等原料(用于对比"卖原料 vs 卖半成品")

**先别碰高单价装备。** 单价高 = 单笔占用本金多 = 周转慢,
跟"现金流健康、回本快"的目标冲突。

物品 ID 从 `ao-bin-dumps` 的 `items.json` 解析,不要手写。

## 验收标准

1. 一次完整扫描在限流内跑完,不触发 429
2. 输出里没有明显离谱的"机会"(troll 过滤生效)
3. 随机抽 5 条结果进游戏核对,价格误差在可接受范围
4. 跟 AlbionSnipe / AlbionKit 的结论交叉对比,**对不上的地方就是过滤逻辑的缺口**
