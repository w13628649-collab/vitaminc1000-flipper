# 抓包能拿到的字段

三个项目交叉核对,每条都标了来源:

| 代号 | 项目 | 版本 | 语言 |
|---|---|---|---|
| **ADC** | ao-data/albiondata-client | `12ff34e` 2026-09-16 | Go |
| **SA** | Triky313/AlbionOnline-StatisticsAnalysis | `9f4471b` 2026-09-06 | C# |
| **OR** | Nouuu/Albion-Online-OpenRadar | `b0f024e` 2026-09-16 | Go |

`param` 是 Photon 包里的参数编号。标 **?** 的是三边都没有依据、要自己实测的。

## 先看这个:真实 opcode 藏在 params 里

Protocol18 下,Photon 自己的 `EventData.Code` 只用两个值(OR,实测):

- `3` = Move(热路径)
- `1` = 其他全部

**真实的 event code 在 `params[252]`(int16),真实的 operation code 在 `params[253]`。**
按 Photon 的 Code 去分发只会看到两类包。

### opcode 数值会随版本漂移

- 2026-06-29 那次 patch 在上游枚举里插了两条,**248 及以上的 code 全部 +2**(OR)
- 2026-08-31 Dragonfire 在 `NewMob` 的 `[32]` 插了一个参数,portal tag 从 `[32]`→`[33]`、
  enchant 从 `[33]`→`[34]`;`NewRandomDungeonExit` 从 `[5]` 起全部后移一位(OR)
- ADC 里 `evSkillData`(旧 114)和 `evPlayerOnlineStatus`(旧 77)就是因为这个失效、
  被注释掉的

**所以下面所有数值都是快照,不是常量。** 换版本先用 `params[252]` 对一遍。

## 单位与通用约定

| 项 | 说明 |
|---|---|
| 货币/定点数 | **协议里一律 ×10000 的整数**(SA 的 `FixPoint.InternalFactor = 10000`)。银币、金币、学习点、重置点都按这个换算。ADC 邮件解析里的 `price / 10000` 就是这个;但它的挂单 JSON 直接反序列化**没有除** |
| `LocationId` | 城市挂单在包里是**空的**,由客户端用当前所在地补;走私贩巢穴/休息区自带,形如 `xxxx@yyyy`。补进来的是**市场自己的 id**,不是城市本体,见下面那节 |
| `QualityLevel` | 1 普通 / 2 良好 / 3 优秀 / 4 杰出 / 5 不凡(照游戏内中文客户端的品质下拉逐字核对过,别写成"卓越/大师") |
| `AuctionType` | `offer` = 卖单,`request` = 买单 |
| `Timescale` | 0 = Hours(24 小时) / 1 = Days(7 天) / 2 = Weeks(4 周) |
| 时间戳 | 邮件/金价是 Unix 秒;市场历史是 C# ticks |
| 抓包过滤 | `tcp port 5056 || udp port 5056` |
| 服务器判定 | 按源 IP:东 `5.45.187.*` / 西 `5.188.125.*` / 欧 `193.169.238.*` |

### ⚠ 市场有独立于城市本体的 location id,不归一化两路数据永远对不上

抓包补进来的是**市场**那个 id,而 AODP 用的是城市本体的名字。
`ao-bin-dumps` 的 `formatted/world.json`(`[{Index, UniqueName}]`,1,427 条)里
它们是不同的条目:

```
0000  Thetford              ← AODP 用这个
0006  Bank of Thetford
0007  Thetford Market       ← 抓包拿到的是这个
0301  Thetford Portal

1000 Lymhurst / 1002 Lymhurst Market
2000 Bridgewatch / 2004 Bridgewatch Market
3004 Martlock  / 3008 Martlock Market
4000 Fort Sterling / 4002 Fort Sterling Market
3003 Caerleon  / 3005 Caerleon Market / 3013-Auction2 Caerleon Market
5000,5001 Brecilien / 5003 Brecilien Market
```

