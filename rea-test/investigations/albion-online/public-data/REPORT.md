# Albion Online Asia / East：公开地图、市场与玩家数据调查

调查日期：2026-10-13，Asia/Singapore。用户安装版本为 `1.32.030.345319`。
本专题覆盖公开来源与少量接口样例；本地完整资源、元数据和原生反编译由同级报告提供。
本目录没有启动游戏、安装抓包工具、连接 NATS 或下载数据库备份。

## 1. 可以取得的“全部”是什么

| 所需数据 | 已确认获取方式 | 可以达到的覆盖 | 不能由此确认的内容 |
| --- | --- | --- | --- |
| 世界地图、区域、出口、模板、资源与怪物规则 | 当前本地 GameData 解码；版本冻结的 ao-bin-dumps 作为参考 | 安装包携带的静态定义可以递归全量导出 | 今天亚服实际开放的实例、即时采集余量、随机刷新结果 |
| 全部物品与市场位置定义 | 本地 items/localization/world；AODP location 映射 | 本地枚举及附魔变体可列出 | 每个可交易物品在每个市场当前都有报价 |
| 市场价格和成交统计 | East AODP REST | 社区贡献者观察到的价格/历史 | 服务器权威全部订单、每笔成交及完整当前盘口 |
| 社区项目所保存的全量市场数据库 | East daily backup + 历月 history | 项目已保存的数据全量 | 项目未观察、未保存的数据 |
| 持续订单观察 | East 公共 NATS deduped subjects | 订阅后贡献者新提交的订单观察 | 开始订阅前全部订单、隐藏订单、订单消失的实际原因 |
| 公开玩家、公会、联盟、击杀资料 | Asia GameInfo | 查询目标的公开档案与部分历史事件 | 当前地图全部玩家、当前坐标/在线状态、隐藏角色 |
| 地图人物的身份、移动、状态 | 本地客户端解码器与受授权的离线记录 | 客户端实际收到的对象事件；本次只做静态分析 | 未发送给客户端的全图/全球人物状态；本次没有实时样例 |

“本地静态完整”“社区已保存完整”和“服务器实时完整”必须分别记账。搜索玩家名称并不能枚举全部人物；对所有物品循环调用价格 API 也不会补出未观察的订单。

## 2. 证据与版本

完整 URL、HTTP 状态、抓取 UTC 时间、大小、SHA-256、覆盖范围在
[sources-manifest.json](sources-manifest.json)。`PUB-...` 是本专题的公开证据 ID，不是 REA 的原生 Evidence ID。
保存的 HTTP 内容已经解压 Content-Encoding，哈希对应落盘字节，不是压缩后的传输字节。

| 来源 | 固定修订 | 日期 UTC | 用途 |
| --- | --- | --- | --- |
| ao-data/albiondata-client | `c6edd9456bec4657755c25f173e8ebc7a31a17f7` | 2026-10-12 11:29:14 | 订单、历史、地图建筑解码与事件 enum |
| ao-data/albiondata-server-rails | `5f8fb43a60a39e4a04d4fb313ad01a161e032ef5` | 2026-09-29 10:03:34 | 价格聚合、单位换算、市场映射、限流、候选 orderbook |
| ao-data/ao-bin-dumps | `1a469e53de028c5f85cdd80505f10cbd0a2515cf` | 2026-10-12 11:25:12 | 社区静态资源参考 |
| ao-data/albiondata-bin-dumper | `e73a181eafd0db785f889aba8dbe7c69f83b71ad` | 2025-10-19 17:56:16 | 静态 `.bin` 导出算法 |
| ao-data/albion-network | `ff601f07de38259f9ec9f4a25960183fa5c9e4a4` | 2021-11-28 | 历史人物协议示例，仅供分类参考 |
| ao-data/AlbionPacketHandler | `15adef4f983ddf2d6971c94fe6aa2f4771a31698` | 2021-07-13 | 历史人物对象生命周期示例 |

少量样例的重要哈希：

