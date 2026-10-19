# Albion Online 亚洲服：地图、市场与地图人物数据报告

调查日期：**2026-10-13**，Asia/Singapore。服务器：**Asia / East**。
目标安装目录：`C:/Users/ludy/AppData/Local/AlbionOnline`。
客户端：`albiononline-win32-full-1.32.030.345319`；Unity `6000.3.12f1`；IL2CPP 元数据 **v39**。

## 1. 结论与获取路线

**安装包中的静态地图和物品数据已经导出；亚洲服公开市场和玩家接口已经取得响应样例。**
地图人物的当前身份、装备、位置和生命状态需要从客户端收到的人物消息中维护，公开玩家档案不能提供实时人物地图。
REA/Ghidra 已完成本版本原生库导入和 **23 个选定函数**分析，保存 **24 条 Evidence**；函数的间接流、字段语义与实时覆盖限制详见各专题。

这里的“所有数据”有不同覆盖范围，使用时需要随结果保存：

| 你要的数据 | 获取入口 | 本次结果 | 覆盖范围 |
| --- | --- | --- | --- |
| 世界地图区域、出口、标记、市场位置 | 本地 `GameData` → world/cluster XML | 已导出完整文件及结构化目录 | 此安装包携带的定义，含测试、岛屿和动态候选 |
| 地图模板、资源点/怪物/建筑/宝箱定义 | cluster → template → tile/compound → 语义规则表 | 已导出定义和放置记录 | 静态候选；当前刷新、余量、附魔和活动层另需运行状态 |
| 地图图片、地形高度和碰撞 | Unity assets、`.resS`、`.pmsh`、tile 几何表 | 已建立资源与字节范围索引 | PNG、完整 Mesh、碰撞编码及最终地图渲染尚未完成 |
| 全部物品 ID、名称、附魔、交易类别 | items + localization | 已导出 12,237 个唯一物品 ID | 静态物品清单；不保证全部可交易或有行情 |
| 市场买卖报价和成交历史统计 | East AODP REST | prices/history HTTP 200，有原始样例和查询工具 | 社区观察到的报价范围、历史汇总 |
| 社区已保存的市场全库 | East 数据库备份 | 已核对目录与恢复说明 | 项目所保存的数据；本次未下载/恢复大型备份 |
| 持续订单观察 | East 公共 NATS | 已核对文档和源码 | 贡献者新观察到的订单；本次未连接 feed |
| 玩家姓名、公会、联盟、统计、历史装备 | Asia GameInfo | 已核对档案、公会、联盟和战斗事件 | 公开档案与事件快照；无当前坐标或在线状态 |
| 地图人物出现、移动、装备与生命更新 | 当前客户端 Photon 业务消息 | 当前版本类型和处理代码分析，详见人物章节 | 客户端实际收到的对象；本次未取得实时样本 |

**没有证据支持从这些入口取得未发送给客户端的全地图人物坐标，或游戏服务器权威的全部实时市场订单。**
本次分析没有启动游戏、附加游戏进程或捕获实时流量；公开 HTTP 样例和本地静态证据均已保存。

专题原始细节：

- [本地静态数据报告](static-data/REPORT.md)：解码、地图结构、坐标、Unity 资源、物品和市场位置。
- [公开数据报告](public-data/REPORT.md)：Asia 接口、备份、NATS、字段、单位及来源修订。
- [当前版本元数据与协议报告](metadata/REPORT.md)：人物、拍卖和地图消息结构。
- [REA 原生分析报告](native/REPORT.md)：实际 Ghidra Evidence、函数和分析限制。
- [证据索引](EVIDENCE-INDEX.md)；[本机 REA 环境记录](../../REA-ENVIRONMENT.md)。

## 2. 现在直接使用现有数据

以下命令在工作区根目录 `C:/Users/ludy/Documents/code/rea-test` 运行。
结构化数据已经存在，不需要再次解码或载入整个游戏资源目录。

### 2.1 查询地图、物品与市场

```powershell
$albionPython = 'C:\Users\ludy\AppData\Local\Programs\Python\Python314\python.exe'
$albionCatalog = '.\investigations\albion-online\query_catalog.py'

# 精确读取 Drownhorse Basin / 0217 的静态区域记录
& $albionPython $albionCatalog cluster '0217'

# 对物品 JSON 全文做文字查询，最多输出 5 条
& $albionPython $albionCatalog item 'T4_BAG' --limit 5

# 查询城市与普通市场的本地 cluster 映射
& $albionPython $albionCatalog markets
```

[query_catalog.py](query_catalog.py) 按行读取 JSONL。
`item` 查询会匹配整个记录中的文字，包括属性和描述，并非仅匹配 ID 或名字。
精确关联时应读取记录的 `item_id` 或 cluster 的 `attributes.id`。

### 2.2 查询亚洲服报价、历史与玩家档案