不收敛的话 `(T5_CLOTH, "Thetford Market")` 和 AODP 的 `(T5_CLOTH, "Thetford")`
永远是两个 key,融合一次都不会发生,查价页上同一座城会出现两行。

实测确认:抓到的 `location_id` 就是 `"0007"`,解析出来是 `Thetford Market`。

收敛规则建议**数据驱动**而不是硬编码:剥掉 `" Market"` / `" Portal"` 后缀和
`"Bank of "` 前缀,**只有剥完的结果本身也是个已知地点名时才采纳**。
这样 `Black Market` 不会被砍成 `Black`(world.json 里没有叫 Black 的地方),
黑市保持原样。另外 `3013-Auction2` 这种带后缀的要先按 `-` 切一刀。

落库时**把原始 `location_id` 一起存着**。收敛规则将来改了,有原始值才能重算;
只存归一化结果的话,老数据和新数据会分裂成两个 key。

---

## 1. 挂单 — `opAuctionGetOffers` / `opAuctionGetRequests` 响应

响应参数 `0` 是一个**字符串数组,每个元素本身就是一段 JSON**,直接反序列化:

| JSON 字段 | 类型 | 含义 |
|---|---|---|
| `Id` | int | 订单 ID |
| `ItemTypeId` | string | 物品 ID,含附魔后缀 |
| `ItemGroupTypeId` | string | 物品组 ID |
| `LocationId` | string | 城市;城市挂单为空,由客户端补 |
| `QualityLevel` | int | 品质 1–5 |
| `EnchantmentLevel` | int | 附魔等级 |
| `UnitPriceSilver` | int | 单价 |
| `Amount` | int | **这张单剩多少件** |
| `AuctionType` | string | `offer` / `request` |
| `Expires` | string | 到期时间 |

请求侧(你在游戏里点了什么)`opAuctionGetOffers`:

| param | 字段 | 含义 |
|---|---|---|
| 1 | Category | 大类 |
| 2 | SubCategory | 子类 |
| 5 | Quality | 品质 |
| 6 | Enchantment | 附魔 |
| 8 | ItemIds | uint16 数组,查询的物品 |
| 10 | EnchantmentLevel | |
| 12 | MaxResults | |
| 14 | IsAscendingOrder | 排序方向 |

---

## 2. 成交邮件 — `opReadMail` 响应

正文是 `|` 分隔的字符串,按邮件类型解析。邮件类型来自 `opGetMailInfos`(下一节)。

**正文布局(SA 注释里写死了,权威)**:

```
params[1] = QUANTITY | UNIQUE_ITEM_NAME | TOTAL_PRICE | UNIT_PRICE
            body[0]    body[1]            body[2]       body[3]
```

对照之下 ADC 的**过期邮件解析是坏的**:它把 `body[1]`(物品名字符串)拿去
`strconv.Atoi` 当数量用,必然报错并 `return nil`,整条过期通知就被丢掉了。
也就是说 `Sold` 这个成交率字段,用现成的 ADC **实际取不到**,得自己改。
成交邮件那条(`body[0]`=数量、`body[1]`=物品、`body[3]`=单价)是对的。

**`MARKETPLACE_SELLORDER_FINISHED_SUMMARY`(卖单成交)** → JSON:

| JSON 字段 | 类型 | 来源 | 含义 |
|---|---|---|---|
| `Id` | int | op param 0 | 邮件 ID |
| `ItemTypeId` | string | body[1] | 物品 |
| `LocationId` | string | MailInfo | 城市 |
| `Amount` | int | body[0] | 成交件数 |
| `UnitPriceSilver` | int | body[3] / 10000 | 单价 |
| `Expires` | string | MailInfo | |
| `TotalAfterTaxes` | float | 算出来的 | 用硬编码 `SalesTax = 0.03` 算,和 4%/8% 对不上 |

