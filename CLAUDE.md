# Albion 市场数据与交易工具

## 项目现状(2026-09-22)

**Go 客户端 + Go 服务端。2026-10-19 起只有作者一个人用**(离开了公会),
多人协作、CDK、靠成员覆盖数据这些前提都作废。重心从精炼转到**黑市倒卖**
(精炼实测九成亏本,只有开专注才赚),设计见 `docs/black-market.md`。

- 语言**只用 Go**,界面那层 HTML/CSS/JS 除外。Python 已经全部删掉,别再写
- **成员只开两个窗口:游戏,和我们的客户端。不要浏览器。**
  抓包和展示都在 Windows 客户端里(WebView2 窗口 + 内置本地 HTTP + 反向代理),
  界面资源编进 exe;服务端只负责收数据、下发数据和后端运算,不托管界面
- 界面配色用浅色,沿用旧版 web 端那套:银灰纸底 + 墨色字 + 青铜点缀
  (token 见 `guild/internal/webui/assets/app.css` 的 `:root`)。**用户 2026-09-23 明确要求**
  ("这个黄色背景我也不喜欢,还是以前那种配色好看"),推翻了之前"深暗木底 + 金边 + 羊皮纸字色、
  跟着游戏走"的做法。不要加暗色模式分支,也别再引入 Cinzel 那种游戏风字体
- 交易模型:四种执行方式、跨城套利、风险指标、吃深度、资金分配、实盘记账
- **精炼利润系统已上线**:「精炼」页签,四条线 T4–T8,整条线矩阵、原料来路、推荐卡片、
  候选路线和步骤清单、校准小工具(反推转化系数 k 和专精 FCE);引擎在 `guild/internal/refine`。
  **装备制造还没动工**。数据地基:
  - **配方**:`docs/client-data/recipes.jsonl.gz`,2,946 个可制造物品、
    12,437 条配方,带输入物品数量、专注消耗、产出数量、制造线
  - **工作站费率**:操作 **197 `GetClusterMapInfo`**(不是之前记的事件 49)。
    一个区一次请求拿全城建筑。详见 `docs/client-data.md` 第二点六节。
    **注意不要照抄 AODP 的结构体**——它把 `long[]` 写成 `[]int` 会溢出
  - **但拿到的是"这台站收多少",不是"这个物品制造要花多少"**。
    单品费用 = Item Value × 0.1125 × 费率 / 100,而
    **881 个 equipmentitem 和 754 个 weapon 在 items.xml 里都没有 itemvalue**,
    只有精炼材料那类 simpleitem 有。**所以这条链对精炼通、对装备断。**
    眼下够用(抱怨的就是精炼),做装备前要先追 `ItemStatsView.get_ItemValueStat`
  - **城市加成、站点↔物品映射、机制常量全在静态表里**,一个包都不用抓:
    `craftingmodifiers.xml` 141 条 craftinglocation、`buildings.xml` 9,199 条
    craftingitemlist、`ItemValueToNutrition=0.1125`、`maxuseagefee=1000`

进度和接口清单见 `docs/progress.md`,口径和设计取舍见 `docs/flipper.md`。

### 作者情况

- SRE / AI 基础设施背景,熟悉 Go、Python、容器、时序数据库。不需要解释基础概念
- 玩 **Albion 亚服(East)**,火法 build,纤维采集
- **已有游戏高级会员(premium)** → 市场税 4%
- 启动资金约 **1000 万银币**
- 策略优先级:**销量 > 利润率**。要现金流健康、回本快,能接受低毛利

### 回复偏好

- 用中文
- 不要重复已确定的结论,直接做事
- 涉及游戏规则/ToS 的判断,拿不准就说拿不准,不要编

---

## 不变的核心要求

原始需求见 `SPEC-flipper.md`。这几条任何时候都不能破:

