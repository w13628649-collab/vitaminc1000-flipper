# Albion 市场数据与交易工具

## 项目现状(2026-09-21)

**当前阶段:做倒爷工具(跨城/同城价差扫描)。**
精炼/制造利润系统是后续阶段,暂不动工。别把两者混在一起——倒爷工具不需要
NATS、不需要时序数据库,一个定时脚本就够。

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

## 第一阶段交付物:价差扫描器

详细需求见 `SPEC-flipper.md`。核心要点:

- 数据源只用 **AODP REST API**(免费),不碰 NATS
- 必须有 **troll 过滤**——这是最容易漏且代价最高的一环
- 必须输出**数据新鲜度**,不能只给价差
- 排序主键是**日化收益**,不是单笔利润率

---

## 交易经济学常量

```python
MARKET_TAX = 0.04        # 高级会员 4%(非会员 8%)
SETUP_FEE  = 0.025       # 挂单手续费,不退

# 即时买入 + 挂单卖出的往返摩擦
FRICTION = MARKET_TAX + SETUP_FEE   # ≈ 6.5%
```

价差必须**显著高于 6.5%** 才有意义。实盘中还要扣掉运输时间和未成交风险。

> 待验证:挂买单是否也收 2.5% 手续费。如果收,"挂买单 + 挂卖单"的双挂策略
> 摩擦约 9%。游戏内实测确认。

## AODP API 的关键限制

**prices 端点不返回挂单深度。** 只有 `sell_price_min` / `buy_price_max` 等价格,
**没有数量**。所以"最低卖价 1000"可能只对应 1 件货。

这是 troll 过滤必须存在的根本原因,也是本地抓包(AlbionSnipe / albiondata-client)
优于纯 API 的地方。

其他限制:

- 限流 180 次/分钟、300 次/5 分钟;URL 上限 4096 字符 → **批量塞 item id**
- 每条价格带 `_date` 时间戳,**必须检查新鲜度**
- history 端点的 `item_count` 是成交量,但**只统计卖单**
- history 时间戳是 C# ticks:`(ticks - 621355968000000000) / 10000000`

### 端点

```
Base: https://east.albion-online-data.com
/api/v2/stats/prices/{items}.json?locations=&qualities=
/api/v2/stats/history/{items}.json?date=&end_date=&locations=&time-scale=
/api/v2/stats/gold.json?count=
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

## 参考

- 完整讨论记录(需要时再读,不要默认加载):`docs/session-transcript.md`
- 第一阶段需求:`SPEC-flipper.md`
- 物品/配方:https://github.com/ao-data/ao-bin-dumps(`items.json`、`world.txt`)
- 抓包客户端:https://github.com/ao-data/albiondata-client
- 物品图标:`https://render.albiononline.com/v1/item/{id}.png`
