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

## 二、不用一个个点物品:市场界面本来就有批量路径

这是从客户端 GUI 代码读出来的,是这份调查里最直接能省力的一条。

被动抓包只看得到**客户端自己请求了什么**,所以覆盖率的上限由"成员在游戏里
点了哪些界面"决定。但市场界面本身就有按筛选条件的**批量查询**,不是只能
一个物品一个物品点:

| 界面 | 方法 | 一次请求拿到 |
|---|---|---|
| **购买** 标签页 | `MarketPlaceBuyView.DemandOffers` + `GetCurrentFilter` | 按筛选条件的一整页**卖单**,多个物品 |
| **买单** 标签页 | `MarketPlaceBuyOrdersView.DemandRequests` + `GetCurrentFilter` | 按筛选条件的一整页**买单**,多个物品 |
| 物品详情弹窗 | `MarketPlacePopUpGui` | 单个物品的两侧 + 历史曲线 |
| 创建买单 | `MarketPlaceCreateBuyOrderView.DemandLowestOfferPrices` | 该物品各品质的最低卖价(op 97) |

两个标签页发的请求参数结构一模一样(`AuctionGetOffers` / `AuctionGetRequests`,
14–16 个字段:分类、子类、搜索词、tier、`short[]` 品质数组、翻页),
**都是过滤查询,不是单物品查询**。

### 每页 50 条,而且翻页会重新请求服务器

```
MarketPlaceGui.PageSize       = 50
MarketPlaceFooterView.MaxPage = 19
```

`OnPagingChanged` 有独立真实地址(`0x180fb9090`),不是空函数——对照:
同一个类的 `OnFiltersChanged` 指向 `0x1804873c0`,那是 IL2CPP 把所有空方法
合并到的同一个壳,好几个类的 `Assertions` 也指向它。所以**翻页确实会重新
向服务器要数据**。

流程是:`BuildItemListFromFilter` 在**本地**按筛选条件列出匹配的物品
(客户端自己有全量物品表),分页决定这一页是哪 50 个,再把这批索引
(请求里那个 `short[]`)发给服务器。

**但正因为每页 50 条,大多数情况根本不用翻页:**

| 选什么 | 变体数 | 页数 |
|---|---|---|
| 「矿石」子类(含全部等级和附魔) | 27 | **1** |
| 整个 `crafting/resources`(五种原料) | 133 | 3 |
| 整个 `crafting/refinedresources`(五种成品) | 115 | 3 |

矿的 27 个变体(T2–T8 × 5 档附魔)**全在一页上**。清空 tier 和附魔筛选,
一次请求全拿到——不是 21 次。

### 和之前那条结论的出入

调查报告里写过"「从集市购买」列表页只发卖单请求,必须点进物品详情页
才同时抓到买单"(实测 153 个组合只有卖单、只有 15 个两侧都有)。
那条观察本身没错,但结论下窄了:**那是只翻了「购买」页的结果**。
买单有自己的标签页,它一样是批量的。

### 挂单能批量,历史成交不能

这是个要紧的不对称:

| | 谁在请求 | 能不能批量 |
|---|---|---|
| 卖单(op 81) | `MarketPlaceBuyView.DemandOffers` + 筛选 + 分页 | **能** |
| 买单(op 82) | `MarketPlaceBuyOrdersView.DemandRequests` + 筛选 + 分页 | **能**(方法全是真实现) |
| 历史成交(op 95/96) | 只有 `MarketPlacePopUpGui` 和 `PopUpDetailsGraphModule` | **不能** |

浏览列表的视图**一个都不请求**历史成交——它只在物品详情弹窗里取。
想要历史就得一个个点开。

不过这个不对称其实是顺的:**要新鲜的那个(挂单)能批量,只能单点的
那个(历史)变化慢**。历史是按天分桶的,而且 AODP 本来就给
(虽然 `item_count` 只统计卖单)。