```powershell
$albionFetch = '.\investigations\albion-online\public-data\fetch_public_data.py'

& $albionPython $albionFetch prices --server east --items 'T4_BAG,T4_BAG@1' --locations '3005,2004' --qualities '1,2,3,4,5' --output '.\investigations\albion-online\public-data\manual\bag-prices.json'

& $albionPython $albionFetch history --server east --items 'T4_BAG' --locations '3005' --qualities '1' --date '2026-10-01' --end-date '2026-10-12' --time-scale 24 --output '.\investigations\albion-online\public-data\manual\bag-history.json'

& $albionPython $albionFetch search --server east --query 'Albion' --output '.\investigations\albion-online\public-data\manual\search.json'

# player-id 从搜索响应选取；下面是本次公开样例的 ID
& $albionPython $albionFetch player --server east --player-id 'wpDh5rpcTXyKkmDpnm7PNg' --output '.\investigations\albion-online\public-data\manual\player.json'
```

[fetch_public_data.py](public-data/fetch_public_data.py) 只依赖 Python 标准库，保存原始 JSON 和同名 `.metadata.json`，记录 URL、server、UTC 时间、状态、响应头和 SHA-256。默认拒绝覆盖；后续再查时换输出文件名。
它有 gzip、大小限制、输入编码和 429 退避。**该新增工具尚未运行测试或执行查询**；本次接口成功样例来自研究采集脚本，不能当作新增工具的执行验证。
失败时另存唯一 `.failure-<id>.metadata.json`，保留已有 JSON 与其来源记录；服务要求等待超过 300 秒时停止自动重试并记录期限。

## 3. 地图：静态定义、位置与运行状态

### 3.1 已导出的范围

本地 `game/Albion-Online_Data/StreamingAssets/GameData` 中 **3,278 个 `.bin` 全部成功解码**：3,259 个有效 XML，19 个 `profanity_*` 为非 XML 词表载荷。每个文件的输入/输出路径、字节数、SHA-256 和解析状态都在 [extraction-manifest.json](static-data/extraction-manifest.json)。

| 数据 | 数量 | 主要产物 |
| --- | ---: | --- |
| cluster 定义 | 1,427；enabled 为 1,336 | [map-clusters.jsonl](static-data/catalogs/map-clusters.jsonl) |
| 出口 | 3,223 | [map-exits.json](static-data/catalogs/map-exits.json) |
| 小地图 / 世界地图标记 | 1,945 / 132 | cluster 的 `world_sections` |
| 市场定义 | 106 | [market-locations.json](static-data/catalogs/market-locations.json) |
| 世界引用模板实例 / 模板文件 | 15,384 / 1,910 | [template-index.json](static-data/catalogs/template-index.json) |
| 模板 tile/compound 放置记录 | 1,888,023 | [template-placements.jsonl](static-data/catalogs/template-placements.jsonl) |
| 普通 tile / compoundtile 定义 | 159,078 / 9,272 | tile 与 compound 目录 |
| 带游戏语义子节点的 tile | 8,497 | [tile-semantics.json](static-data/catalogs/tile-semantics.json) |
| 世界根 criticalObjects / staticdungeons | 1,195 / 98 | [world-other-sections.jsonl](static-data/catalogs/world-other-sections.jsonl) |

1,336 个 enabled 定义包括测试、岛屿和动态候选，**不能解释成今天亚洲服实际开放的区域数**。
同样，静态放置记录不能解释为当前世界中存在的实体数量。

### 3.2 恢复地图数据的关联顺序

```mermaid
flowchart TD
  A[当前 world.xml 区域目录] --> B[cluster 布局 file]
  B --> C[templateinstance 引用 位置 旋转 活动层]
  C --> D[template 普通和复合 tile]
  D --> E[tile 几何和游戏语义]
  E --> F[harvestables mobs 建筑 宝箱和刷新规则]
  C --> G[Unity 模板纹理与地图图层]
  H[客户端运行时区域和对象消息] --> I[当前实例与对象状态]
  F --> I
```

1. `decoded/cluster/world.xml` 列出 `id/displayname/file/enabled/type/origin/size/worldmapposition` 及小地图 bounds、高度范围等属性。结构化目录保留所有原始 XML 分组和字段，包括 exits、territories、outposts、worldbosses、travelpoints、marketplaces、roads、distribution、mobcounts。
2. 按 `cluster@file` 找 `.cluster.xml`。1,427 个区域引用的布局均存在。出口 `targetid` 常为 `出口 UUID@区域 ID`；保留原字符串和 `targettype/restricted/staticdungeonentrance` 等条件，建立连接图。
3. 读取 `templateinstance@ref/pos/rot/dyn/activelayer`。全部引用均找到候选，但 **427 个同名模板有多个环境变体**，如 DEAD/GREEN/RED；当前脚本保留全部候选，没有强制选择。
4. 展开模板中的 tile 和 compoundtile，并关联 `cluster/tiles.xml` 的语义子节点。采集、怪物生成、建筑、宝箱、传送门、鱼区等可以由此识别。
5. 关联 `harvestables.xml/mobs.xml/randomspawnbehaviors.xml/resourcedistpresets.xml/resourceprefabmappings.xml/dynamictemplates.xml/dynamicclusters.xml` 等完整规则表。

资源规则中有 Tier、产出物品、charges、重生系数、采集时间；怪物规则中有生命、攻击、fame、prefab 和重生范围。
这些说明类型和候选生成规则；**当前资源余量、稀有附魔、怪物存活、宝箱内容和刷新倒计时需服务器实例状态**。
道路、迷雾、地下城的当前实例和连接也不能从动态模板目录直接得到。

