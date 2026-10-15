# 客户端静态数据:能用上的部分

来源是一份对 Albion 客户端的逆向调查工作区(`rea-test`,2026-10-13,
客户端 `1.32.030.345319`,IL2CPP metadata v39,亚服)。那份工作区 2.5 GB、
26,678 个文件,不进仓库;这里只留**蒸馏过的产物**和**结论**,
以及把它们重新生成出来的脚本。

产物在 [client-data/](client-data/):

| 文件 | 内容 |
|---|---|
| `photon-codes.json` | 客户端声明的 Photon 码表:**556 个操作码 + 711 个事件码** |
| `recipes.jsonl.gz` | 2,946 个可制造物品、12,437 条配方(81 KB) |
| `extract.py` | 从调查工作区重新生成上面两份的脚本 |

客户端升版之后重新跑一遍调查,再跑 `extract.py <工作区路径>` 就能更新,
不用重新摸索一遍抽法。

## 一、码表:我们写死的四个全对

```
AuctionGetOffers       = 81   ✓
AuctionGetRequests     = 82   ✓
Join                   =  2   ✓
GetGameServerByCluster = 17   ✓
```

四个都和客户端枚举一致。(这里原先有过一次误判:某份精选产物里没有 17,
一度以为它错了。全表出来才确认是那份产物只选了 31 个类型,不是码错了。
**码表要查全表,不要查精选**。)

调查报告里还有一句直接印证我们修过的一个 bug:

> `ad1/ad2` 响应静态参数 **key0 为 string[]**

挂单就在 `params[0]`。我们 9 月那次修的正是"`HandleResponse` 卡在 253 上
导致这条路全死"——现在有客户端元数据佐证。

### 没用上但现成的行情接口

```
79  AuctionCreateOffer          89  AuctionGetFinishedAuctions
80  AuctionCreateRequest        91  AuctionFetchAuction
83  AuctionBuyOffer             92  AuctionGetMyOpenOffers
85  AuctionModifyAuction        95  AuctionGetItemAverageStats
88  AuctionSellRequest          96  AuctionGetItemAverageValue
                                97  AuctionGetLowestOfferPrices
```

`89 AuctionGetFinishedAuctions` 值得注意:**成交记录**。
我们现在的 `market_history` 只有 AODP 那一路(而 AODP 的 `item_count`
只统计卖单成交)。这个操作如果能抓到,就是第一手的双边成交。

## 二、配方:精炼功能的整个地基

`recipes.jsonl.gz` 每行一个可制造物品。实样(T5 精布):

```json
{"item_id":"T5_CLOTH","kind":"simpleitem","tier":"5","ench":"0",
 "craft_category":"fiber","shop":"crafting","shop_sub":"refinedresources",
 "recipes":[{"silver":"0","focus":"94","time":"0.0625","amount":"1",
             "inputs":[{"id":"T5_FIBER","n":"3","ench":"0"},
                       {"id":"T4_CLOTH","n":"1","ench":"0"}]}]}
```

抽检(和游戏内一致):

```
T4_CLOTH       [fiber] 产1 专注  54   2×T4_FIBER + 1×T3_CLOTH
T5_METALBAR    [ore  ] 产1 专注  94   3×T5_ORE   + 1×T4_METALBAR
T6_PLANKS      [wood ] 产1 专注 164   4×T6_WOOD  + 1×T5_PLANKS
T5_LEATHER     [hide ] 产1 专注  94   3×T5_HIDE  + 1×T4_LEATHER
T4_STONEBLOCK  [rock ] 产1 专注  54   2×T4_ROCK  + 1×T3_STONEBLOCK
```

`craft_category` 决定属于哪条精炼线,也就决定**哪座城有 40% 专属加成**
(五条线各 27 个物品,石头线 7 个)。配合 `CLAUDE.md` 里那张回收率表,
原料盈亏平衡价现在就能算 —— 不需要任何实时数据。

同一个物品常有多条配方(比如多一条用阵营代币替换部分原料的),
算利润时要逐条比,不能只取第一条。

**比 GitHub 的 `ao-bin-dumps` 可靠**:这份是本机实际安装的那个客户端版本
解出来的,不是上游某次快照。我们现在的目录同步走的是 GitHub。

## 三、专精和制造费:确认可抓,码也找到了

这两件事 `CLAUDE.md` 和待办里一直记着"要手填",现在有了具体入口。