### 待办:这几条要进游戏实测才能定

代码只能推到这里,下面三条必须有实时样本:

1. **分类下拉能选到哪一层。** 能直接选「资源」大类就是 3 页覆盖五种原料;
   只能下钻到「矿石」就是每个子类 1 页(也才 5 次)
2. **列表那 50 条是物品还是挂单。** 推断是物品(`BuildItemListFromFilter`
   是"建物品列表",`SortItemsByPrice` 排的也是 items),但没有实时样本证实。
   如果是挂单,热门货一个物品就能占满一页,页数要重算
3. **买单页到底是不是批量的。** 结构上和购买页一模一样,但没实测过

**怎么测**:清空筛选、选矿石、翻一页,然后查库里这一下进来了多少个不同的
`(物品, 品质)`。27 个 ⇒ 是物品清单;远少于 27 ⇒ 是挂单。
`store.CapturedItems` 本来就按 `(物品, 品质)` 记了件数和 `last_seen`,
一条 SQL 就能看。

## 三、配方:精炼功能的整个地基

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

## 四、专精和制造费:确认可抓,码也找到了

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

## 五、物品目录:已经换成客户端那份

**做完了。** `guild/internal/catalog/data/items.jsonl.gz`(127 KB,编进二进制),
**11,601 个物品**,中英文名、分类、等级、附魔、品质档数全来自客户端的
`items.xml` + `localization.xml`。

| | 以前 | 现在 |
|---|---|---|
| 来源 | GitHub `ao-data/ao-bin-dumps` | 客户端 `1.32.030.345319` |
| 物品数 | 11,391 | **11,601** |
| 拿法 | 服务端启动时 HTTP 拉两份 dump(17+24 MB) | 编在二进制里,解压即用 |
| 没网 | 整个倒爷页面 503 | 照常 |

为什么内置而不是每次拉:

- **准。** ao-bin-dumps 是社区按自己节奏从某个版本导出的二手数据,
  和成员实际装的客户端不保证同版本
- **不依赖网络。** 服务端第一次起来就有完整目录
- **小。** 127 KB,比一次 HTTP 请求的抖动代价还低

代价是跟着客户端版本走,**游戏大版本更新后要人工重新解**:
重新跑一遍调查 → `python3 docs/client-data/extract.py <工作区>`。

`POST /api/catalog/sync`(走 GitHub)那条路留着,当作"客户端数据过期、
又来不及重新解"时的临时补丁。`catalog_meta` 里记了 `source`
(`client/1.32.030.345319` 或 `ao-bin-dumps`),随时能查现在用的是哪份。

**636 个没有中英文名的占位条目不收。** 那份数据自己标了这件事、不伪造填充,
我们跟着它的口径——收进来只会让搜索和菜单里多出一堆看不懂的 id。

## 六、市场位置:已经按 AODP 权威表重写

**做完了。** 工作区里冻结了 AODP 服务端自己的 `lib/location.rb`
(`public-data/sources/aodp-server/lib/location.rb`),`internal/world`
照抄它的表和归一化规则,再用客户端的 `market-locations.json` 交叉核对。

照抄而不是自己推,是因为融合的另一端就是 AODP:**它认什么我们就得认什么**,
推得再合理只要不一致就合不上。

### 黑市:从"拿不准"变成"确认是独立地点"

原来 `world` 的注释猜 3003 可能是黑市、怕并进 Caerleon 会让 NPC 求购
冒充买单。两头都证实了:

- 客户端 `market-locations.json`:cluster 3003 绑的 marketplace 实体
  类型就是 `BLACKMARKET`
- AODP 的 `location.rb`:`3003 → Black Market`,和 Caerleon 市场 3005 分开

所以不再原样放行,而是收敛成 `Black Market` 这个独立 key。

**黑市机制**(客户端本地化原文 `@TUTORIAL_BLACK_MARKET_HOW_TEXT`):

