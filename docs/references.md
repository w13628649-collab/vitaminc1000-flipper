# 参考项目

克隆在仓库旁边的 `../references/`(不进仓库:各自带 `.git`,放进来会变成 submodule)。
在另一台机器上一条命令拉齐:

```bash
mkdir -p ../references && cd ../references
for r in JPCodeCraft/AFMDataClientCore JPCodeCraft/AlbionDataAvalonia \
         Triky313/AlbionOnline-StatisticsAnalysis \
         ao-data/albiondata-client ao-data/albiondata-server-rails ao-data/AlbionPacketHandler \
         ao-data/albion-network ao-data/albiondata-bin-dumper \
         Nouuu/Albion-Online-OpenRadar mattias-wiberg/AlbionFlipper \
         felipelincoln/albion-blackmarket-arbitrage hrichharms/AlbiPy cosmic-fi/albionkit \
         dignityofwar/albionroads SugarF0x/albion-navigator; do
  [ -d "$(basename $r)" ] || git clone --depth 1 --filter=blob:limit=5m "https://github.com/$r.git"
done
```

## 按用途

| 项目 | 语言 | 看什么 |
|---|---|---|
| **AFMDataClientCore** | C# | Albion Free Market 数据客户端核心,**按 2026-10-12 补丁更新**。个人即时买卖、市场邮件、资产总览、专精(事件 150)、**专注(事件 10)**、游戏内估价、成交历史、金价。字段布局最新最全 |
| **AlbionDataAvalonia** | C# | 上面那个的桌面端:交易追踪器、邮件追踪器、SQLite 持久化。个人账本怎么拼出来的看这里 |
| **AlbionOnline-StatisticsAnalysis** | C# | 最完整的交易监控和复盘分析:利润时间序列、几点成交最多、物品排名、按城市统计;9 类交易含付给工作站的制作费。还有制作计算器 |
| albiondata-client | Go | AODP 官方抓包客户端,我们抓包层的祖先 |
| albiondata-server-rails | Ruby | AODP 服务端:挂单去重、5 分钟分桶取当前价、成交历史 |
| AlbionPacketHandler、albion-network | C# | AODP 早期的包处理库 |
| albiondata-bin-dumper | C# | 从客户端导出静态数据的工具(我们已经有客户端直接解出来的) |
| Albion-Online-OpenRadar | Go | 抓包层:多网卡运行时增减、网卡分类和 ExitLag 识别、录包、真实录包做回归测试 |
| AlbionFlipper、albion-blackmarket-arbitrage | JS / Python | 黑市倒卖,见 `black-market.md` |
| AlbiPy | Python | 最小的抓包嗅探库,看最简实现 |
| albionkit | TS | 综合工具集 |
| albionroads、albion-navigator | TS / Godot | 阿瓦隆之路路网,见 `research/roads-of-avalon.md` |

## 调研结论(2026-10-19)

### 个人即时买卖:请求里只有订单号,价格和物品从挂单缓存里查

AFM 的做法:浏览市场时把挂单按订单号缓存(最近 500 条);你点购买时客户端发
`AuctionBuyOffer`,它去缓存里查这个订单号得到物品、品质、单价、地点,先记一笔待确认,
服务端响应成功再入库。卖给买单同理。

| 操作 | 码 | 键(AFM 的解法,待样本核对) |
|---|---|---|
| `AuctionBuyOffer` 请求 | 83 | 1 = 数量,2 = 订单号 |
| `AuctionSellSpecificItemRequest` 请求 | 315 | 1 = 订单号,4 = 数量 |

### 挂单成交:靠市场邮件

`GetMailInfos`(174)响应:3 = 邮件 id,7 = 地点,11 = 类型,12 = 收到时间。
`ReadMail`(176)响应:0 = 邮件 id,1 = 正文(`|` 分隔,价格 ×10000)。

| 邮件类型 | 含义 | 正文字段 |
|---|---|---|
| `MARKETPLACE_SELLORDER_FINISHED_SUMMARY` | 卖单卖完 | 数量 \| 物品 \| 总银 \| 单价 |
| `MARKETPLACE_BUYORDER_FINISHED_SUMMARY` | 买单买齐 | 数量 \| 物品 \| 实付总额(单价 = 总额 ÷ 数量,**不能用挂单限价**) |
| `MARKETPLACE_SELLORDER_EXPIRED_SUMMARY` | 卖单到期 | 成交数 \| 总数 \| 总银 \| 物品 |
| `MARKETPLACE_BUYORDER_EXPIRED_SUMMARY` | 买单到期 | 成交数 \| 总数 \| 退款 \| 物品(**不给实付价**,单价只能从退款反推) |
| `BLACKMARKET_SELLORDER_EXPIRED_SUMMARY` | 黑市卖单到期 | 同卖单到期 |

### 专精和专注

- 事件 150 `FullAchievementInfo`:2 = 专精编号数组,3 = 对应等级,1 = 已满 100 级的编号。
  编号是 `achievements.xml` 里的下标
- 事件 10 `CraftingFocusUpdate`:3 = **当前专注余额**(float),2 = 这次增减,1 = 时间戳(ticks)

### 资产

`AssetOverview` 系列(520–527)给到容器级:每个仓库有哪些箱子、各多少件。
具体物品要打开容器后从 `AttachItemContainer` + 各类 `New*Item` 事件拿。

### 市场数据:AODP 怎么做

- **去重**:每条挂单算 SHA256,Redis 存 10 分钟,窗口内重复的不写库。
  我们已经是"状态没变只推 `last_seen`、不追加事件",同样效果
- **当前价只信最新一眼**:每条挂单记一个 5 分钟分桶,查价时每个 (物品, 城市, 品质, 方向)
  只取最新那个桶里的挂单。被吃掉的单不在新一眼里出现,自然不算。
  **我们的做法更细**:`book.Build` 的"最近一轮"规则按每次响应的价格区间判——旧单落在最新
  一页的价格范围里却没出现就剔除;比最新一页最差价还深的,看列表翻没翻到底决定剔除还是标
  stale。AODP 一刀切会把没重新翻到的更深挂单也扔掉,我们不会。这块不用改
- **黑市挂单永不过期**:到期时间在 2050 年以后,它改成一个月后
