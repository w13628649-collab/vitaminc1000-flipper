# 阿瓦隆之路的传送门在包里长什么样

2026-10-19。客户端 1.32.030.345319,数据来自 `rea-test/` 里的元数据和静态表。

## 门是哪条事件

事件 **448 `NewTunnelExit`**。"Tunnel" 是阿瓦隆之路的内部名,客户端里这几个类连得上:

| 类 | 作用 |
|---|---|
| `TunnelExitObjectView` | 世界里那个门的 3D 物体 |
| `TunnelExitMapLabel` | 地图上的标记:`ShowTooltip`、`UpdateLabels`、`OnTunnelExitChanged`、`GetSpriteByType` |
| `RoadsOfAvalonPortalTooltip` | 悬停提示框:`Show`、`GetStateIconSpriteName` |
| `FXAvalonRoadsPortal` | 门的特效:`SetTime`、`SetShutdownWarning`、`SetRingStates`、`SetPortalActivity` |

提示框的本地化文本:

| 键 | 英文 | 中文 |
|---|---|---|
| `@ROADS_OF_AVALON_TOOLTIP_TITLE` | Road of Avalon to | 阿瓦隆之路: |
| `@ROADS_OF_AVALON_TOOLTIP_TARGET_CLUSTER` | `{0}` | `{0}` |
| `@ROADS_OF_AVALON_TOOLTIP_CLOSE_TIME` | Closes in | 关闭倒计时: |
| `@ROADS_OF_AVALON_TOOLTIP_FREE_TIME` | Free to use | 免费使用 |

另有一条 **524 `NewTunnelExitTemp`**,只有 id、坐标、一个 float、一个 string,没有时间字段。
具体对应哪种门还没定。

## 区域在静态数据里

`static-data/catalogs/map-clusters.jsonl` 里簇类型以 `TUNNEL_` 开头的一共 **400 个**:

| 类型 | 数量 | 例 |
|---|---|---|
| `TUNNEL_BLACK_LOW` | 90 | `TNL-031` = Hasos-Iuimaum |
| `TUNNEL_HIDEOUT` | 72 | `TNL-151` = Quaent-Vynsum |
| `TUNNEL_LOW` | 52 | `TNL-058` = Cynos-Oxaeaum |
| `TUNNEL_ROYAL` | 44 | `TNL-001` = Ouyos-Aoeuam |
| `TUNNEL_BLACK_MEDIUM` | 32 | `TNL-050` = Poues-Unatam |
| `TUNNEL_HIDEOUT_DEEP` | 28 | `TNL-154` = Quatun-Nusas |
| `TUNNEL_DEEP_RAID` | 20 | `TNL-096` = Firos-Ezatam |
| `TUNNEL_BLACK_HIGH` | 18 | `TNL-055` = Coros-Atinaum |
| `TUNNEL_ROYAL_RED` | 16 | `TNL-023` = Hynos-Oiaelos |
| `TUNNEL_MEDIUM` | 10 | `TNL-097` = Firos-Osinsum |
| `TUNNEL_DEEP` | 10 | `TNL-101` = Fuyes-Izohun |
| `TUNNEL_HIGH` | 8 | `TNL-099` = Ooros-Ataltum |

区域本身是固定的,变的只是区域之间的连接。所以包里大概率只发 `TNL-xxx` 这样的 id,
提示框里的名字是客户端查本地表翻出来的。

## 事件 448 的 13 个键

键号按客户端的序列化规则推出:祖先类字段在前,每层按 metadata token 排序,
带属性 76961 的字段剔除,从 0 编号。父类 `any` 没有字段,`er` 只有一个静态字段(不算),
`au8` 自己的 13 个字段 token 连续递增(`0x4003731`–`0x400373d`),都不带 76961。

**这条规则在 op 95 的响应(`adw`)上和 AODP 的标注 3/3 对上:**
键 0 `long[]` = ItemAmounts、键 1 `long[]` = SilverAmounts、键 2 `GameTimeStamp[]` = Timestamps。

| 键 | 字段 | 类型 | 含义 |
|---|---|---|---|
| 0 | `db5` | long | 对象 id(所有 New* 事件都是这样,离开事件按它删) |
| 1 | `db6` | Vector2 | 门在当前地图的坐标。其它事件里 Vector2 在线上是长度 2 的 float 数组 |
| 2 | `db7` | float | 推测是朝向 |
| 3 | `db8` | string | 两个字符串之一应是目标区域 id |
| 4 | `db9` | bool | ? |
| 5 | `dca` | int | ? |
| 6 | `dcb` | string | 另一个字符串 |
| 7 | `dcc` | GameTimeStamp | |
| 8 | `dcd` | GameTimeStamp | |
| 9 | `dce` | short | 推测和容量有关 |
| 10 | `dcf` | short | 同上 |
| 11 | `dcg` | GameTimeStamp | |
| 12 | `dch` | GameTimeStamp | |

### 时间戳

线上是 uint64 的 .NET ticks。AODP 读 op 95 的 `GameTimeStamp[]` 用的就是 `[]uint64`。
换算:`(ticks − 621355968000000000) / 10000000` = Unix 秒。

四个时间戳字段带的是属性 **76962**。全表 76962 只出现在 `GameTimeStamp` /
`GameTimeStamp[]` 上(135 处),序列化器只剔除 76961,所以它们照常发送;
76962 对编码有什么影响没追到,但 ticks 格式已由 AODP 证实。

## 还没定的,一次抓包就能定

字段名被混淆了,下面这些静态元数据定不了:

- 两个字符串哪个是**目标区域**:都拿去 `map-clusters.jsonl` 里查,
  能查到 `TNL-xxx` 并且显示名和提示框一致的就是
- 四个时间戳哪个是**关闭时间**:都换成钟点,等于"抓包时刻 + 提示框的关闭倒计时"的就是。
  另一个大概率是"免费使用"截止时间
- 两个 short、bool、int 的含义
- 524 `NewTunnelExitTemp` 是哪种门

## 社区工具现在怎么做

GitHub 上看了六个阿瓦隆之路地图项目,没有一个解这条事件:

| 项目 | 做法 |
|---|---|
| [dignityofwar/albionroads](https://github.com/dignityofwar/albionroads) | 多人手动录入,WebSocket 共享 |
| [SugarF0x/albion-navigator](https://github.com/SugarF0x/albion-navigator) | Ctrl+S 截屏,识别文字 |
| [Pililink/Avalon-Atlas](https://github.com/Pililink/Avalon-Atlas) | 截屏识别 |
| [StreamOfMindSoup/albion-portal-screenshot-ocr](https://github.com/StreamOfMindSoup/albion-portal-screenshot-ocr) | 截图 OCR |
| [Crinlorite/avalontracker](https://github.com/Crinlorite/avalontracker)、[obxd/roadinator-web](https://github.com/obxd/roadinator-web) | 手动录入 |

它们识别的正是提示框上的"目标区域 + 关闭倒计时"——这条事件里本来就有的字段。