**`MARKETPLACE_SELLORDER_EXPIRED_SUMMARY`(卖单过期)** → JSON:

| JSON 字段 | 类型 | 来源 | 含义 |
|---|---|---|---|
| `Id` | int | op param 0 | 邮件 ID |
| `ItemTypeId` | string | body[1] | ⚠ 代码里 body[1] 同时被当 amount 用,下标疑似错的 **?** |
| `LocationId` | string | MailInfo | |
| `Amount` | int | body[1] | 当初挂了多少件 |
| `Sold` | int | body[0] | **实际卖掉多少件** |
| `UnitPriceSilver` | int | body[2] / 10000 | 单价 |
| `Expires` | string | MailInfo | |

只认这两种 OrderType,买单成交的邮件没有 handler。

---

## 3. 邮件列表 — `opGetMailInfos` 响应

四个平行数组,按下标对齐:

| param | 字段 | 含义 |
|---|---|---|
| 3 | MailIDs | []int,邮件 ID |
| 6 | Locations | []string,城市 |
| 10 | OrderTypes | []string,邮件类型(上一节那两个字符串) |
| 11 | Expires | []int64,Unix 秒 |

`opReadMail` 依赖这个缓存,没先加载列表就解析不出邮件。

---

## 4. 成交历史 — `opAuctionGetItemAverageStats` 响应

请求:

| param | 字段 | 含义 |
|---|---|---|
| 1 | ItemID | int32,数值 ID(不是字符串 ID) |
| 2 | Quality | |
| 3 | Timescale | 0/1/2 |
| 4 | Enchantment | |
| 255 | MessageID | 用来和响应配对 |

响应(三个平行数组,按下标对齐):

| param | 字段 | 含义 |
|---|---|---|
| 0 | ItemAmounts | []int64,成交件数 |
| 1 | SilverAmounts | []uint64,**成交总银币**(可自算精确均价) |
| 2 | Timestamps | []uint64,C# ticks |
| 255 | MessageID | 配对用 |

物品 ID、品质、timescale **不在响应里**,得靠 MessageID 和请求配对(客户端用
`MessageID % 8192` 做环形缓存)。上传时合成:

| JSON 字段 | 含义 |
|---|---|
| `AlbionId` | int32 物品数值 ID |
| `LocationId` | |
| `QualityLevel` | |
| `Timescale` | |
| `MarketHistories` | 数组,每项 `{ItemAmount, SilverAmount, Timestamp}` |

### 实测核对过的三件事

拿一份真实上报的报文对过(`AlbionId 1162`,`LocationId "0007"`,`Timescale 1`):

```
均价 = SilverAmount / ItemAmount / 10000
    = 493,987,560,000 / 65 / 10000 = 759,981
```

游戏内那个物品的「市场历史」面板显示的就是「平均价格 759,981,已售项目 65」,
**分毫不差**。挂单价那边同样 `/10000`:实测 45 个可比物品,自抓价 / AODP 价的
中位比值 **1.000**;金价 `133,347,494 / 10000 = 13,334.7` 对上 AODP 的 `13,334`。

均价要**先除成浮点再四舍五入**。连做两次整除会丢精度:
`493987560000 // 65 // 10000 = 759,980`,比游戏显示少 1。

### ⚠ Timescale 必须进主键,否则不同粒度的桶会互相污染

响应里没有 timescale,但**上报的 JSON 里有**。它决定桶宽:

| Timescale | 桶宽 | 游戏里对应的页签 |
|---|---|---|
| 0 | 小时 | 24 小时 |
| 1 | 6 小时 | 7 天 |
| 2 | 天 | 4 周 |

玩家在「市场历史」里切一下页签,同一段时间就会以**两种粒度**各来一份。
如果落库主键只到 `(item, location, quality, bucket)`,粗粒度那条只会覆盖掉
细粒度里时间戳相同的那一个桶,其余的原样留着 —— 同一段时间被重复计数。

