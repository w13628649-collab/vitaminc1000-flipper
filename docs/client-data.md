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

**那 50 条是挂单,不是物品。** 这条之前写反了,改过来:

```
MarketPlaceBuyEntry        字段 a : dgc   + TimeLabel(到期) + DistanceLabel
MarketPlaceBuyOrdersEntry  字段 a : dgc   + TimeLabel
```

`dgc..ctor(string)`(VA `0x180b20e70`)用 JsonFx 把响应里的一条 JSON 解成
`Albion.Common.Auction.AuctionDTO`,存在 this+0x10。这个 DTO 有 21 个字段,
含订单 id、卖家角色 id、数量、到期时间、ReferenceId——**是一条挂单**。
列表行自带「到期」和「距离」两个标签,物品级的行不会有到期时间。

所以 `PageSize=50` = 一页 50 **条挂单**。之前算的"矿的 27 个变体全在一页上、
一次就拿全"是错的。

### 请求点名一批物品,响应回一页挂单

这两件事要分开看:

| | 内容 |
|---|---|
| **请求** | `key8 : short[]` 是**物品索引数组**,一次点名一批物品;`key12` 是 MaxResults |
| **响应** | `params[0] : string[]`,每条是一个 AuctionDTO,**一页 50 条挂单** |

所以"一次请求覆盖多个物品"成立,但**回来的不是每个物品一行**,而是这批物品
里的 50 条挂单。上限 `MaxPage=19` ⇒ 一套筛选最多翻出 20 × 50 = 1000 条挂单。

**这反而让"清空筛选"变得可疑。** 如果服务端是按价格全局排序,清空附魔和 tier
去查矿,排在前面的会是最便宜的 T2 矿的几十条挂单,贵的 T7/T8 可能一条都排不进来
——覆盖面不但没变宽,还可能变窄。按单个物品筛反而每次都拿到该物品完整的盘口。

服务端到底怎么在 27 个物品里挑这 50 条(全局按价排?还是每个物品各给几条?),
代码里定不了,**这是现在最该实测的一条**。

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

### 待办:进游戏实测才能定的

1. **服务端在一批物品里怎么挑那 50 条挂单。** 全局按价排序,还是每个物品各给几条?
   这条决定"清空筛选"到底是帮忙还是帮倒忙,是优先级最高的一条
2. **分类下拉能选到哪一层**(能不能选到「资源」大类,还是只能下钻到「矿石」)
3. **买单页是不是真批量。** 结构和购买页一模一样,但没实测过
4. 三个 int(`key11/12/13`)里哪个是页码。`key12` 社区标注为 MaxResults,
   另两个一个大概是 Max Distance,剩下那个是页码——翻一页看哪个变就知道
5. 一次请求的 `short[]` 能塞多少个物品(协议上限 / 服务端上限 / UDP 分片上限)

**一轮抓包能同时关掉上面全部。** 开一次市场,做四个动作:
清空筛选选矿石翻一页、换一次排序、把距离拉满、拨一下「显示基础价」开关。
然后对着这一轮的包看:响应里 AuctionDTO 的条数 vs distinct `ItemTypeId`
(DTO 里 `ItemTypeId` 就是字符串 id,不需要 short↔item_id 映射)、
请求里哪个 int 变了、`key8` 里装了几个 short。

### ⚠️ 第 0 号实测项:确认你的账号现在是不是明文

比上面所有都优先。AODP 自己的 FAQ 原文:

> In January 2024, SBI made changes to market data. **All accounts were receiving
> encrypted data** making the AODP client unable to capture live market data.
> We worked with a community manager at SBI; they decided to allow **many accounts**
> to receive unencrypted market data again. However, unencrypted data is based on
> **some combination of variables that we are not aware of.**

也就是说明文**不是协议属性,是服务端对部分账号的当前取值**,而且判定规则
连 AODP 都不知道。albiondata-client 自己有 `EncryptionClear` / `Detected` 指示。
**这一条不成立,整条被动抓包路线归零。**开一次游戏看一眼就能定。

## 二点五、能不能自己批量发请求(结论:不走这条路)

这是单独查过的一轮(六维取证 + 证伪 + 查漏)。结论分三段。

### 协议层面:支持批量

`AuctionGetOffers(81)` 的请求有 17 个键,其中:

| 键 | 类型 | 含义 |
|---|---|---|
| `key0` | long | 继承自拍卖基类 `aj8.bqs`,**极可能是市场建筑对象 id** |
| `key8` | `short[]` | **物品索引数组** —— 一次点名一批物品 |
| `key12` | int | MaxResults |

两个独立来源互相印证:客户端 IL2CPP 元数据(`aj2` 的字段表),和
albiondata-client 的 `operation_auction_get_offers.go` 里的 mapstructure 标签
(`ItemIds []uint16 "8"`、`MaxResults uint32 "12"`),8 个键位置类型全对。
客户端侧签名也一致:`MarketPlaceBuyView.DemandOffers(gav pFilter, short[] pItemTypes)`。

**所以"能不能批量"的答案在协议里是能。** 拦路的不是协议。

### 但"调接口"这个东西不存在

没有 HTTP,没有 RPC,没有任何可以"调"的入口。只有一条**已登录玩家的
Photon 可靠 UDP 会话**。把一个包塞进那条会话,需要三样东西,而这轮调查
对这三样**一行证据都没有**:

1. **写方向的序列化器** —— 已定位的 `ci9.b<T>` 全是读方向。
   谁把 `aj2` 的 17 个字段写进 `Dictionary<byte,object>`,没找到
2. **Photon 可靠层** —— peerID、握手 challenge、每通道序列号、CRC 开关,
   全是按公开 Photon 标准推的,混淆过的 `Photon3Unity3D.dll` 一个函数都没反汇编过
3. **登录握手 op5** —— 请求类 `xn` 有 28 个字段(含 `byte[]` 和多个平台 token),
   响应 `ahw` 有 99 个字段,结果码里有 `IncorrectClientVersion=200` /
   `ClientCorrupted=208` / `AccountAlreadyInUse=203`

换句话说:**"请求体可重建"只覆盖了链条的前三分之一**,后面两段是空白。

### 就算做成了,收益也很小

`key0` 那个 long 极可能是**市场建筑对象 id**(旁证:它只挂在拍卖族基类上,
不在通用请求基类 `x6` 上;事件基类 `avk.ddn` 同样是 long 且已定为对象 id;
客户端有 `MarketPlaceGui.get_BuildingId() -> long`)。

如果是,那就意味着**你必须站在那个市场里才能发这个包**——而站在市场里,
你本来就能点刷新。省掉的是几次鼠标点击,不是解锁新能力。跨城查盘口
在协议层面解决不了。

(注:这条是推断。`MarketPlaceFilterSearchBar` 有 `DistanceDropdown` 和
`get_CurrentSetDistance()`,走私贩网络也确实全网共享订单,所以"距离"维度
存在;但那是走私网络的机制,不等于能任选城市。)

### 合规:条款比我们之前记的严

客户端自己打包了 SBI 完整《条款与条件》
(`localization.xml` 的 `@LOGIN_POPUP_GENERAL_TERMS_AND_CONDITIONS`,约 5 万字符),
绑在 1.32.030.345319 上,是当前有效、可逐句引用的一手文本。§13.3「禁止操纵」
有三条直接压在这件事上,**且都没有挂"除非游戏规则允许"的豁免**:

> - Use software playing in place of the player (such as scripts or bots)
> - Use measures that may cause an unreasonable or excessive load on technical capacities
> - Use software enabling "data mining" or which intercepts or captures data otherwise

第一条管手段,第二条管量("批量"二字本身就落在这里),**第三条字面上把
被动抓包也写进去了**。

**这是对 CLAUDE.md 旧说法的纠偏**:不能说"被动抓包在条款里是合规的"。
条款字面不允许;AODP 能存在靠的是 SBI 针对那个具体项目的单独公开表态
(MadDave 2017 / SirisLi 2025,两张截图确实挂在 AODP 服务端仓库里:
`app/assets/images/maddave-approval.png`),属于**选择性不执行,不是条款豁免**。
这种默许不会自动延伸到一个条款里有独立禁止条文的新行为上。

AODP FAQ 里对 SBI 立场最接近原文的引用是:

> Sandbox Interactive has stated that **analyzing and viewing** data **in this way**
> is acceptable. You must not modify, manipulate, or **interfere with** the game.

"analyzing and viewing" 都是被动动词,"in this way" 指的是前文的抓包方式。
主动发包既不是 viewing,也不在 "this way" 范围内。

另外两条顺带查到的:

- **§4.2.2 禁止任何商业用途**("any use of the Services for commercial purposes
  is prohibited unless specifically allowed")。这条和采集方式无关,**独立压在
  CDK 售卖计划上**,是一个平行的风险面,要单独决策
- 客户端装了 **EAC + XignCode 两套反作弊**,而且操作码全表里有
  `ClientAntiAutoClickerInfo=422`、`ClientBotPatternDetectionInfo=423`、
  `ClientAntiGatherClickerInfo=424`——客户端会主动向服务端上报行为模式特征。
  封号理由枚举里 `@BAN_REASON_NEW_THIRD_PARTY_PROGRAMS` 和 `@BAN_REASON_BOTTING`
  是常备的可勾选类别

还有一条检测面值得记:`MarketPlaceCreateBuyOrderView.DemandLowestOfferPrices`
的 VA 是空壳 `0x1804873c0`,即**本版本客户端从不发 op 97**。客户端从不发的包,
你一发就是真实客户端永远不会产生的流量。同理,自建请求如果填了客户端从不填的
键组合,也是同类指纹。

### 真正能少点鼠标的两条路

都不在"发包"这边:

1. **AODP REST 本身就是批量接口。** 官方文档原文:"You can request a lot of
   data in a single call by combining the items and locations you need"。
   URL 上限 4096 字符、180 次/分钟(**注意:不声明 gzip 会被 rack_attack 压到
   6 次/分钟**)。248 个原料 × 8 城 × 5 品质 ≈ **2 次 HTTP**。
   零数据包、零会话状态、零密钥。让 AODP 当覆盖率基线,自己只补它缺的
2. **200 人天然分布在 8 城。** 客户端有 `MarketPlaceGui.StoreState` /
   `RestoreState`(配 `StateKey.ForCharacter` + `StateLifeTime.Permanent`),
   成员把筛选设一次就会被记住,之后路过市场点一下刷新就是一次完整批量请求。
   零检测面、零合规风险、零工程量

## 二点六、制造手续费:答案是操作 197,不是事件 49

你那条抱怨("按 N 键打开小地图就可以看到所有商品的制造手续费")**能解决**,
而且来源比原先猜的好得多。

之前 CLAUDE.md 里记的线索是事件 49 `CraftBuildingInfo`。那条是**服务端推送**的
单建筑消息。真正值钱的是另一个:

### `GetClusterMapInfo = 197`(操作,客户端主动请求)

| | |
|---|---|
| **请求** | 只有一个参数:`key0 = string`(区域 id) |
| **响应** | 类型 `afb`,**92 个参数键**,一次覆盖整个区所有建筑 |

响应里装的正好是精炼/制造成本模型缺的那几个输入:

| 键 | 类型 | 含义 |
|---|---|---|
| `key17` | `int[]` | BuildingType |
| `key18` | `float[][]` | 坐标 |
| `key20` | `long[]` | Durability(耐久) |
| `key22` | `long[]` | AvailableFood(营养余量) |
| `key24` | `long[]` | AvailableSilver |
| `key25` | `string[]` | Owners |
| `key33` | `long[]` | **AssociateFee** |
| `key34` | `long[]` | **PublicFee** ← 使用费 |

CLAUDE.md 里"使用费按每 100 营养消耗计价、站点没耐久或没营养不能制造"说的
就是 `key34` / `key22` / `key20`。**一个城一次请求,不是一个物品一次**——
这正是市场数据死活拿不到的那种批量形状。

### 接进来的工作量:很小

`op 197` 是 **operation response**,`params[253] == 197`。只要在
[protocol.go](../guild/internal/protocol/protocol.go) 的 `HandleResponse`
switch 里加一个分支就行,**事件通路完全不用碰**。

解码也是零工作量:AODP 读的那 11 个键全是基本类型或基本类型数组,
`deserializer.go` 现成支持(`float[][]` 走 `deserializeNestedArray` 递归)。
要坐标里的 `Vector2[]` 或所有者 `Guid[]` 才需要写自定义类型解码器——先不做。

**⚠️ 不要照抄 AODP 的结构体。** `operation_get_cluster_map_info.go` 把
坐标声明成 `[][]int`、费用/营养/耐久全声明成 `[]int`,而客户端声明的是
`float[][]` 和 `long[]`。坐标会被截成整数丢精度,银币类 `long` 塞进 `int`
有溢出风险。**按客户端声明的类型落库。**

### 没定死的一条:谁触发 op 197

`AlbionGui.ClusterMap.ClusterMapBase`(N 键那个区域地图类)有
`OnGetClusterInfo` `0x18114ac90`、`PostOpen` `0x18114bbf0`、
`AddBuilding` `0x181147000`、`AddRealEstate` `0x181147610`,
但**发请求的调用点没找到**。已恢复的 `0x180a9c910` 只是通用的
"响应 → `ci9.b<afb>` 解析 → 回调"壳子,只覆盖收方向。

所以"按 N 开图才发 / 进区就发 / 能不能请求自己不在的区"三种可能都排除不掉。
**覆盖率天花板悬在这里**——要是只有按 N 才发,成员路过城市不开图就一条不贡献。
实测很简单:开一次城里的 N 键地图,看有没有 op 197。

### 这块数据没有 AODP 兜底

AODP 的地图数据是**只写的**:客户端上传到 NATS 的 `mapdata.ingest`,
但服务端完整路由表(`config/routes.rb`)里**没有任何地图端点**。
建筑和费用数据没有任何公开来源——想要只能自己抓,也就没有第二路交叉校验。
这和市场数据"抓包 + AODP 双路"的格局完全不同。

(AODP 自己处理 op 197 时,ZoneID 不是纯数字就直接跳过,即地下城/藏身处/
个人岛一概不要。我们只要城里的制造站,这个取舍可以照抄。)

## 二点七、加密是按消息粒度,不是按连接

这条修正了第 0 号实测项的口径。

包头 `flags==1` 是包级,每条消息自己的 `msgType==131` 是消息级,
一个 Photon 包里 `commandCount` 条命令各带自己的 `channelId` 和 `msgType`
——**同一个包里可以一条响应是密文、旁边的事件是明文**。

AODP FAQ 原文直接证实它是按数据种类施加的:加密生效时
"market history and gold history are still sent to the AODP server;
only **live market data** is not sent"。也就是说 op 81 的响应是密文,
而 op 95/96(历史)和 gold 在同一条会话里仍然明文可读。

**推论:市场被加密 ≠ 地图被加密,反之亦然。** 2024-01 那次改动针对的是
挂单列表这一个东西。

但客户端里找不到任何 Albion/Photon 自有的加解密配置(全表 31,553 条常量里
带 crypt 的全是 .NET/Steam/Epic 库),所以**静态上判不出 SBI 对哪些 op
开了加密,只能实测**。

### ⚠️ 我们的客户端现在答不了第 0 号问题

上场实测前必须先补两处,否则会误诊:

1. **没有正向 Clear 信号。** AODP 是在一条 `AuctionGetOffers` 响应成功
   解出订单后才置 `EncryptionClear` 的,还有"最近是否发过市场请求"做门控。
   我们 [main.go:145](../guild/cmd/client/main.go#L145) 对**任何**加密包
   都喊"市场数据被加密"
2. **只认 `msgType==131`,`switch` 没有 `default` 分支。**
   ([parser.go:370](../guild/internal/photon/parser.go#L370) 和 :380-387)
   若出现别的加密消息类型(按 Photon 惯例 `0x80|kind`,比如加密事件),
   它连 `OnEncrypted` 都不触发,**静默消失**。`parser_test.go` 对这些零覆盖

另外:AODP 那个 Clear/Detected 指示灯**只说明 op 81**,对事件命名空间
(地图/建筑/资源)不作任何陈述。绿灯不保证地图事件可读,红灯也不证明不可读。

## 二点八、我们的抓包层现状

查的时候顺手核的,记下来:

- **码表已对着你实际装的客户端逐位验过,完全吻合**:操作 555/555、
  事件 710/710,名字零不匹配。客户端比 AODP 各多一条
  (op 555 `RequestDebugMobInfo`、event 711 `InventoryFrozenSlotsUpdate`)。
  这一版的漂移是**在尾部追加**,不同于 2026-06-29 那次中间插入导致 ≥248 全 +2,
  所以既有码位安全
- **解析器现在只认 4 个码**:81、82、`Join=2`、`GetGameServerByCluster=17`
- **全部事件被丢弃**:[main.go:139](../guild/cmd/client/main.go#L139) 的
  `capture.Handler` 字面量压根没填 `OnEvent`,711 个事件码解析完即丢。
  Photon 层本身是完整的,缺的是上面这一层接线

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