### 3.3 坐标如何组合

应分别保存世界地图显示坐标、cluster 局部坐标、Unity XYZ、模板局部坐标及小地图像素坐标。
`worldmapposition` 不是经纬度，也不能直接加到玩家/资源的局部位置上。

组合静态放置点时：先选区域模板变体与活动层 → 递归应用 compound 的 scale/旋转/平移 → 应用 templateinstance 变换 → 最后按实际小地图 bounds 和纹理图层投影。
一般 Unity Y 轴旋转的候选公式为：

```text
x' = cos(θ) x + sin(θ) z + tx
y' = y + ty
z' = -sin(θ) x + cos(θ) z + tz
```

每层 scale 必须先应用；坐标轴正方向、原点、小地图翻转和最终 runtime 选择仍需消费代码或展示证据核对。
[placement-example-0217.json](static-data/catalogs/placement-example-0217.json) 提供 30 个直接语义放置候选，保留来源和推断标签；它没有递归展平 compound，也没有选出全部动态层。

### 3.4 图像、高度、碰撞和 Mesh 的获取入口

已离线读取 **307 个 Unity SerializedFile v22 文件**，得到 2,082,125 个对象和 100,472 个资源路径。
[unity-assets/map-assets.json](static-data/unity-assets/map-assets.json) 将 2,984 个地图资源路径解析到对象，其中 2,930 个 Texture2D 的 `.resS` 切片范围有效。
索引保存 source、path ID、offset/size、候选尺寸/纹理格式和 external file 映射。
`StreamingAssets/CustomMeshes/meshes.toc` 还索引 11 个 `.pmsh` 包、3,943 个有效切片。

可从 `clustermap/`、`minimapgen/`、`worldmap/` 路径选择基础模板图和图层，用支持 Unity 6000.3 的格式工具读取 Texture2D 与对应 `.resS`，再按 `minimapgendata.xml` 的 `vMin/vMax`、图层和实例位置组合。
对象身份用 `(source, path_id)`；`Layer_01` 等短名字重复，不能单独作键。
本次只完成索引，**没有导出 PNG、恢复完整 Mesh 或渲染最终地图**。

tile 几何表保留 2,066 个 height、2,992 个 collision、1,128 个 effect 和 5,109 个 path 条目。
height/collision 中的 Base64 载荷尚未完成语义解码；这里的 **paths 是 prefab 资源路径**，不等于导航网格。
要生成可行走图仍需恢复其编码、格点尺度、offset、compound 变换和活动层。

### 3.5 当前亚洲服世界配置

本地有当前 `worldsettings_asia.bin`，没有 `world_asia.bin` 和区域 patch。
公开仓库 HEAD 更新于 2026 年，但 `world_asia.xml/world_asia_patch.xml` 的逐文件内容历史停留在 **2023 年**。
本次没有将它们应用于当前 world。
最终 Asia world 的加载选择和在线实例清单仍需当前客户端加载逻辑或运行证据确认。

### 3.6 地图动态消息与地块/建筑状态

本版本 `GetClusterData=40` 的响应含 byte[]；`GetClusterMapInfo=197` 请求 key0 为 string，响应类型 `afb` 有 **92 个接收字段**。
`n.b(g7q)`，VA `0x180a9c910`，检查响应成功后通过 `ci9.b<afb>` 解析并交给 callback。
其中包含位置数组、Guid/名称、访问权限、阵营堡垒状态、宝箱状态、旋转与时间字段；[元数据专题](metadata/REPORT.md) 保留全部键、类型和 native offset。

冻结 AODP 解码器解释 GetClusterMapInfo 的部分键如下；这些语义来自社区实现，当前本地字段类型提供对应线索，尚未逐项追完客户端 callback：

| 参数键 | 社区字段含义 | 当前本地接收类型 |
| --- | --- | --- |
| 0 | ZoneID | string |
| 17 / 18 / 20 | 建筑类型 / 坐标 / 耐久 | int[] / float[][] / long[] |
| 22 / 23 / 24 | 食物 / 奖励 / silver | long[] |
| 25 / 31 | 所有者 / 权限 | string[] / AccessRights.Preset[] |
| 33 / 34 | 关联费 / 公开费 | long[] |

该响应是区域地图状态路线，不能理解为地形全量或全服人物列表。
AODP 源码会上传 `mapdata.ingest`，但当前公开文档和 REST 没有已确认的完整地图状态消费接口；源码存在不证明公开生产部署可用。

## 4. 市场：物品、位置、报价、订单与历史

### 4.1 物品与市场主数据

6,049 个物品直接定义展开为 **12,237 个唯一字符串 ID**，包括附魔和日记变体。
[market-items.jsonl](static-data/catalogs/market-items.jsonl) 保留 item_id、Tier、类别、重量、堆叠、制作属性、附魔和名称。
40,070 个本地化键已经输出；636 个展开 ID 缺所选中英文名称，保持缺失。
`dumper_index` 复现社区 ItemExtractor 顺序，但**尚未证明它就是本版本网络物品数字 ID**。