实测复现:先按 6h 粒度上报某天 4×1000 件(合计 4000),再按 24h 粒度上报
同一天 4000 件,桶变成 `[00:00→4000, 06:00→1000, 12:00→1000, 18:00→1000]`,
合计 7000,**虚增 75%**。而这个数直接喂日均成交量,也就是吃单量估算的分母。

两种修法二选一:把 timescale 放进主键、读的时候只取一种粒度;或者入库时
检测时间区间重叠,重叠就用更细的那份替掉粗的。

注意 `store.WriteHistory` 现在**硬写 `timescale=1`** 且 `ON CONFLICT` 是
`DO UPDATE SET item_count = EXCLUDED.item_count`(覆盖不是累加),
一旦有人改成拉 `time-scale=1`(小时),24 个小时点会撞同一个主键,
一天的成交量只剩最后进来的那一个小时。抓包这一路接上之前先把这里定死。

### AlbionId → item_id 靠 `formatted/items.txt`

响应里的 `AlbionId` 是 int32 数值,落库要的是字符串 id。`ao-bin-dumps` 的
`formatted/items.txt` 就是这张表,12,237 行,格式:

```
   1116: T4_METALBAR                          : Steel Bar
   1162: T8_LEATHER_LEVEL3@3                  : Exceptional Fortified Leather
   1171: T5_CLOTH                             : Ornate Cloth
```

正则 `^\s*(\d+):\s*(\S+)\s*:` 就能解。翻不出来的要**明确报出来**,
别静默丢 —— 游戏更新加了新物品时,你需要知道是哪些。

---

## 5. 金价 — `opGoldMarketGetAverageInfo` 响应

| param | JSON | 含义 |
|---|---|---|
| 0 | `Prices` | []int |
| 1 | `Timestamps` | []int64,Unix 秒 |

---

## 6. 区域建筑 — `opGetClusterMapInfo` 响应

一堆平行数组,下标对齐,一个下标一栋建筑:

| param | JSON | 含义 |
|---|---|---|
| 0 | `ZoneID` | 区域 ID(转 int 失败说明是副本实例) |
| 17 | `BuildingType` | []int,建筑类型(枚举值代码里没有 **?**) |
| 18 | `Coordinates` | [][]int,坐标 |
| 20 | `Durability` | []int,耐久 |
| 22 | `AvailableFood` | []int,剩余营养 |
| 23 | `Reward` | []int |
| 24 | `AvailableSilver` | []int |
| 25 | `Owners` | []string,所有者名 |
| 31 | `Permission` | []int(枚举值代码里没有 **?**) |
| 33 | `AssociateFee` | []int,协会费率 |
| 34 | `PublicFee` | []int,公共费率 |

---

## 7. 角色自身 — `opJoin` 响应(JoinResponse)

登录和每次切换区域各发一次。ADC 只取了 5 个字段,**SA 解析得全得多**:

| param | 字段 | 类型 | 含义 | 源 |
|---|---|---|---|---|
| 0 | UserObjectId | long | 自己在当前区域的实体 ID,和 Move/NewCharacter 里的 ObjectId 同一套 | SA |
| 1 | UserGuid | Guid | 角色 UUID | SA/ADC |
| 2 | Username | string | 角色名 | SA/ADC |
| 6 | CharacterPartsJSON | string | 角色外观部件,**ADC 里注释掉了** | ADC |
| 8 | MapIndex | string | 当前地图 ID | SA/ADC/OR |
| 9 | — | []float32 | **自己的坐标**(len 2) | OR |
| 27 | CurrentFocusPoints | double | **当前焦点** | SA |
| 28 | MaxCurrentFocusPoints | double | **焦点上限** | SA |
| 33 | Silver | FixPoint | **银币**(÷10000) | SA |
| 34 | Gold | FixPoint | **金币**(÷10000) | SA |
| 37 | LearningPoints | FixPoint | 学习点(÷10000) | SA |
| 38 | Edition | string | 版本,**ADC 里注释掉了** | ADC |
| 41 | Reputation | double | 声望 | SA |
| 43 | ReSpecPoints | long[] | 重置点,取数组第 `[1]` 个再 ÷10000 | SA |
| 54 | InteractGuid | Guid | 当前交互对象 | SA |
| 56 | GuildID | Guid | 公会 ID | ADC |
| 58 | GuildName | string | 公会名 | SA/ADC |
| 64 | SourceExitPosition | float[2] | 来源出口坐标 | SA |
| 65 | SourceClusterIndex | string | 来源区域;静态地牢用 66 | SA |
| 66 | SourceClusterIndex(备) | string | 静态地牢优先用这个 | SA |
| 67 | HomeClusterIndex | string | 家园区域 | SA |
| 79 | AllianceName | string | 联盟名 | SA |
| 98 | IsReSpecActive | bool | 重置是否生效中 | SA |

参数编号排到 98,中间仍有空档没人解析。SA 的代码里 `PlayTimeInSeconds` 标着
"Temporarily removed until value is found",说明在线时长的参数编号连它也没找到。

### 背包、仓库、会员、税率不在这个包里

`opJoin` 只给上面这些。你问的那几样各有出处:

| 你要的 | 在哪 | 字段 | 源 |
|---|---|---|---|
| 银币(实时变动) | `UpdateMoney` 事件 | `0` ObjectId,`1` CurrentSilver | SA 注释 |
| 银币(拾取/领取) | `TakeSilver` 事件 | 样例 `map[0:-57 1:2178162 2:-57 3:10000000 8:10000]` | SA 注释 |
| 背包里的物品 | `NewSimpleItem` / `NewEquipmentItem` 事件 | `0` ObjectId,`1` ItemId,`2` Amount,`4` 估价,`5` CrafterName(装备才有) | SA 注释 |
| 物品进出背包 | `InventoryPutItem` / `InventoryDeleteItem` | `0` ObjectId,`1` 格位,`2` InteractGuid | SA 注释 |
| 容器/仓库内容 | `AttachItemContainer` 事件 | `0` ObjectId,`3` **ItemId[]** | SA 注释 |
| 仓库列表 | `BankVaultInfo` 事件 | `0` ObjectId,`1` `guid@区域`,`2` vaultGuids,`3` vaultNames,`4` iconTags,`5` colors | SA |
| 会员状态 | `PremiumChanged` / `PremiumExtended` 事件 | 字段未解析 **?** | SA 枚举 |
| 会员加成(采集) | `HarvestFinished` 事件 | `5` StandardAmount,`6` CollectorBonusAmount,`7` **PremiumBonusAmount** | SA 注释 |
| 会员加成(声望) | `UpdateFame` 事件 | `1` TotalPlayerFame,`2` 带区域倍率的声望,`3` 队伍人数,`4` 倍率,`5` **IsPremiumBonus**,`6` BonusFactor,`10` 背包声望 | SA 注释 |
| 工作站费率 | `opGetClusterMapInfo` 响应 | 见第 6 节 `PublicFee` / `AssociateFee` | ADC |
| 区域税/公会税(设置动作) | `opChangeClusterTax` / `opChangeGuildTax` | 字段未解析 **?** | SA 枚举 |
| **市场税率 / 挂单费率** | **三个项目都没有** | 没找到在包里传输的证据。多半是客户端本地常量,按 premium 状态取 4%/8% + 2.5% **?** | — |
| 装备变更 | `CharacterEquipmentChanged` | 样例 `map[0:297 1:26283117 2:[0 1721 0 0 0 2330 2301 2468 0 0] 5:[...]]`,`2` 是装备槽 ItemId 数组 | SA 注释 |
| 制造完成 | `CraftItemFinished` 事件 | 字段未解析 **?** | SA 枚举 |
| 重置点消耗 | `UpdateReSpecPoints` | `2` GainedReSpec,`3` PaidSilver | SA 注释 |
| 物品熟练度 | `AttunementInfo` | `0` UniqueItemName,`2` GainedAttunement,`3` MaximalAttunementValue | SA 注释 |