| 要的东西 | 消息 | 码 | 类型 |
|---|---|---|---|
| 专精等级 | `FullAchievementProgressInfo` | **153** | 事件 |
| 专精增量 | `AchievementProgressInfo` | **152** | 事件 |
| 专精全量 | `FullAchievementInfo` | **150** | 事件 |
| 制造站费率 | `CraftBuildingInfo` | **49** | 事件 |
| 房主改费率 | `CraftBuildingChangeSettings` | **47** | 操作 |
| 当前专注值 | `CraftingFocusUpdate` | **10** | 事件 |
| 制造完成 | `CraftItemFinished` | **71** | 事件 |

专精在 Albion 内部归在**成就系统**下(命运板 = Destiny Board = Achievement),
所以搜 `Specialization` 一无所获,搜 `Achievement` 才找到。
客户端那边对应的 GUI 类型是 `ItemDetailsDestinyBoardView`、
`AlbionItemPowerMasteryProgress`。

制造费这条还有旁证:游戏内那个精炼计算面板 `CraftingCalculationsView`
一共只有三个 setter ——

```
SetUsageFee · SetNutritionConsumption · SetCraftingCapacityConsumption
```

另有 `EnchantItemView.OnUsageFeeInfo`(`On…` 开头是消息处理器)。
`buildings.xml` 里只有 `defaultcostfactorusers` 这种静态默认值,
**真实费率是房主设的服务器状态,必须抓**。

### 但:这份调查没有实时样本

报告原话:

> 本次分析没有启动游戏、附加游戏进程或捕获实时流量。

所以上面给的是**码和消息名**,不是字段布局。要把专精和制造费真正用起来,
还得在游戏里把对应界面打开一次、抓到样本、对着解字段。码表省掉的是
"哪条消息"这一步,不是"字段怎么解"。

同理,`radar-roadmap/ENCRYPTION-BOUNDARY.md` 对"哪些包不加密"的结论是:
不能静态断言,得有实时样本才知道某次连接启没启用保护。

## 四、物品目录:可以换一份更准的

`static-data/catalogs/market-items.jsonl`(17 MB,在调查工作区里)有
**12,237 个展开 ID**,带 EN-US / ZH-CN / **ZH-TW** 三语名,
外加 `weight`、`maxstacksize`、`unlockedtocraft`、完整 shopcategory 树。
名字是从客户端自己的 `localization.xml` 解的。

我们现在是 11,391 个、只有中英、走 GitHub 的 dump。

那份文件老实标了 **636 个 ID 没有这三种语言的名字,不伪造填充** ——
换过来的话我们的 `NameOf` 退回 item_id 的分支会更常走到,这是对的。

没把它放进仓库,是因为 17 MB 而且和现有 `catalog` 同步路径重叠,
换不换是另一个决定。要换的话 `extract.py` 里加一段就行。

## 五、市场位置:可以校验 `world` 包

`static-data/catalogs/market-locations.json` 给了每座城的
`city_cluster_ids` 和 `market_cluster_candidates`,带 displayname、
cluster_type 和 marketplace GUID。例如:

```
Thetford  城市本体 0000  ·  市场 0007 "Thetford Market"
```

和我们 `internal/world` 里那张表对得上。它还多给了黑市、Brecilien
这些边界情况的依据,如果要扩 `world` 的覆盖,从这里取证比猜强。

## 六、对那 8 条优化诉求的影响

| # | 诉求 | 这份数据帮得上吗 |
|---|---|---|
| 4 | 原料盈亏平衡价 | **能,配方齐了,现在就能做** |
| 2 | 专精免手填 | 给了事件码 152/153,字段要自己抓 |
| 5 | 制造费免手填 | 给了事件码 49,字段要自己抓 |
| 1 | 点一下卡半天 | 无关 —— 是架构问题 |
| 8 | 预计算而不是点击时算 | 无关 —— 是架构问题 |
| 3 | 总利润 / 总利润率 | 无关 |
| 6 | 数据陈旧 | 无关 |
| 7 | 界面太密 | 无关 |

**一句话:这份数据解决的是"算什么"(配方、码表),不解决"怎么算得快"
(第 1、8 条)和"怎么显示"(第 3、7 条)。那两类是我们自己的架构和界面问题。**

## 七、原始工作区

解压在 `/data/code/albion/albion-flipper/rea-test/`(2.5 GB,不进仓库)。
入口:

- `investigations/albion-online/REPORT.md` —— 主报告
- `investigations/albion-online/EVIDENCE-INDEX.md` —— 证据索引、SHA-256、来源
- `metadata/results/all-field-constants.json` —— 码表源头
- `static-data/decoded/items.xml` —— 配方源头(10.8 MB)
- `static-data/decoded/buildings.xml` —— 制造站静态定义
- `radar-roadmap/ENCRYPTION-BOUNDARY.md` —— 加密边界,写得很克制

报告自己把"观察 / 推断 / 未知"分得很清,引用时照抄它的限定词,别加码。