品质 `quality=1..5` 为 Normal、Good、Outstanding、Excellent、Masterpiece；`T4_BAG@1` 中的 `@1` 是附魔，需独立保存。

| AODP 市场 | 市场 location ID | 与城市的区别 |
| --- | --- | --- |
| Thetford | 7 | 本地格式 `0007`，城市 `0000` |
| Lymhurst | 1002 | 城市 `1000` |
| Bridgewatch | 2004 | 城市 `2000` |
| Martlock | 3008 | 城市 `3004` |
| Fort Sterling | 4002 | 城市 `4000` |
| Caerleon 普通市场 | **3005** | 城市 `3003` |
| Black Market | **3003** | 单独的黑市类型，需与普通市场分开 |
| Brecilien | 5003 | 城市候选 `5000/5001` |

完整本地 106 个市场定义中还有新人、portal、rest、hideout、岛屿等配置，不保证均由公共 API 支持。
市场 UUID、城市 cluster ID、市场 cluster ID 和公共 API location 是不同身份；对照 [location.rb](public-data/sources/aodp-server/lib/location.rb)。
默认 locations 仅有 8 个主要市场，省略参数不代表全部市场；Smugglers Network 还有展开/归并规则。

### 4.2 已确认的 East REST

本任务市场 host：**`https://east.albion-online-data.com`**。
West 为 `https://west.albion-online-data.com`，Europe 为 `https://europe.albion-online-data.com`；所有数据键带 server。

已返回 HTTP 200 的请求：

```text
/api/v2/stats/prices/T4_BAG.json?locations=Caerleon,Bridgewatch&qualities=1
/api/v2/stats/history/T4_BAG.json?date=2026-10-01&end_date=2026-10-12&locations=Caerleon&qualities=1&time-scale=24
```

| 响应 | 字段 | 应如何解释 |
| --- | --- | --- |
| prices | item_id、city、quality、sell_price_min/max、buy_price_min/max、各自 `*_date` | 社区最新聚合时间桶中的报价范围；非全盘口、非成交价 |
| history | item_id、location、quality、data 中 item_count/avg_price/timestamp | 成交/售出历史统计汇总；非每笔交易或当前报价 |

REST `time-scale=1/6/24` 是小时聚合尺度；客户端历史 wire 的 0/1/2 是 Hours/Days/Weeks 枚举，不能直接互换。

**价格 0 和日期 `0001-01-01T00:00:00` 是缺测占位**，展示层转 missing/null，并保留 raw 值。
不能据此断定免费、无订单或无供需。本次 T4_BAG 样例就存在这种缺测。
历史按服务桶时间保存；日期范围可能返回前一天边界，不能伪造修正源时间。

源码中的 orderbook 候选端点：

```text
/api/v2/stats/orderbook/T4_BAG.json?locations=Caerleon&qualities=1
```

**East 实测 HTTP 404**，因此不作为当前可用的盘口方案。
源码存在端点不能证明生产部署已上线。

### 4.3 如何覆盖全部需关注物品

从本地 item_id 清单选交易目标 → 明确所有市场和品质 → 按编码后 URL≤4,096 字符分批 → 串行请求并缓存 → 按 `(server,item_id,location,quality)` 更新有效观察。
保留每批请求、返回组合、缺测和观察时间。遍历完整物品清单仍只能取得项目已观察的数据。

文档限流为 180 次/分钟、300 次/5 分钟。请求声明 gzip，遇到 429 按 Retry-After/RateLimit-Reset 退避。
冻结源码还对未声明服务支持的压缩方式的请求限制 6 次/分钟，部署值可以变化。
不要并行启动多个查询进程绕过总体请求节奏。

### 4.4 社区全库与持续订单观察