## 8. 技能 — `evSkillData`

**handler 写好了但没接线**:`decode.go` 里注册被注释掉,注释说 event code
在游戏更新后变了,旧值是 114。要用得自己找到新 code 填回去。

四个平行数组,下标对齐:

| param | 字段 | 含义 |
|---|---|---|
| 1 | SkillIds | []int |
| 2 | Levels | []int,等级 |
| 3 | Percentages | []float64,距下一级百分比 |
| 4 | Fame | []string,声望;值被 `[[ ]]` 包着,代码用切片剥掉 |

上传 JSON:`{Id, Level, PercentNextLevel, Fame}`。

---

## 9. 在线状态 — `evPlayerOnlineStatus`

同样**没接线**(旧 event code 77):

| param | 字段 | 类型 |
|---|---|---|
| 0 | CharacterID | string |
| 1 | CharacterName | string |
| 2 | IsOnline | bool |

---

## 10. 房产拍卖 — `opRealEstateGetAuctionData` 响应

| param | 字段 | 含义 |
|---|---|---|
| 0 | Unknown | 代码里就叫 Unknown **?** |
| 1 | HighestBidderName | 最高出价者 |
| 2 | CurrentWinningBid | 当前最高价 |
| 3 | AuctionStartTime | |
| 4 | AuctionEndTime | |

请求带 `PlotID`(param 0)。`opRealEstateBidOnAuction` 的请求和响应结构体都是空的。

---

## 11. 活动 / 土匪

`FestivitiesUpload.Events[]`:

| JSON | 类型 | 含义 |
|---|---|---|
| `Kind` | uint8 | |
| `Category` | string | |
| `UniqueName` | string | |
| `StartTime` | int64 | |
| `EndTime` | int64 | |

`BanditEvent`:`{EventTime int64, Phase int}`

---

## 12. 区域切换 — `opGetGameServerByCluster`

| param | 字段 | 含义 |
|---|---|---|
| 0 | ZoneID | 目标区域,客户端用它更新当前位置 |

---

## 13. 地图上的玩家与世界对象

没有单独的"地图玩家 JSON"。**玩家是两个事件拼出来的**:出现时一个、移动时一个。
下面的 code 全部来自 OR 的实测(pcap 回放测试钉住),字段是 `params[252]` 里的真实码。

### 玩家出现 — `NewCharacter`,真实 code **29**

| param | 内容 | 源 |
|---|---|---|
| 0 | ObjectId(区域内实体 ID) | SA |
| 1 | 角色名 | SA/OR |
| 5–7 | ByteArray | OR |
| 7 | Guid | SA |
| 8 | 公会名 | SA |
| 16, 17 | ByteArray | OR |
| 19–37 | float32,一串属性 | OR |
| 40 | 装备 ItemId 数组(byte[]/short[]/int[] 三种都出现过) | SA |
| 51 | 联盟名 | SA |

OR 的说法是这个事件带"昵称、公会、联盟、装备 id、法术 id、阵营 flag、初始血量"。
SA 只取了其中 6 项,剩下的在 `19–37` 那段 float32 里,**没人标出每一位是什么** **?**

### 玩家移动 — `Move`,Photon dispatch byte **3**(不在 `params[252]` 体系里)

| param | 内容 |
|---|---|
| 0 | int64 实体 ID |
| 1 | ByteArray,mode=3 时 30 字节 / mode=4 时 22 字节 |
| 4 | float32 posX —— 从 `params[1]` 偏移 **9** 取 |
| 5 | float32 posY —— 从 `params[1]` 偏移 **13** 取 |