- 必须有 **troll 过滤**——这是最容易漏且代价最高的一环
- **抓包只解操作响应(`params[253]`),一条事件(`params[252]`)都不解。**
  白名单按 (码, 参数键) 显式枚举,默认丢弃,不许"全抓后过滤"。
  这条线和 AODP 的实际采集面逐字重合,也正好把"服务端因为你站在那里才推给你
  的东西"整类排除在外。判准:**这条数据换个人换个时间看是不是同一个值?**
  是→可以收(费率/税率/所有者/建筑布局);否→不能收(资源节点/怪/人物位置)。
  理由见 `docs/client-data.md` 第二点十节
  - **唯一的例外**:专精三条事件 150/152/153(`FullAchievementInfo` /
    `AchievementProgressInfo` / `FullAchievementProgressInfo`)。**用户 2026-10-17 批准**,
    只记原始样本(全键,每码每次运行最多 20 条),业务层一个键都不给,解码器写好之前不进
    任何业务逻辑。什么时候下发**没验证过**(可能是登录/进区时推,不一定是开界面时)
  - **op 197 的样本只记白名单里那 10 个键**,不是全键:其余 82 个键没人看过,按
    AddBuilding/AddRealEstate 的签名可能有 canUse/guildAccess/ownerCharacter 这类
    "换个人看值就不同"的东西。全键样本没有批准,要开得先问用户
  - 白名单在 `guild/internal/protocol/allowlist.go`,**服务端收上传时也按它再判一次**
    (`api.saveCaptureExtras`),建筑只收城区本体(`world.ClusterCity`)
- 必须输出**数据新鲜度**,不能只给价差
- 排序主键是**日化绝对收益**,不是单笔利润率
- 抓包数据优先,AODP 兜底。两边的成交历史是同一份,不能取平均

---

## 交易经济学常量

```python
MARKET_TAX = 0.04        # 高级会员 4%(非会员 8%),只收卖方
SETUP_FEE  = 0.025       # 创建费,买单卖单都收,下单当场扣、不成交不退
```

**不要把费率直接相加当门槛。** 买侧的费乘在买价上、卖侧的费乘在卖价上,
基数不同,真实盈亏平衡价差要高于名义和。以 `econ.Mode.Breakeven` 为准:

| 买 / 卖 | 名义和 | 真实盈亏平衡 |
|---|---|---|
| 吃卖单 / 吃买单 | 4.0% | **4.17%** |
| 吃卖单 / 挂卖单 | 6.5% | **6.95%** |
| 挂买单 / 吃买单 | 6.5% | **6.77%** |
| 挂买单 / 挂卖单 | 9.0% | **9.63%** |

两个模式名义上都是 6.5%,真实门槛差 0.18pp —— 相加的算法根本分不开它们。

**运输时间现在可以算出来,不用再拍脑袋**:静态数据有 2,619 条区域出口、
99.4% 双向闭合,用出口坐标能算距离(Thetford↔Lymhurst = 10 跳 / 5,461 单位),
配 `movespeed=5.5` 换算成分钟。**而且进出 Caerleon 的 10 条快旅边全是
`noluggage=true`——Caerleon 套利不能快旅搬货,必须骑**,这条要单独建模。
见 `docs/client-data.md` 第二点九节。

实盘还要再扣运输时间和**未成交风险**:创建费下单就扣,挂上去没人来 = 白付
2.5%,这一项目前在模型里还是 0。口径核对过程见 `docs/flipper.md`。

## AODP API 的关键限制

**prices 端点不返回挂单深度。** 只有 `sell_price_min` / `buy_price_max` 等价格,
**没有数量**。所以"最低卖价 1000"可能只对应 1 件货。

这是 troll 过滤必须存在的根本原因。

AODP 服务端源码里有一个**带深度**的端点 `/api/v2/stats/orderbook/:id`
(`MarketOrderbookService` 按价位返回 `SUM(amount)`),但**实测没部署**:
2026-10-17 east / west / europe 四种写法全部 404,同一时刻 prices 正常 200。
所以挂单深度**仍然只有抓包拿得到**,"抓包优于纯 API"不用打折。

其他限制:

- 限流**实测 100 次/60 秒** + 300 次/300 秒(文档写的 180/分钟不对;2026-09-20 打到边界时
  429 原文 "limits requests to 100 per 60 seconds"),固定窗口、200 响应不带限流头。
  `conf.API.RatePerMinute` 默认已改成 95。URL 文档写 4096,实测 nginx 请求行 8192 才 414
  → **批量塞 item id**
- 每条价格带 `_date` 时间戳,**必须检查新鲜度**
- history 端点的 `item_count` 是成交量,但**只统计卖单**
- history 时间戳是 C# ticks:`(ticks - 621355968000000000) / 10000000`

### 端点

```
Base: https://east.albion-online-data.com
/api/v2/stats/prices/{items}.json?locations=&qualities=
/api/v2/stats/history/{items}.json?date=&end_date=&locations=&time-scale=
/api/v2/stats/gold.json?count=
/api/v2/stats/orderbook/{id}          ← 源码里有,三个区都没部署(404)
```