| 本地证据 | SHA-256 |
| --- | --- |
| [价格](samples/prices.json) | `ba0ba30344e97bac788ffdb5adee6aad4b2171ec1f2a5ca5e3693aa9a4b11028` |
| [历史](samples/history.json) | `f87ec8e26a2d43062d431daad44580a392057e9b59f339ac5eaf08a789fb3901` |
| [玩家档案](samples/player.json) | `b2b34eef0a0ce4ee1402080e85f3e8e7bd3439614598767c8407016d36f4742b` |
| [击杀事件](samples/events.json) | `637ae887bf4ade05130b7699f20aee99c6f44155f79e0bcadac7afcf188e457b` |
| [市场位置源码](sources/aodp-server/lib/location.rb) | `6d4d59b4ab21bcc1e97135631ebc2631e15e1c91646e9b6d2b4ae501e0782f77` |
| [订单去重与单位换算源码](sources/aodp-server/app/services/market_order_dedupe_service.rb) | `106bf00463765d14b5d11cb5ccffe1ea1cf0e6f02807aa679932ef818e47c788` |

公开网页和 API 会变化。源码行为仅证明该修订的实现，不能独自证明今天服务端已经部署该实现。

## 3. 静态地图与物品：优先当前安装包

### 3.1 现有本地导出

本次安装包的 **3278 个 GameData `.bin` 已成功解码**。使用同级
[extraction-manifest.json](../static-data/extraction-manifest.json) 复核输入/输出哈希；
[catalog-summary.json](../static-data/catalogs/catalog-summary.json) 给出结构化计数。

当前本地目录包含：

- 1427 个 cluster 定义，1336 个 enabled；1427 个世界引用布局均找到。
- 3223 个出口，1945 个 minimap markers，132 个 worldmap markers，106 个 marketplace 定义。
- 1910 个本地 template 文件、15384 个世界 template instances；不能将社区仓库历史模板文件数当成本地版本文件数。
- 159078 个普通 tile、9272 个 compound tile；资源、mobspawn、建筑、箱子和出口语义存在于 tile/template 子元素中。
- 6049 个物品定义展开为 12237 个唯一可识别物品 ID；索引遵循社区 ItemExtractor 顺序，数值网络 ID 映射仍需当前客户端证据确认。

可直接使用以下产物：

| 目标 | 文件 |
| --- | --- |
| 区域属性 | [map-clusters.jsonl](../static-data/catalogs/map-clusters.jsonl) |
| 区域连接图 | [map-exits.json](../static-data/catalogs/map-exits.json) |
| 地图与引用诊断 | [map-manifest.json](../static-data/catalogs/map-manifest.json)、[map-diagnostics.json](../static-data/catalogs/map-diagnostics.json) |
| 城市/市场/黑市映射候选 | [market-locations.json](../static-data/catalogs/market-locations.json) |
| 物品 ID、附魔、中文/英文 | [market-items.jsonl](../static-data/catalogs/market-items.jsonl)、[localization-en-zh.jsonl](../static-data/catalogs/localization-en-zh.jsonl) |
| 模板与 tile 放置 | [template-index.json](../static-data/catalogs/template-index.json)、[template-placements.jsonl](../static-data/catalogs/template-placements.jsonl)、[compound-tile-placements.jsonl](../static-data/catalogs/compound-tile-placements.jsonl) |
| tile 定义与几何 | [tile-definitions.jsonl](../static-data/catalogs/tile-definitions.jsonl)、[tile-geometry-tables.jsonl](../static-data/catalogs/tile-geometry-tables.jsonl) |
| 实例/刷新候选 | dynamicclusters、dynamictemplates、mists、resourcedistpresets、randomspawnbehaviors 等 JSONL |

读取顺序：`cluster/world.xml` → 按 `file` 找 cluster 布局 → 找模板实例引用、位置、active layers → 解析模板中的普通/复合 tile → 关联 tile 语义、资源/怪物定义。保留源路径、复合变换和 layer；相同短模板名可能有多个候选。仅取 cluster 文件中的几个坐标不足以恢复地图对象。

