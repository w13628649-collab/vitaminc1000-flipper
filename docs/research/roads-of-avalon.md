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

## 门的机制(`tunnelsystem.xml`)

```xml
<tunnelexitsettings lifetimemin="43200" lifetimemax="86400" shutdownwarningtime="3600"/>
```

门存活 12–24 小时,关闭前 1 小时开始警告。三种尺寸:

| 类型 | maxcharges | 每次恢复 | usedconnectionduration |
|---|---|---|---|
| SOLO | 7 | 300 秒 | 300 秒 |
| GROUP | 7 | 600 秒 | 600 秒 |
| RAID | 20 | 1200 秒 | 1200 秒 |

据此推测事件 448 的两个 short(键 9、10)是当前次数 / 最大次数,
四个时间戳里有关闭时间和警告开始时间。`FXAvalonRoadsPortal.SetRingStates`
大概就是门上显示剩余次数的那几个环。都待抓包确认。

门的**位置**是固定槽位:区域由模板拼成,`S_ROADS_Portal_01` 就是门槽
(300 个区域共 1,536 个,每区 2–6 个)。`S_ROADS_Portal_Mistcity_01` 只有
10 个区域有,是迷雾城随机门的落点。变的是哪些槽开着、通向哪里。

## 每个区域里有什么:静态数据能精确算出来

区域由模板实例拼成,每个实例带 `active_layers`;模板按层组织,
T4/T6/T8 各一层。两边的层 id 对得上,所以按激活的层数刷新点就是该区域的内容。

宝箱刷新点是 `cluster/tiles.xml` 里的瓦片,引用 `lootchests.xml` 的刷新行为:

| 瓦片 | 刷新行为 | 小地图图标 | 颜色 |
|---|---|---|---|
| `SpawnPoint_LOOTCHEST_SMALL_AVALON_ROAD_T*` | `AVALON_SMALL_SOLO` | `roads_of_avalon_solo_pve` | 绿 |
| `..._MEDIUM_AVALON_ROAD_VETERAN_T*` | `AVALON_MEDIUM_VETERAN` | `roads_of_avalon_group_pve` | 蓝 |
| `..._SMALL_AVALON_ROAD_ELITE_T6/T8` | `AVALON_SMALL_ELITE` | `roads_of_avalon_raid_pve` | 金 |
| `..._MEDIUM_AVALON_ROAD_ELITE_T6/T8` | `AVALON_MEDIUM_ELITE` | `roads_of_avalon_raid_pve` | 金 |

金箱只在两个 RAID 模板里(`S_ROADS_PVE_RAID_01`、`M_ROADS_PVE_RAID_01`)。
金箱的小地图配置是 `interestareaonly="false"`。

所有阿瓦隆宝箱同一个模型 `ROAD_LOOTCHEST_AVALON`、同一个名字"阿瓦隆宝箱"。
每个刷新点出箱概率 0.648,被开后 2–4 分钟重刷;品质按权重 6:3:1 掷
BASE / CHAMPION / BOSS。所以算出来的是**刷新点数**,即上限。

中型 RAID 模板里还有两个 `AVALON_ELITE_STATUE`:怪物
`T6_MOB_TN_AVALON_STATUE_ELITE_STANDARD`,中文"**阿瓦隆造物女神**",
刷新 9.75–19.5 小时。它总是成对跟着中金箱出现。

400 个区域的统计:

| 项目 | 有的区域 | 合计 | 单区最多 |
|---|---|---|---|
| 小金箱 | 74 | 96 | 3 |
| 中金箱 | 102 | 106 | 2 |
| 造物女神 | 102 | 212 | 4 |
| 蓝箱 | 222 | 268 | 3 |
| 绿箱 | 380 | 1,378 | 9 |
| 组队地城 | 180 | 248 | 4 |
| 单人地城 | 80 | 84 | 2 |
| 团队地城 | 6 | 6 | 1 |

**有金箱的区域 112 / 400。** 例:

```
TNL-031 Hasos-Iuimaum   绿 5,单人地城 1,组队地城 1
TNL-050 Poues-Unatam    小金 1,中金 1,造物女神 2,绿 2,组队地城 1
TNL-027 Conos-Avaelum   中金 1,造物女神 2,绿 8
```

资源按类型和等阶的数量在区域定义的 `distribution` 段里直接有。

### 和社区网站对照验证(2026-10-19)

用户常用的网站(水印 AVALON TRAVELERS)上 Poues-Unatam(TNL-050)的截图:
T6、两种金箱图标各 1、绿箱 2、一个地城入口、纤维和木头。和算出来的完全一致。

位置也对上了。地图是旋转 45° 的斜视图,用木头、纤维、两个绿箱五个地标拟合出
世界坐标 (x, z) → 截图像素 (u, v):

```
u ≈ 0.878·x + 0.864·z + 879
v ≈ 0.610·x − 0.628·z + 665
```

拿它预测没参与拟合的东西:

| 静态数据 | 预测 | 截图实际 |
|---|---|---|
| `M_ROADS_PVE_RAID_01` (-505, -185) | (275, 473) | 左边大台子上的金箱 (265, 470) |
| `S_ROADS_PVE_RAID_01` (-45, 55) | (887, 603) | 中间偏右的金箱 (895, 590) |
| 组队地城 (-45, 275) | (1077, 465) | 右侧门楼 (1095, 465) |
| 门槽 (-505, 175) | (586, 247) | 上方漩涡 (575, 255) |
| 门槽 (-145, 355) | (1058, 354) | 右侧漩涡 (1040, 365) |
| 门槽 (-145, -445) | (367, 856) | 左下漩涡 (345, 855) |
| 门槽 (175, -425) | (665, 1039) | 底部蓝色箭头 (670, 1040) |
| 门槽 (175, 275) | (1270, 599) | 右侧路尽头的圆圈,没有门 |

误差都在 20 像素以内。结论:**左边大台子上的是中金箱(带两个造物女神),
中间偏右的是小金箱。** 5 个门槽里截图上开着 3 个。

**玩家叫法(用户确认):**

| 玩家叫法 | 静态数据 | 模板 | 有它的区域 |
|---|---|---|---|
| **金王座** | 中金箱 `AVALON_MEDIUM_ELITE`,带两个造物女神 | `M_ROADS_PVE_RAID_01` | 102 |
| **台子** | 小金箱 `AVALON_SMALL_ELITE` | `S_ROADS_PVE_RAID_01` | 74 |

王座那块地形像王座,所以叫王座。

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