## 游戏机制常量(已核实)

### 回收率

```python
BASE_BONUS  = 0.18   # 所有皇家城市 + 凯尔利恩 + Brecilien 的基础生产加成
FOCUS_BONUS = 0.59
CITY_REFINE_BONUS = 0.40   # 精炼专属,叠加在 BASE 之上
CITY_CRAFT_BONUS  = 0.15   # 制造专属,明显小于精炼

def return_rate(base, city, focus):
    b = base + city + (focus or 0)
    return b / (1 + b)
```

| 情况 | 加成合计 | 回收率 |
|---|---|---|
| 加成城 + 专注 | 117% | 53.9% |
| 非加成城 + 专注 | 77% | 43.5% |
| 加成城 无专注 | 58% | 36.7% |
| 非加成城 无专注 | 18% | 15.2% |

**个人岛完全没有城市生产加成**,不开专注时回收率为 0。省下的手续费换不回来。

### 精炼专属城市

| 城市 | 精炼线 |
|---|---|
| Thetford | 矿石 → 锭 |
| Fort Sterling | 木头 → 木板 |
| Lymhurst | 纤维 → 布 |
| Martlock | 兽皮 → 皮革 |
| Bridgewatch | 石头 → 石块 |

### 工作站

- 使用费按**每 100 营养消耗**计价,上限 1000 银(Foundations 更新后从 10000 降下)
- 站点没耐久或没营养 → 不能制造
- 城内按 **N 键**看地图,点击建筑高亮所有同类型,悬停比价

## 多开与合规

- **premium 和焦点按角色计费**,不是按账号。一个账号最多 3 角色,
  但同账号一次只能登录一个角色 → 多城并行需要多账号
- 一台机器可以同时跑多个客户端,官方允许
- ToS 11.2:同时使用多个角色时,不得在城市/据点/岛屿之外互动、不得一起战斗、
  不得互相当斥候、**所有同时在线的角色必须有 premium**
- 明确禁止:多账号同时在游戏世界里一起采集

### SBI 对第三方软件的判断标准(2020 公告)

- 在 PvE、采集或 PvP 上给直接好处 → 禁止
- 修改客户端,或在客户端内额外显示信息(overlay / radar) → 禁止
- 数据聚合:通常可接受,条件含"数据在网站上提供而非游戏内覆盖层"、
  "不提供实时的游戏内优势"
- **公开条款里没有提到收费与否**
- 拿不准发邮件问官方

被动抓包(albiondata-client)被默许,依据是 SBI 前技术负责人 MadDave 2017 年
和社区经理 SirisLi 2025 年的公开表态,都挂在 AODP 首页。

**但要说清楚:那是选择性不执行,不是条款豁免。** 客户端自己打包的 SBI 完整
《条款与条件》(当前版本,可逐句引用)§13.3 明文禁止三件事,而且**都没有挂
"除非游戏规则允许"的口子**:

> - Use software playing in place of the player (such as scripts or bots)
> - Use measures that may cause an unreasonable or excessive load on technical capacities
> - Use software enabling "data mining" or which intercepts or captures data otherwise

第三条字面上把被动抓包也写进去了。所以**不能说"被动抓包在条款里是合规的"**。
AODP FAQ 里对 SBI 立场最接近原文的引用是 "analyzing and viewing data in this way
is acceptable" —— 两个被动动词,且 "this way" 特指抓包。

**主动向游戏服务器发请求不在这个范围里,不做。** 完整论证见 `docs/client-data.md`
第二点五节。另外 §4.2.2 禁止任何商业用途,这条和采集方式无关,**独立压在 CDK
售卖计划上**,要单独决策。

## 待验证 / 待办

1. ~~挂买单是否收 2.5% 手续费~~ → **已确认:收**。依据是游戏自己的本地化文本
   (`localization.json`):买单和卖单都有 `MARKETPLACE_*ORDER_LABEL_SETUP_COST`
   ("SETTING UP THIS ORDER COSTS {0}" / "下达这份订单需要花费{0}"),
   `@MARKETPLACE_SETUPFEE` 中文就叫"创建费"。所以往返摩擦 9% 是对的,
   `economics.buy_order_setup_fee` 保持 true。
   另注:`@ITEMDETAILS_STATS_AUCTION_TAX_REDUCTION`("你的拍卖税减少{0}%")说明
   **有装备能减拍卖税**,税率不是固定常量;走私贩网络另收"距离税费"(Distance Fee)