另有 operation request `Move` 真实 code **22**:`[0]` 实体 ID、
`[1]` []float32 起点(len 2)、`[3]` []float32 终点。

### 位置能不能读 —— 分对象

这是这一节的关键,**别被"全都加密了"这种说法带偏**:

| 对象 | 位置读得到吗 | 说明 |
|---|---|---|
| 怪物、采集点、宝箱、祭坛 | **明文** | OR:`params[1]` 偏移 9/13 直接就是坐标 |
| **其他玩家** | **读不到** | 同样的偏移上多套了一层 XOR |
| 自己 | 明文 | `opJoin` 响应 `params[9]`,以及 Move request 自带 |

玩家位置那层 XOR 用一个 8 字节 `XorCode`:`加密值 XOR XorCode = 相对坐标`,
逐字节异或 4 字节 float。`XorCode` 走 `KeySync` 事件(OR 写的是"当前 code 603",
并特意提醒这个数字每次枚举插入都会变),而 KeySync 本身被 Photon 的 AES 层包着
(AES-256-CBC,IV 全零,密钥是 DH 共享密钥的 SHA256)。

所以被动抓包的链条断在这:拿不到 AES 密钥 → 读不到 KeySync → 没有 XorCode →
玩家坐标解不开。要拿只能上 MITM 代理去截 DH 握手,那是另一种性质的东西。

注意 OR 自己也说了,**玩家的出现(名字/公会/联盟/装备)是明文的**,
加密的只是持续移动中的坐标。ADC 能被动读市场、地图、邮件也是同理——
Albion 只对一部分内容上加密,不是全流量不可读。

### 其他世界对象(位置都是明文)

| 事件 | 真实 code | 关键 param | 源 |
|---|---|---|---|
| `NewSimpleHarvestableObjectList` | 39 | `[0]` []int16 批量 id,`[3]` []float32 批量坐标 | OR |
| `NewHarvestableObject` | 40 | `[5]` typeNumber,`[6]` mobileTypeId,`[7]` tier,`[8]` **[]float32 X/Y**,`[10]` size,`[11]` 附魔 | OR |
| `NewHarvestableObject`(旧样例) | — | `[0]` ObjectId[],`[2]` 最大采集次数,`[3]` 坐标数组,`[4]` 当前次数 | SA 注释 |
| `NewMob` | 123 | `[1]` typeId,`[7]` []float32 X/Y,`[13]` 最大血量,`[19]` 稀有度,`[31]` 名字(迷雾生物带 `MISTS_` 前缀),`[33]` 传送门标签,`[34]` 附魔 | OR |
| `NewRandomDungeonExit` | 325 | `[1]` []float32 X/Y,`[3]` 名字,`[6]` 模板,`[7]` 变体,`[9]` 附魔,`[16]` `MISTS_*` 标签 | OR |
| `NewLootChest` | — | `[1]` 坐标,`[3]` 类型,`[4]` 完整名,`[5]` 等级,`[6]` 时间戳 | SA 注释 |
| `NewShrine` | — | `[1]` 坐标,`[3]` 类型,`[4]` 类别 | SA 注释 |
| `NewLoot` | — | `[3]` 掉落者名,`[4]` 坐标,`[5]` float,`[7]` 数量 | SA 注释 |

`NewMob` 那几个下标就是 2026-08-31 Dragonfire 之后的值,之前 portal tag 在 `[32]`、
附魔在 `[33]`。

### 区域切换时的两个响应

| 消息 | 真实 code | 关键 param | 源 |
|---|---|---|---|
| `JoinFinished`(response) | 2 | `[8]` string mapId,`[9]` []float32 自己的坐标 | OR |
| `ChangeCluster`(response) | 41 | `[0]` string 新 mapId | OR |

## NATS topic