East 备份：[https://www.albion-online-data.com/database-east/](https://www.albion-online-data.com/database-east/)。
恢复 daily backup 到自己的新数据库，再按需求将较早月份历史按月份降序导入。保存下载 URL、日期、哈希、dump schema 和导入日志；恢复说明及历史主键冲突详见公开专题。
本次仅核对目录，未下载/导入大备份。

公开文档中的 East NATS：

```text
nats://public:thenewalbiondata@nats.albion-online-data.com:24222
marketorders.deduped
markethistories.deduped
```

这是项目公开共享的订阅凭据。已有 NATS CLI 时可用公开专题中的 `nats sub` 示例；本次未安装或连接。
新订阅不能自动补齐此前订单。文档去重写 5 分钟，冻结 server Redis TTL 为 10 分钟，实际部署未观察；自行记录 last_seen、断线和缺失范围。

订单通常含 `Id/ItemTypeId/ItemGroupTypeId/LocationId/QualityLevel/EnchantmentLevel/UnitPriceSilver/Amount/AuctionType/Expires`。
按冻结 AODP server 的处理实现，原始订单和 ingest 中 `UnitPriceSilver` 放大 **10,000 倍**；server 在发布 deduped 前已经除以 10,000，REST 也使用换算后 silver。本次用户账户 wire 的实际金额单位尚未观察。
**按来源记录单位，deduped 不要重复除以 10,000。**
某些历史时间使用 C# ticks：`unix_seconds=(ticks-621355968000000000)/10000000`。
保留大整数/decimal 或原始字符串，避免 JavaScript Number 精度损失。

市场数据由贡献者正常加载的结果形成，覆盖不完整。订单后来未被观察可能是成交、撤单、到期或无人加载，不能从消失直接生成成交记录。
部分账户的市场流量据项目 FAQ 仍加密；本次未检查用户账户状态。GameData 静态 DES 常量与在线市场保护无对应关系。

### 4.5 当前客户端拍卖消息

本版本业务操作号：`AuctionGetOffers=81`、`AuctionGetRequests=82`、`AuctionGetItemAverageStats=95`、`AuctionGetItemAverageValue=96`、`AuctionGetLowestOfferPrices=97`。
当前客户端的 `dgc..ctor(string)`，VA **`0x180b20e70`**，通过 JsonFx 将字符串解析为 `Albion.Common.Auction.AuctionDTO`，存入 this+0x10。
该泛型实例由元数据 usage 对应到共享原生实现，类型/字段表和证据见元数据专题。
`ad1/ad2` 响应静态参数 key0 为 string[]，key1 为 bool；响应处理器 `0x180b27010/0x180b27340` 遍历字符串数组并创建 `dgc`。
DTO 有 21 项字段，包括订单/金额/数量/物品/品质/附魔/到期/地点，还定义卖家与买家 ID/名称、取货状态和 ReferenceId。
**字段存在不保证公开市场响应会填充买卖方身份或所有值**，本次没有观察用户账户响应的脱敏和空值行为。
请求参数、分页、过滤和返回字符串数组只描述客户端加载的结果，不建立服务器全盘口完整性。

## 5. 地图人物信息

### 5.1 公开玩家档案：已可读取

亚洲服 host：**`https://gameinfo-sgp.albiononline.com/api/gameinfo`**。
已核对 search、players、kills、deaths、events、guilds、alliances，均有 HTTP 200 样例。

| 所需信息 | 公开字段/入口 | 本次样例的限制 |
| --- | --- | --- |
| 角色姓名与永久公开 ID | `/search?q=...`、`/players/{Id}` 的 Name/Id | 搜索不是全部玩家人口导出 |
| 公会/联盟 | GuildName/Id、AllianceName/Id/Tag；guilds/alliances | 公开档案快照，非实时地图名单 |
| 外观与统计 | Avatar/Ring、KillFame/DeathFame、LifetimeStatistics | 不能反推当前位置或当前活动 |
| 装备/IP/背包 | Player schema 与 event 的 Killer/Victim/Participants | 玩家档案样例装备全 null、IP0；不能解释成当前裸装 |
| 历史战斗 | kills/deaths/events 的 EventId、TimeStamp、装备与 BattleId | 装备只代表事件时刻；Location 样例均 null |
| 当前坐标、所在区域、在线状态 | 本次档案没有相应字段 | 没有可用的实时人物地图入口证据 |

`KillArea=OPEN_WORLD` 只表示事件分类，不是坐标。
部分 kills/deaths 请求 `limit=1` 仍返回 10 条，应按真实 count、EventId 和时间范围记分页覆盖。
公开接口会变化；历史官方说明不提供此 GameInfo API 的公众支持，当前可用性以本次样例为准。

### 5.2 当前客户端人物业务消息

当前元数据直接恢复操作/事件属性，类型与业务编号有对应证据；不是通过字符串猜测。
普通 Photon app event 在参数 key **252（0xFC）** 中保存 short 业务事件号；operation response 的 key **253（0xFD）** 是 short 操作号，255 可为请求关联号。
outer Code=3 是特殊 Move 路线，outer Code=2 还有特殊处理。
**NewCharacter=29 是业务事件号，不代表 Photon 外层 wire byte 就是 29。**

当前字段排序链：父类型先 → 每一级 public instance declared fields → 排除特定 `en` 属性字段 → 按 metadata token 排序 → byte 参数键从 0 递增。
证据 `AO-META-PARAMETER-ORDER-001` 及 [protocol-parameter-schema.json](metadata/results/protocol-parameter-schema.json)。
以下类型是接收对象的 CLR 字段类型，**native offset 是对象偏移，不能当作网络包字节偏移**。

| 消息 | 当前类型 / 业务号 | 关键静态参数 | 用途和未确认部分 |
| --- | --- | --- | --- |
| NewCharacter | atd / 29 | key0 long 实体 ID；1 姓名；8/9 公会名/Guid；51/52 联盟名/Guid；40 装备索引 int[]；14/15 Vector2；16/17 byte[]；56 FlaggingStatus | 建立人物对象；公会/联盟有强静态语义证据，Guid7永久身份与装备槽对应仍未知 |
| Move | as2 / 3 | key0 long；1 GameTimeStamp；2 Vector2；3/4 float；5 Vector2；6 long；7 bool；8 byte[] | 更新已知对象的位置/运动；压缩字节及各运动字段完整语义未恢复 |
| Leave | arp / 1 | key0 long | 对象离开路线，保留事件和缓存失效时间 |
| CharacterEquipmentChanged | alv / 90 | key0 long；1/5 时间；2 装备索引 int[]；7 int[]；3 long；4 int；6 byte；8/9 bool | key2 与人物创建 key40 共用装备处理；槽位、numeric item 映射和各标记含义需进一步核对 |
| HealthUpdate | ap9 / 6 | key0 long；1 时间；2/3 float；4/5 byte；6 long；7 short；8 bool | 生命相关更新；不能仅凭 float 类型把值命名为 HP/最大 HP |
| HealthUpdates | aqa / 7 | key0 long；1 时间数组；2/3 float[]；4/5 byte[]；6 long[]；7 short[] | 批量生命相关值；数组长度/对应和条件缺失需实际解析验证 |

完整 NewCharacter **66 项**、Move **9 项**、装备 **10 项**、单次生命 **9 项**、批量生命 **8 项**表见 [元数据报告](metadata/REPORT.md)。
Move/装备/Health 的 key0 来自父类 `avk.ddn`，不能只按自有字段序号推键。
NewCharacter 坐标在本版 key14/15 有 Vector2；不采用旧示例的 key12 或 Move byte[] 9/13 偏移作为本版实现。

`GetCharacterEquipment=143` 请求参数为 Guid、bool；响应为 Guid、long[]、byte[]、float。
它为装备查询提供代码入口，但请求 Guid 与 NewCharacter Guid 的对应、权限和返回字段语义未全部证明。
GuildUpdate103、GuildPlayerUpdated104、GuildMemberWorldUpdate106、PartyPlayerUpdated238、MountHealthUpdate312 还有不同订阅范围，不能合并成全球全部人物状态。

### 5.3 已确认的人物创建与小地图值流

`cjb.d(g7s,short)`，VA **`0x180aacaf0`**，使用 `ci9.b<atd>`，从 key0/`atd.cvx` 查询本地 `ciy` 实体表；不存在则创建 `cp0` 并加入。
因此 key0 已确认是本地实体 ID，**不能直接当作 GameInfo 永久 player ID**。
创建时传递 key1 string、key4 int、key7 Guid、key64/65 string。
后续静态追踪将 key1/`cvy` 关联到 `cp0.af(string)`（`0x180bd9010`），由姓名处理函数存入两个名称字段，`CharacterName.UpdateText` 通过 getter 读取显示名，因此 **key1 已确认是人物显示名称的来源**。普通分支分别保存输入与处理后的名字；特殊匹配分支还会先处理输入，不能把其中一个字段始终当作未经处理的原始姓名。
key8/9 为公会名来源/Guid，key51/52 为联盟名来源/Guid：值流进入关系判断和名称显示，并由 `OtherGuild=9/OtherAlliance=10` 命名枚举与原始颜色跳表交叉定位。名称用途已确认，Guid 含义为强静态语义推导；本次没有服务器样本。
key7 Guid 存入角色基类的另一身份字段，但与 GameInfo 永久 ID 的对应尚未证明。key10/13 string 的完整语义仍未知。

NewCharacter key40 与 CharacterEquipmentChanged key2 进入同一个 `cp0.af(int[])` 装备 setter，逐整数查询客户端物品表，负值清空。
因此已确认装备物品索引数组；**数组 index 到 EquipmentSlot、数字索引到本地 item_id 的精确映射仍需核对**。
key43 与装备变更 key7 进入同一个第二对象索引表处理函数，外观/skin 仅是候选。
HealthUpdate key2/3 经生命状态模型更新到滑条，尚未完整恢复插值和最大值计算，不把它们直接命名成当前 HP/最大 HP。
以上摘要及原始 vtable/跳表/字段证据见 `AO-META-PLAYER-SEMANTICS-001`，[player-semantic-mapping.json](metadata/results/player-semantic-mapping.json)。

`csc.v(g7s)`，VA **`0x180becdc0`**，同样解析 atd。key16 非空时，从中取两个 4 字节片段，经 `e0.a` 和 `BitConverter.ToSingle` 重建 key14，并将转换后的片段写入 key17 缓冲，再重建 key15；后续使用 key14 的两个 float 更新实体位置。
这证明二维位置及字节转换路线存在；key17 在该分支参与写入，不能把 key16/17 简单当作两份独立的坐标输入，也尚未恢复 `e0.a` 算法或物理网络编码。

`MinimapBase.UpdateInterestAreaOnlyMarkerPositions`，VA **`0x18145ae00`**，遍历已有 marker，按 objectId 查询本地实体并调用 `MapLabel.SetPosition`。
`UpdatePartyMemberPositions(string)`，VA **`0x18145c270`**，是队伍标记路线。
这些路径依赖客户端已有的实体或订阅状态，未建立具体服务器兴趣半径。

上述人物函数已有独立 REA 伪代码与汇编；逐项语义和条件分支复核见 [人物原生复核](native/PLAYER-REVIEW.md)。对象布局与静态参数键仍需要元数据配合解释，REA 的匿名函数名不能单独确定字段含义。

### 5.4 如何维护人物地图数据

若进一步处理指定来源的离线消息记录，先冻结 client hash 和解析版本，再按以下状态模型保存：

1. 开始新 session/cluster_instance，记录加入时间与实际 cluster 身份。
2. NewCharacter 建立 entity_id 对象，保存每个字段来源、已确认含义、位置时间和身份候选。
3. Move、装备、生命更新只修改对应对象，保留 sequence/timestamp/last_seen 和缺字段状态；不能用缺值覆盖成零或裸装。
4. Leave/区域切换使当前对象记录失效，保存历史观察；丢事件或重连时标记覆盖缺口，不能把旧位置继续当实时位置。
5. 用实际 local/minimap 坐标规则投影；公开 GameInfo 只补充有明确对应证据的身份和历史资料。

推荐人物字段：`server/session_id/cluster_instance/entity_id/name/guild/alliance/position/coordinate_space/equipment/health/status/observed_at/last_seen/source/coverage`，未确定语义使用原字段名。
本次尚未提供可运行的实时人物采集器，也没有读取全图隐藏角色的方法证据。
可取得的集合应限定为**所选客户端实际收到的对象**；隐藏、跨区、兴趣限制和消息缺失都会影响覆盖。

## 6. 数据怎么组合成可复用数据库

建议先保留原始载荷，再建立以下逻辑表。此处是数据设计建议，不是已经完成的应用。

| 表 | 推荐键 | 核心字段 |
| --- | --- | --- |
| client_snapshot | client SHA-256 / version | 元数据 hash、提取时间、来源 |
| cluster_definition | version + cluster_id | 全部原属性、布局、出口、图层、来源 XML |
| template_candidate | version + source_path | ref、变体、layer、tile/compound 和变换 |
| item_definition | version + item_id | Tier、附魔、名字、品质语义、类别；numeric ID 单独验证 |
| market_definition | version + market_uuid | 城市/市场 cluster、类型、AODP location 映射 |
| market_observation | server + source + observation ID | item/location/quality/price/amount、raw unit、时间、missing |
| order_observation | server + order_id + observed_utc | topic、原始 hash、规范金额、expires、覆盖区间 |
| public_player | server + GameInfo player ID | 名称、公会、联盟、统计 snapshot_at |
| entity_observation | server + session + cluster_instance + entity_id + time | 人物出现/离开、位置、装备、生命、last_seen、字段来源 |

静态定义描述“可以是什么”，观察记录描述“在什么时间收到什么”。
人物短 entity_id 不应直接连接 GameInfo player ID；先保留明确的 UUID/名称关联证据和置信度。
市场位置和物品数值 ID 也不要只凭名字或索引顺序关联。
地图投影需要保留坐标空间和变换版本，避免将世界图位置与 Unity 位置混用。

## 7. 证据、分析工具与复用

实际原生目标为 `game/GameAssembly.dll`；`game/Albion-Online.exe` 是 Unity 播放器入口，业务代码主要在 GameAssembly.dll。没有可直接反编译为普通 C# 的物理 Managed assemblies。

| 输入 | 字节数 | SHA-256 |
| --- | ---: | --- |
| GameAssembly.dll | 89,043,456 | `19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202` |
| global-metadata.dat | 22,381,812 | `b0cffb9a6945366c0057390c2144edbfb7b9fb0b9a2f327ed8517e3051b7684b` |

证据标记：

- `ev_*`：真实 REA/Ghidra 返回，完整记录保存在 `native/evidence/` 和导出的 bundle。
- `AO-META-*`：当前 v39 元数据和 PE/Capstone 辅助分析，属于本地脚本证据。
- 静态 XML/Unity：按 manifest 中源文件及 SHA-256 定位，不冒充 REA Evidence。
- `PUB-*`：公开 HTTP/源码证据，有 UTC、URL、状态与落盘 hash，不冒充游戏运行证据。

主要公开结论的直接证据：

| 结论 | 公开证据 ID / 文件 |
| --- | --- |
| East 报价可访问及缺测占位 | `PUB-samples-prices.json`，[原响应](public-data/samples/prices.json) |
| East 历史汇总可访问 | `PUB-samples-history.json`，[原响应](public-data/samples/history.json) |
| 玩家档案字段与当前坐标缺口 | `PUB-samples-player.json`，[原响应](public-data/samples/player.json) |
| 战斗装备快照与 Location 为 null | `PUB-samples-events.json`，[原响应](public-data/samples/events.json) |
| 市场 location 规范与黑市区别 | `PUB-sources-aodp-server-lib-location.rb`，[冻结源码](public-data/sources/aodp-server/lib/location.rb) |
| 社区备份目录 | `PUB-sources-database-east-directory.html`，[目录快照](public-data/sources/database/east-directory.html) |

全部 70 条公开请求、失败探测与原始来源 hash 见 [sources-manifest.json](public-data/sources-manifest.json)。

REA 全局安装为 npm **6.4.0 / 170 tools / skill v37**；当前 turn 未直接暴露 REA MCP，使用独立持久 stdio 连接。
为解决大型 IL2CPP 的默认分析停滞，本次在调查目录使用冻结源码修订
`d13c9f840d36a321a56917e75c48afcb8d72920e` 的隔离构建，支持禁用指定分析器。
**npm 发布版不实现 `REA_GHIDRA_DISABLED_ANALYZERS`**；不能对发布版仅设置变量就声称生效。
隔离构建复用已安装的兼容 ABI1/NodeAPI8 Windows addon，native 源码不同，来源和能力边界见本机环境记录；全局安装和 Codex 注册仍指向发布版。
实际函数分析成功/失败、profile digest 和清理结果见原生专题。
第三次仅禁用 `Non-Returning Functions - Discovered` 的导入在 1,200 秒启动期限超时并完成清理。
第四次调查配置同时禁用 `Decompiler Switch Analysis`、`x86 Constant Reference Analyzer`、`Function ID`，启动期限 2,400 秒，heap 4G。
其 profile digest 为 `96664c5e569b8baa0db21aa3e46d10a6d2eaff5fe5324a1fbaa54ee3a5a61643`。
这是 Ghidra 及23个函数 Evidence 的 profile；overview 自身使用 composed profile，其 upstream 指向此 Ghidra profile，详见原生专题。
这些设置减少全局 switch 补建、常量地址引用和已知库函数识别；具体函数仍独立反编译，结果不能声称与默认 profile 完全等价。
第四次 overview 实际耗时 1,914.01 秒，随后 23 个函数均成功返回，完整 bundle 为 24 条记录。导出与 `close_binary` 成功，调查持有的进程和临时目录均已清理，见 [lifecycle-final.json](native/lifecycle-final.json)。bundle 导出的 `unknowns=0` 是记录格式的计数，不表示本报告没有未解问题。

运行来源还需保留一个细节：Node 服务启动时使用隔离 HEAD 构建，而 Ghidra 后续编译 postScript 时读取到了本地兼容 Java 扩展，实际走默认分析分支；编译产物及源码版本分别保存在 [第四次运行来源](native/attempt-profile-four)。目录内的 `seeded-noanalysis` 扩展已构建，**本次没有使用该模式**，不把它的限制分析身份混入上述 profile。

主要原生结论的直接证据（完整 ID 及覆盖范围在原响应中）：

| 结论 | REA Evidence ID / 原响应 |
| --- | --- |
| 事件包装 key252 与特殊 Code3 路线 | `ev_69c27a66bb638371ce82cf0a1a6606a85e68720fbcee137980a92d6a5d5754c8`，[响应](native/evidence/photon-event-ingress.json) |
| 响应 key253 业务号、key255 请求关联 | `ev_63b88a60dd86d13f0eab1e35b651715eee6ab6e231b059512fc60b044419233e`，[响应](native/evidence/photon-operation-response-ingress.json) |
| 拍卖 JSON 构造、解析结果写入对象 | `ev_d5eba8610f42f8bad8341201880b2a098503b94a5ef7f1547e6de1c0b641fcc2`，[响应](native/evidence/auction-dto-json-constructor.json)；AuctionDTO 类型由 metadata generic usage 确认 |
| 地图状态成功解析后交给回调 | `ev_fa6bb27327d58d6be58cb7b58ad91541df1995056aa27601a65dfd57d744b96c`，[响应](native/evidence/cluster-map-info-response-handler.json) |
| 人物对象创建和本地实体表 | `ev_1432a2360489cab337f425cde48c2fe3f515b2e1eb2919145f4d50970f6e5657`，[响应](native/evidence/new-character-message-handler.json) |
| 人物状态与坐标转换 | `ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3`，[响应](native/evidence/new_character_state_application.json) |
| 装备索引逐项处理 | `ev_7fb7b0995d7fc471f6c8a8c73f7b0626922f7f0018508a15e5c6d6e718f37c88`，[响应](native/evidence/equipment_indices_setter.json) |

已查询文件名中的两个 `router` 实际分别对应 Died 事件与 Join 操作响应的特定处理器；本次没有据此声称恢复全部业务分发。共享 getter/selector、间接回调、非连续函数 body 和值流截断的详细限制见原生专题与 [SUMMARY.json](native/results/SUMMARY.json)。

大目录读取规则：先读 summary/manifest，再流式筛选 JSONL；不要一次载入百万条放置或 Unity 对象索引。
更新客户端后应选新输出目录重新冻结 hash、提取和映射，不能把本次协议键或 numeric ID 直接沿用到新版本。

## 8. 问题完成度与下一阶段

| 原问题 | 本次回答 | 剩余边界 |
| --- | --- | --- |
| 如何取得地图所有数据 | 安装静态定义已全量提取，地图/模板/语义/图像路径明确 | 当前 Asia 激活世界、动态实例、碰撞/像素解码和最终投影仍需对应证据 |
| 如何取得市场数据 | East 报价/历史可访问，全库备份与订单 feed 路线明确，客户端拍卖结构已分析 | 社区覆盖不完整；orderbook 404；账户加密、NATS 实际部署和服务器全部订单未建立 |
| 如何取得地图人物信息 | 公开档案入口及当前客户端人物事件、字段与生命周期分析 | 实时样本、坐标压缩格式和观察半径仍需验证；全图隐藏人物没有可用入口证据 |

实施顺序：复用现有静态主数据 → 按目标查询 East 行情与档案 → 确需社区全量时恢复备份并接 feed → 若进一步处理地图动态状态，用指定来源的离线记录核对本版本字段、ID、坐标和对象生命周期。
这份报告说明已经取得的产物和下一步具体入口；未验证的运行状态保留为未知，不用静态计数补齐。