2. ~~多客户端并行抓包时城市归属是否正确~~ → **已读源码确认:会串**。
   albiondata-client 整个进程只有一个 Router / albionState,`LocationId` 是单值,
   城市订单的 LocationId 在包里是空的、靠它补,所以谁最后切区两边订单就都算谁的城市。
   详见 `docs/packet-capture.md`
3. 向 SBI support 邮件确认非同时登录的免费小号是否受 premium 条款约束
4. 亚服实际数据覆盖率摸底
5. **⚠️ 第 0 号:确认你的账号现在收到的市场数据是不是明文。**
   2024-01 SBI 把市场数据对**所有账号**加密过,后来只放行了"许多账号",
   判定规则连 AODP 都不知道(FAQ 原文:"based on some combination of variables
   that we are not aware of")。明文**不是协议属性,是服务端对账号的当前取值**。
   这条不成立,整条被动抓包路线归零。比下面所有事都优先。
   **但口径要注意两点**(见 `docs/client-data.md` 第二点七节):
   - 加密是**按消息粒度**不是按连接。AODP FAQ 原文:加密生效时
     market history 和 gold history 照常上传,只有 live market data 不发。
     所以 Clear/Detected **只说明 op 81**,对地图/建筑事件不作任何陈述
   - **我们的客户端现在答不了这个问题**:没有正向 Clear 信号(对任何加密包
     都喊"市场数据被加密"),而且只认 `msgType==131`、`switch` 无 `default`,
     别的加密消息类型会静默消失。**上场前先补这两处**,否则会误诊
   - AODP 那个绿灯**只由拍卖路径置位**,所以它对 op 197 一样零陈述。
     做完它,工作站费率是不是明文照样未知,要单独验
6. **市场列表页的实测**(回家后在游戏里试,一轮抓包能同时关掉好几条):
   开市场做四个动作——清空筛选选矿石翻一页、换一次排序、距离拉满、
   拨「显示基础价」开关。然后看:响应里 AuctionDTO 的条数 vs distinct
   `ItemTypeId`、请求里三个 int 哪个变了、`key8` 塞了几个 short。
   最要紧的一条是:**服务端在一批物品里怎么挑那 50 条挂单**(全局按价排,
   还是每个物品各给几条)。推导见 `docs/client-data.md` 第二节
   - 已定:**那 50 条是挂单不是物品**(`MarketPlaceBuyEntry.a : dgc`,
     `dgc` 包 `AuctionDTO`,行里带到期和距离标签)。之前算的"27 个变体
     一页拿全"是错的
   - 已定:**历史成交拿不到**——只有物品详情弹窗请求 op 95/96,
     浏览列表的视图一个都不发
   - 已定:**主动发请求这条路不走**,理由见 `docs/client-data.md` 第二点五节

## 参考

- 进度与接口:`docs/progress.md`
- 倒爷工具口径:`docs/flipper.md`
- 完整讨论记录(需要时再读,不要默认加载):`docs/session-transcript.md`
- 原始需求:`SPEC-flipper.md`
- **物品/配方/地点:以游戏客户端解出来的数据为准**,见 `docs/client-data.md`。
  物品目录已经内置(`guild/internal/catalog/data/items.jsonl.gz`,11,601 个,
  客户端 1.32.030.345319);配方和 Photon 码表在 `docs/client-data/`。
  **已经同步过的老库启动时也会换成内置目录**(`flip.preferBuiltin`):只有内置解出
  (`catalog.ClientExtractedAt`)之后手动 `POST /api/catalog/sync` 打的 ao-bin-dumps 补丁才保留。
  后台补配方走 `SyncRecipes`,不碰目录。**配方还是 ao-bin-dumps 那份**——客户端解出来的
  `recipes.jsonl.gz` 没有 `maxreturnamount`,换上去会丢掉「哪些投入物不返还」,还没接
  ao-bin-dumps(https://github.com/ao-data/ao-bin-dumps)降级为退路——
  它是社区按自己节奏导出的二手数据,和成员实际装的客户端不保证同版本
- 抓包客户端:https://github.com/ao-data/albiondata-client
- 物品图标:`https://render.albiononline.com/v1/item/{id}.png`