公开:`marketorders` / `markethistories` / `goldprices` / `mapdata` /
`banditevent` / `festivities`(各有 `.ingest` 和 `.deduped`)
私有:`skills` / `marketnotifications`

私有的只发到 `-p` 指定的目标。

---

## 有 opcode、但客户端没有 handler

`client/operations.go` 有 554 个 operation、`client/events.go` 有 711 个 event,
上面这些是全部已实现的。以下 opcode 名字存在,**字段结构未知,要自己抓包试**:

**自己的挂单和交易**

| opcode | 名字含义 |
|---|---|
| `opAuctionGetMyOpenOffers` | 我的未成交卖单 |
| `opAuctionGetMyOpenRequests` | 我的未成交买单 |
| `opAuctionGetMyOpenAuctions` | 我的进行中拍卖 |
| `opAuctionGetFinishedAuctions` | 已完成的交易 |
| `opAuctionGetFinishedAuctionsCount` | 同上,计数 |
| `opAuctionCreateOffer` / `opAuctionCreateRequest` | 挂单 |
| `opAuctionModifyAuction` | 改价 |
| `opAuctionAbortOffer` / `opAuctionAbortRequest` / `opAuctionAbortAuction` | 撤单 |
| `opAuctionSellRequest` | 卖给买单 |
| `opAuctionFetchAuction` | |

**制造、采集、经济**

| event | 名字含义 |
|---|---|
| `evCraftItemFinished` | 制造完成 |
| `evCraftingFocusUpdate` | 焦点变动 |
| `evItemRerollQualityFinished` | 重掷品质完成 |
| `evHarvestStart` / `evHarvestFinished` / `evHarvestCancel` | 采集 |
| `evTakeSilver` / `evRemoveSilver` | 银币进出 |
| `evUpdateFame` | 声望变动 |
| `evInventoryState` / `evInventoryPutItem` / `evInventoryDeleteItem` | 背包 |
| `evChangeEquipment` / `evCharacterEquipmentChanged` | 装备变更 |
| `evNewEquipmentItem` / `evNewSimpleItem` / `evNewFurnitureItem` / `evNewJournalItem` 等 | 各类物品对象 |
| `evCraftBuildingInfo` / `evPlayerBuildingInfo` | 建筑信息 |
| `evAttachItemContainer` / `evDetachItemContainer` / `evLockItemContainer` | 容器(仓库/箱子) |

**角色与公会**

| event | 名字含义 |
|---|---|
| `evCharacterStats` | 角色属性 |
| `evCharacterStatsKillHistory` / `DeathHistory` / `KnockDownHistory` / `KnockedDownHistory` | 战斗历史 |
| `evGuildUpdate` / `evGuildPlayerUpdated` / `evGuildStats` | 公会 |
| `evGuildMemberWorldUpdate` / `evGuildMemberTerritoryUpdate` | 公会成员位置 |
| `evInvitedToGuild` | |

**世界对象**

| event | 名字含义 |
|---|---|
| `evNewCharacter` | 出现的玩家 |
| `evNewHarvestableObject` / `evNewSimpleHarvestableObject` / `evNewSimpleHarvestableObjectList` | 采集点 |
| `evHarvestableChangeState` | 采集点状态变化 |
| `evNewSilverObject` | 地上的银币 |
| `evNewMob`(如有) / `evMove` 等 | |

已实现但上面没细列的:`opAuctionBuyOffer`(买入动作)、`evRedZoneWorldMapEvent`、
`evFestivitiesUpdate`。

---

## 多开时的字段污染

整个 albiondata-client 进程只有**一个** `Router` / `albionState`,
`LocationId` 是单值。城市挂单的 `LocationId` 靠它补:

```go
if order.LocationID == "" {
    order.LocationID = state.LocationId
}
```

所以同时跑两个客户端在不同城市翻市场,两边的挂单会被打上同一个城市。
带 `@` 的(走私贩巢穴/休息区)不受影响,因为那些自带 LocationId。