> 当野怪掉落作为战利品的物品时,该物品实际上来自黑市自身的库存。
> 当黑市的库存中有某项物品特别稀缺时,该物品就能以更高的价格售出。

界面上那个标签页叫「需求」(`@BLACKMARKETUI_DEMANDTAB`),分类叫
「黑市需求订单」。所以**黑市挂的是 NPC 买单,它的买价是需求信号而不是
玩家报价**。两条硬特性:

- **只有 NPC 买单,买不到货** → 往黑市的路线成立,从黑市出来的不成立。
  不用特判,`arb.playable` 发现缺卖价自然不出那条路线
- **只收装备**(`@BUILDINGS_BLACKMARKET_DESC`:"出售装备 - 它会成为
  阿尔比恩战利品的一部分")→ 精炼材料扫到也是空的

另外黑市 NPC 就站在 Caerleon 城里,和 Caerleon 市场隔一条街。
按跨城收路上时间会把"在 Caerleon 买装备、走过去卖给黑市"这条最实在的
路子算差一截,所以 `TravelHoursBetween` 给同 cluster 的一对 0.05 小时。

### 其余新支持的地点

| 原始 | 收敛成 | 依据 |
|---|---|---|
| `0301` / `1301` / … | 传送门城并进主城 | `PORTAL_TO_CITY` |
| `3013-Auction2` | 去后缀 → `3013` → Caerleon | `PORTAL_TO_CITY` |
| `BLACKBANK-####`、`xxxx@BLACKBANK-####` | 走私贩网络(**一个共享盘口**) | `SMUGGLERS_NETWORK_LOCATIONS` |
| `####-HellDen` | 并进主城市场 | `HELLDEN_TO_MARKET` |
| `"0007"` 和 `"7"` | 同一个地点 | AODP 内部存整数 |

走私贩网络那条值得单说:**AODP 把 ~38 个据点当成一个共享盘口**,
请求其中任何一个,它会把整个清单展开返回(`get_locations`)。
所以分开记没有意义。

`HELLDEN_TO_MARKET` 里 AODP 自己留了条警告:`3003-HellDen` 并进
**Caerleon 市场**而不是黑市。我们照抄了,测试里也钉住了。

## 七、对那 8 条优化诉求的影响

| # | 诉求 | 这份数据帮得上吗 |
|---|---|---|
| 4 | 原料盈亏平衡价 | **能,配方齐了,现在就能做** |
| 6 | 数据陈旧 / 覆盖不够 | **能** —— 见第二节,购买页和买单页都是批量的,不用一个个点 |
| 2 | 专精免手填 | 给了事件码 152/153,字段要自己抓 |
| 5 | 制造费免手填 | 给了事件码 49,字段要自己抓 |
| 1 | 点一下卡半天 | 无关 —— 是架构问题 |
| 8 | 预计算而不是点击时算 | 无关 —— 是架构问题 |
| 3 | 总利润 / 总利润率 | 无关 |
| 7 | 界面太密 | 无关 |

**一句话:这份数据解决的是"算什么"(配方、码表),不解决"怎么算得快"
(第 1、8 条)和"怎么显示"(第 3、7 条)。那两类是我们自己的架构和界面问题。**

## 八、原始工作区

解压在 `/data/code/albion/albion-flipper/rea-test/`(2.5 GB,不进仓库)。
入口:

- `investigations/albion-online/REPORT.md` —— 主报告
- `investigations/albion-online/EVIDENCE-INDEX.md` —— 证据索引、SHA-256、来源
- `metadata/results/all-field-constants.json` —— 码表源头
- `static-data/decoded/items.xml` —— 配方源头(10.8 MB)
- `static-data/decoded/buildings.xml` —— 制造站静态定义
- `radar-roadmap/ENCRYPTION-BOUNDARY.md` —— 加密边界,写得很克制

报告自己把"观察 / 推断 / 未知"分得很清,引用时照抄它的限定词,别加码。