世界图坐标 `worldmapposition`、cluster 局部坐标、Unity XYZ、template/tile 原点与 minimap bounds 是不同坐标空间。需要按布局变换组合，不能当作经纬度，也不能直接把局部玩家位置叠加到世界图坐标上。

### 3.2 社区静态仓库

参考活跃的 [ao-data/ao-bin-dumps](https://github.com/ao-data/ao-bin-dumps)，以固定 commit 下载所需文件。`formatted/world.json` 只有 1427 个 `{Index,UniqueName}`，是索引表；它不包含地形、出口、坐标、玩家或资源实例。`formatted/items.json` 是便于使用的物品/多语言表，网络数值索引仍要版本核对。

完整静态地图参考在 `cluster/world.xml`、`cluster/*.cluster.xml`、`templates/**/*.template.xml`、`cluster/tiles.xml`。`minimapgendata.xml` 提供纹理名、layer、vMin/vMax 等生成信息，**并非地图位图本体**。`dynamicclusters.xml` 记录动态模板槽位，**并非今天在线的道路、迷雾、地下城实例及连接**。

### 3.3 亚服文件陈旧陷阱

不能因仓库 HEAD 更新于 2026-10-12 就认为所有 Asia 文件也是最新：

| 路径 | 最后内容修订 | 提交日期 UTC |
| --- | --- | --- |
| `cluster/world.xml` | `90b141e20f4677c2fe3083c519b7f748affa4c47` | 2026-09-23 |
| `worldsettings_asia.xml` | 同上 | 2026-09-23 |
| `cluster/world_asia.xml` | `ae5389007ad171d4fbe72e135748eac977959bf8`，提交说明 update20230329 | 2023-10-08 |
| `cluster/world_asia_patch.xml` | `5801fa9e1866a9a033be95079cd8618e963e9802`，update20230221 | 2023-10-08 |
| `worldsettings_asia_patch.xml` | `76bbd8145846207364835d4597814214148b6184`，update20230214 | 2023-10-08 |

逐文件最后提交响应已保存在 `sources/ao-bin-dumps/*-last-commit.json`。旧 Asia world 有 1262 个 cluster/863 enabled，当前 common world 与本地均为1427/1336。当前本地只有 common world 和现存 Asia settings，没有 world_asia/patch；不要把2023 patch 自动合并到2026 common world。

### 3.4 `.bin` 导出原理与更新方法

[BinaryDecrypter.cs](sources/bin-dumper/Extractor/Extractors/BinaryDecrypter.cs) 的编码流程为 DES CBC/PKCS7，key=`30 EF 72 47 42 F2 04 32`，IV=`0E A6 DC 89 DB ED DC 4F`，之后解压 GZip 得到 UTF-8 XML。本次本地脚本已将此方法应用于安装资源。这些常量只说明静态 GameData 格式，不是在线市场通信解密方法。

已有导出器 [extract_game_data.py](../static-data/extract_game_data.py) 和 [build_catalogs.py](../static-data/build_catalogs.py) 是当前任务可复用入口。若以后选用社区 .NET 工具，它的项目目标为 net7.0，实际递归实现位于 `Extractor/Extractors/BinaryDumper.cs`，并跳过 profanity。从其仓库根目录使用明确的 project：

```powershell
dotnet run --project .\CommandLine\CommandLine.csproj -- -d '<实际 GameData 目录>' -o '<新的输出目录>' -t Both -m Everything
```

这是可选重做方式，本专题没有安装 .NET 工具。文件型全量导出不等于当前全部 Unity 图形资产已经解包，也不等于所有随机地图已经实例化。

## 4. 市场数据：Asia/East REST

### 4.1 服务端隔离

| 区服 | AODP REST |
| --- | --- |
| **Asia / East（本任务）** | `https://east.albion-online-data.com` |
| Americas / West | `https://west.albion-online-data.com` |
| Europe | `https://europe.albion-online-data.com` |

AODP 是社区数据项目，与 SBI 官方 GameInfo 分开。不要使用不明确的默认 host 混合亚服、美服价格。物品名字可以相同，但订单/人物 ID 和数据的 server 必须一起保存。[AODP API 文档](https://www.albion-online-data.com/api)

### 4.2 已验证接口与字段

以下两个 East 请求返回 HTTP 200，原始 JSON 与 UTC 抓取时间已保存：

```text
https://east.albion-online-data.com/api/v2/stats/prices/T4_BAG.json?locations=Caerleon,Bridgewatch&qualities=1
https://east.albion-online-data.com/api/v2/stats/history/T4_BAG.json?date=2026-10-01&end_date=2026-10-12&locations=Caerleon&qualities=1&time-scale=24
```

价格响应字段：`item_id, city, quality, sell_price_min, sell_price_max, buy_price_min, buy_price_max`，四个价格各有 `*_date`。它是报价范围，**不是订单明细，也不是成交价**。品质 1..5 依次为 Normal/Good/Outstanding/Excellent/Masterpiece；附魔是 `T4_BAG@1` 等物品标识的一部分，不能与 quality 混用。

实测 T4_BAG/品质1：

| 市场 | sell min/max | buy min/max | 观察时间/含义 |
| --- | --- | --- | --- |
| Bridgewatch | 0/0 | 1/2087 | buy 时间2026-10-12 11:00；sell 日期 year0001，缺测 |
| Caerleon | 0/0 | 0/0 | 日期全部 year0001，缺测 |

**`0` 与 `0001-01-01T00:00:00` 是无观察值的占位符，不能解释为免费、真实零价，或市场没有订单。** 给 UI/分析层转换成 missing/null，并保留 raw 值。价格时间是观察/服务聚合时间，不能解释为交易所保证的实时快照。

冻结 [market_data_service.rb](sources/aodp-server/app/services/market_data_service.rb) 从近一天记录中按 item/location/quality/auction_type 选择最新 **5分钟时间桶**，再算 min/max；它没有返回全深度。清理模型还删除已过期或一天未更新的订单。因而不能将 max/min 差值直接解释为全市场真实价差，不能把缺测推断为无供需。

历史响应字段：`location, item_id, quality, data[]`；每条包含 `item_count, avg_price, timestamp`。历史是售出/成交统计汇总，不是当前 buy/sell 订单。API `time-scale=1/6/24` 表示小时聚合跨度，24 为日。

样例开始日期2026-10-01、结束2026-10-12，返回第一点2026-09-30、最后2026-10-11。冻结服务对日聚合将00:00前移一分钟归入前一天，因此日期边界须按聚合语义处理。不要在入库时擅自把这些日期加一天；保留服务原值和所请求范围。

其他**文档列出、本专题未逐个请求**的接口：

```text
/api/v2/stats/charts/<逗号分隔物品>.json?date=YYYY-MM-DD&end_date=YYYY-MM-DD&locations=...&qualities=...&time-scale=6
/api/v2/stats/gold.json?count=2
/api/v2/stats/gold.json?date=YYYY-MM-DD&end_date=YYYY-MM-DD
/api/v2/stats/festivities
```

### 4.3 市场 ID 与城市 ID

REST `locations` 使用 AODP 的市场 ID/名称。city cluster ID 不能无条件当成 market ID，尤其 Caerleon：

| 市场 | AODP ID | 注意 |
| --- | --- | --- |
| Thetford | 7 | 原始市场可能为字符串 `0007`，保留 raw 标识 |
| Lymhurst | 1002 | 城市 cluster1000 |
| Bridgewatch | 2004 | 城市 cluster2000 |
| Caerleon 普通市场 | **3005** | 城市 cluster3003 |
| Black Market | **3003** | 与普通 Caerleon 市场分开 |
| Martlock | 3008 | 城市 cluster3004 |
| Fort Sterling | 4002 | 城市 cluster4000 |
| Brecilien | 5003 | 城市 cluster5000 |

完整有效清单、portal合并和 Smugglers Network 归并见冻结的
[location.rb](sources/aodp-server/lib/location.rb)。默认 locations 只选8个主要市场；省略参数不等于所有市场。源码会把若干 Smugglers Network 请求展开到共同网络集合，不能认为返回地点总与输入一一对应。

### 4.4 已提供单次查询工具

[fetch_public_data.py](fetch_public_data.py) 只依赖 Python 标准库。默认为 East；支持 `prices/history/player/search` 四种模式，需要明确目标和输出。

从本工作区根目录使用已存在的 Python：

```powershell
$albionPython = 'C:\Users\ludy\AppData\Local\Programs\Python\Python314\python.exe'
$albionFetch = '.\investigations\albion-online\public-data\fetch_public_data.py'
& $albionPython $albionFetch prices --items 'T4_BAG,T4_BAG@1' --locations '3005,2004' --qualities '1,2,3,4,5' --output '.\investigations\albion-online\public-data\manual\bag-prices.json'
& $albionPython $albionFetch history --items 'T4_BAG' --locations '3005' --qualities '1' --date '2026-10-01' --end-date '2026-10-12' --time-scale 24 --output '.\investigations\albion-online\public-data\manual\bag-history.json'
```

工具成功时保存原始 JSON 和 `<output>.metadata.json`，其中有 URL、server、UTC日期、HTTP状态、请求尝试、响应头、SHA-256 与缺测说明。默认拒绝覆盖，输入物品/地点会编码，编码后URL上限4096。发送 `Accept-Encoding: gzip`，每次尝试先等待2秒，429按 Retry-After/RateLimit-Reset 退避，最多4次；若服务器要求等候超过300秒，则停止自动重试并记录期限，留待以后再查。payload/gzip 解压均有大小限制。

网络、响应长度、gzip或JSON解析失败时返回非零，失败记录保存为唯一的 `<output>.failure-<id>.metadata.json`，含 `result_status=failed/output_written=false`。即使使用 `--overwrite`，请求失败也保留原有 raw 和原有同名 metadata，避免旧数据与新失败来源配对；实际收到响应时即保留HTTP状态和响应头。成功覆盖才更新原有配对文件。输出目录的权限或写入错误仍会阻止文件保存；读取数据前应核对配对metadata的 `sha256`。

**这个新工具本次未运行测试，也未执行新的查询；本目录已保存的接口样例来自 fetch_sources.py 的只读研究请求。** 若以后多批次调用，顺序运行并使用明确批次清单；不要并行启动多个进程突破总体速率。

官方公布180请求/分钟、300请求/5分钟；URL≤4096字符。持续服务需启用gzip。冻结 `rack_attack.rb` 还对无压缩声明的请求额外限为 **6请求/分钟**；1分钟/5分钟数值取环境变量，部署可调整。429的 `RateLimit-Reset` 在冻结实现为Unix重置时间；不能忽略它立即重试。

规划全部物品的周期采集：从本地 market-items 选需交易的 UniqueName → 明确 markets 和 qualities → 按编码后URL长度分批 → 串行执行/缓存 → 按返回 item/city/quality记录 observation_at/source/missing → 只更新有较新有效观察的数据。静态ID全量可以做任务清单，不代表所有请求组合都有响应记录。

### 4.5 orderbook 不能按源码直接假定可用

冻结 `routes.rb` 和 [market_orderbook_service.rb](sources/aodp-server/app/services/market_orderbook_service.rb) 存在 `/api/v2/stats/orderbook/:id`：按价格层求 amount总和，sell升序、buy降序，附 updated_at。
但本次 East 请求：

```text
https://east.albion-online-data.com/api/v2/stats/orderbook/T4_BAG.json?locations=Caerleon&qualities=1
```

实测 **HTTP404**，已记入 manifest。因此它是源码候选端点，当前没有可用性证据；不能把它作为已可运行的盘口方案。即使以后上线，它仍只反映项目观察到的订单。

## 5. 获取社区市场全库与持续 feed

### 5.1 全库

Asia备份目录为 [database-east](https://www.albion-online-data.com/database-east/)，
West为 `/database/`，Europe为 `/database-europe/`。已保存East目录HTML，未下载tgz/sql.gz。
2026-10-13目录包括 `db_backup_2026-10-13T02_00_24.tgz` 以及历史月文件。

[官方README](https://www.albion-online-data.com/database/%21README.txt) 的恢复顺序：

1. 在独立新数据库里恢复选定日期的 latest daily backup；这包括项目当前价格和最近6个月加当前月历史。
2. 若要更早历史，按所需月份降序导入 `market_history_YYYY_MM.sql.gz`。
3. 保留下载URL、日期、哈希、解包清单、dump schema/version和导入日志；源码是Rails/MySQL风格，先确认实际dump格式和依赖。
4. README指出2023-03历史和2023-12附近可能主键冲突；冲突处理只能在自己的恢复数据库按该说明调整2023-03范围后导入monthly。不要对游戏服务器或已有生产数据库执行清理。

这是恢复项目数据库的实施计划，没有在本任务下载或导入。若只要少量物品，REST更轻；若确实要项目全历史，使用备份比长期重复REST请求合适。备份全量仍不是Albion服务器全量。

### 5.2 NATS公开订阅

官方[developer文档](https://www.albion-online-data.com/developer)公开的Asia连接串：

```text
nats://public:thenewalbiondata@nats.albion-online-data.com:24222
```

这是项目公开共享订阅凭据，不是用户个人凭据。West端口4222，Europe34222。若已安装官方 NATS CLI，可人工选择订阅：

```powershell
nats sub 'marketorders.deduped' --server 'nats://public:thenewalbiondata@nats.albion-online-data.com:24222'
nats sub 'markethistories.deduped' --server 'nats://public:thenewalbiondata@nats.albion-online-data.com:24222'
```

这些命令仅作为后续接入示例，本次没有安装CLI或连接feed。`goldprices.deduped`、`festivities.deduped` 也是文档主题；`*.ingest` 含重复。

去重窗口存在**文档/源码差异**：developer网页写5分钟；冻结订单去重服务Redis `ex:600` 为10分钟。当前NATS部署没有实测。订阅程序不能依赖固定5分钟必有刷新，必须自行记录 last_seen/重连/丢失范围并按来源版本设规则。

订单字段为 `Id, ItemTypeId, ItemGroupTypeId, LocationId, QualityLevel, EnchantmentLevel, UnitPriceSilver, Amount, AuctionType, Expires`。**格式与价格单位按topic区分**：

| 层级 | 冻结源码观察 |
| --- | --- |
| 客户端订单/wire以及 `marketorders.ingest` | `Orders` envelope，`UnitPriceSilver` 为原始放大单位 |
| `marketorders.deduped` | 发布单个订单对象；server已将 `UnitPriceSilver /=10000`，并规范位置 |
| 入库与REST prices | 使用已换算的silver值，不能再除10000 |

该差异来自 [market_order_dedupe_service.rb](sources/aodp-server/app/services/market_order_dedupe_service.rb) 的发布前后顺序，不能从相同字段名假设同单位。客户端数值/JSON价格应同时保留 raw_value 和 unit/source，再做单位验证；本次未取得用户账户wire样例。

市场历史feed保存 `ItemAmount, SilverAmount, Timestamp` 和 item/location/quality/Timescale。wire Timescale枚举0=Hours、1=Days、2=Weeks（对应游戏展示尺度），不等于REST的1/6/24。某些时间为C# ticks：

```text
unix_seconds = (ticks - 621355968000000000) / 10000000
```

金额和ticks可能超出JavaScript安全整数范围；用整数/decimal或原始字符串，避免先转float造成精度损失。不能把UnitPrice、SilverAmount和REST avg_price当作同一单位未经核对混合。

推荐存储键 `(server, order_id)`；观察记录保留 `observed_utc, source_topic, raw_hash, item, market, quality, enchantment, price, amount, expires`。新订阅者没有自动历史补齐，先恢复备份再接feed仍要记录二者覆盖时间差。未再见到某订单只说明观察消失/陈旧，可能已成交、撤销、过期或没人重新打开；不能凭消失制造成交记录。

## 6. 地图运行状态与客户端协议的边界

当前AODP客户端使用Photon解析UDP/TCP payload，operation/event参数键使用253/252。冻结enum提供版本参考：Join2、GetGameServerByCluster17、AuctionGetOffers81、AuctionGetRequests82、AuctionGetItemAverageStats95、GetClusterMapInfo197、GoldMarketGetAverageInfo250。当前listener还能按响应内容识别订单字符串数组，说明仅硬编码操作编号不足以保障跨版本解析。

`GetClusterMapInfo` 在[解码器](sources/aodp-client/client/operation_get_cluster_map_info.go)中提供：

| 参数键 | 字段 |
| --- | --- |
| 0 | ZoneID |
| 17/18/20 | BuildingType / Coordinates / Durability |
| 22/23/24 | AvailableFood / Reward / AvailableSilver |
| 25/31 | Owners / Permission |
| 33/34 | AssociateFee / PublicFee |

这说明可以有区域地图上的建筑/地块状态响应，并不是地图地形全量，也不是玩家坐标。代码上传 `mapdata.ingest`，非整数ZoneID实例会被忽略。当前developer文档未列公开mapdata消费方案，REST routes未提供可确认的完整地图状态API；代码存在不证明生产订阅可用。

AODP的订单只来自贡献者正常打开、客户端实际收到的市场结果。FAQ说明部分账户live market数据仍加密，项目无法处理；历史和gold仍可贡献。本次没有启动用户游戏，也没有该账户capture，因此不能承诺用户被动读取订单必然成功。静态DES解码器不能用来绕过在线市场保护。[加密FAQ](https://www.albion-online-data.com/client-faq#encryption)

若以后用户提供其有权分析的既有 `.pcap`/`.gob`，AODP配置有 `-o` 离线入口、`-d` 禁上传和debug筛选；可对该文件离线解释已记录的市场字段。例如已存在匹配版本客户端时：

```powershell
& '<已存在的AODP客户端.exe>' -o '<用户提供的记录.pcap>' -d -debug -operations '81,82,95,197'
```

本次没有这类记录或执行该命令。Windows构建使用Npcap/WinPcap兼容库，是否安装应另行看所用二进制依赖；这不是在当前任务自动安装抓包前提的要求。现阶段可确定解码结构与局限，不能报告已捕获的市场/建筑运行值。

## 7. 地图人物信息：公开档案与局部事件分别处理

### 7.1 Asia GameInfo可读资料

已验证的Asia官方host为 `https://gameinfo-sgp.albiononline.com/api/gameinfo`。下面均返回HTTP200，保存为samples：

```text
/search?q=Albion
/players/wpDh5rpcTXyKkmDpnm7PNg
/players/wpDh5rpcTXyKkmDpnm7PNg/kills?limit=1&offset=0
/players/wpDh5rpcTXyKkmDpnm7PNg/deaths?limit=1&offset=0
/events?limit=1&offset=0
/guilds/aXGLUa6yS6q3t_4vL0Fg8A
/alliances/bMmQv71uRom7gQeeg9JBZw
```

| 数据类型 | 已见字段 | 实际限制 |
| --- | --- | --- |
| Player | Name/Id、GuildName/Id、AllianceName/Id/Tag、Avatar/Ring、KillFame/DeathFame/FameRatio、LifetimeStatistics | 样例没有当前zone、XY或在线字段 |
| Equipment/Inventory/IP | 档案schema有这些字段 | JaceCoe档案为装备全部null、Inventory[]、IP0；不能当作当前裸装 |
| LifetimeStatistics | PvE、Gathering、Crafting、FishingFame、FarmingFame、Timestamp | 统计快照，不能反推出当前活动位置 |
| Kill/Death/Event | EventId/TimeStamp、Killer/Victim、Equipment、Participants、GroupMembers、BattleId、KillArea、Location | 装备为事件时刻快照；本次Location均null，KillArea=OPEN_WORLD只分类 |
| Guild | Founder、Founded、Alliance、Logo、killFame/DeathFame、MemberCount等 | 不是成员实时地图状态 |
| Alliance | Guilds、NumPlayers、Founder、Founded等 | 不是当前在线地图人数 |

kill/death请求写limit=1，但各返回10条；全局events请求返回1条。实际分页参数支持因端点而异，不能照其他接口假定有效。保存响应count、去重EventId与最早/最晚TimeStamp，检测分页是否重复，遇到空页/重复页/服务错误结束明确范围。搜索返回部分匹配，不是全部玩家人口导出。

直接查询工具：

```powershell
& $albionPython $albionFetch search --query 'Albion' --output '.\investigations\albion-online\public-data\manual\search.json'
& $albionPython $albionFetch player --player-id 'wpDh5rpcTXyKkmDpnm7PNg' --output '.\investigations\albion-online\public-data\manual\player.json'
```

GameInfo可公开访问，但官方2020年论坛声明其主要供内部开发用途、不向公众提供该API支持。原帖直接抓取403，检索工具可读；已保存[摘要与访问方式](sources/gameinfo/official-support-statement-note.md)。这是一条历史声明，当前接口行为以2026样例为准；没有稳定合同、全量地图或实时玩家API的证据。[官方讨论](https://forum.albiononline.com/index.php/Thread/131017-Update-on-API-New-Endpoints/)

### 7.2 客户端人物事件能说明什么

冻结2026 AODP事件enum中有 Leave1、Move3、HealthUpdate6、HealthUpdates7、NewCharacter29、CharacterEquipmentChanged90。这些是协议线索，AODP市场客户端没有现成人物跟踪输出，不能承诺运行它直接得到附近全部玩家。

2021年公开示例可见NewCharacter的ID/Name/Guild/Position、Move的位置更新；另一示例在Leave移除对象、JoinFinished清空人物/怪物/资源缓存。旧协议参数位置（如NewCharacter12、Move byte[]偏移9/13）**没有经当前2026客户端wire验证，不可直接当实现规范**。本地元数据和REA结论应优先于这些旧示例。

从这些生命周期可以推断角色数据是按客户端收到的对象增删维护；缓存的旧位置必须标last_seen，离开后不能当作仍在地图上。Photon官方说明兴趣管理可以在服务端限制客户端收到的对象消息，支持这一一般机制；它不能单独证明Albion今天使用的具体半径、规则或同名PUN/Fusion版本。[Photon资料](https://doc.photonengine.com/server/v5/applications/loadbalancing/interestgroups)

因此“地图人物全量”应限定为**已发送给所选客户端的对象集合**，并记录join/leave、cluster实例、entity ID、观察时间、状态。实体短ID可能在不同区域/重连复用；不能用它直接替代GameInfo的永久player ID。缺事件、隐藏、跨区、遮蔽、局部兴趣限制都会使集合不完整。本次没有实测可见半径，也没有证据支持获得未发送角色或全球实时坐标。

## 8. 推荐实施顺序与尚未验证项

1. 使用已导出的本地静态目录建立地图/出口/模板/物品/市场主数据，保留版本与源哈希。
2. East REST按明确物品、品质、市场取得报价和历史；存原值、missing、时间、source/server。
3. 确需社区全历史时恢复East备份；确需持续订单观察时选择East公共NATS，单独处理原始/规范价格单位和不完整覆盖。
4. GameInfo补充已知玩家、公会、联盟与事件历史；不要用档案字段制造实时人物图层。
5. 如需校验当前客户端动态字段，后续必须有指定来源的被动记录或运行证据，再核对版本、数值ID、价格/时间单位、字段键、坐标变换及对象生命周期。

本专题已回答公开静态数据、East市场与公开人物资料的获取路线，并以少量响应验证了可用host/字段。仍未知：用户账户市场加密状态、实际NATS部署/去重窗口/mapdata订阅、当前orderbook部署、客户端兴趣范围、全局动态实例状态和服务器权威订单。这些未知不能由本地安装包补齐。
