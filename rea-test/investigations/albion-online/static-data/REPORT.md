# Albion Online 本地静态数据提取报告

记录日期：2026-10-13，Asia/Singapore。用户选择 Asia / East 服务器。

本报告分析的原始安装目录是 `C:/Users/ludy/AppData/Local/AlbionOnline`；客户端版本为 `albiononline-win32-full-1.32.030.345319`，Unity 为 `6000.3.12f1 (fca03ac9b0d5)`。所有输入只读，输出写入当前调查目录。本工作没有启动游戏、附加进程或捕获网络流量。

## 已经获得的数据

本地 `StreamingAssets/GameData` 中 **3,278 个 `.bin` 全部成功解密和 GZip 解压**。其中 **3,259 个为可解析 XML**；另外 **19 个 `profanity_*` 是词表载荷**，仍保存原始解码结果及 XML 解析诊断。本地 `profanity.xml` 本身是有效 XML。解码后的文件后缀统一使用 `.xml`，19 个词表的后缀不表示载荷是 XML。

`extraction-manifest.json` 保存每个原始路径、相对路径、输入/输出字节数、输入/输出 SHA-256、解析状态和错误；因此可准确恢复覆盖范围，不能把公开数据版本或文件名误认为本次安装的事实。

| 本地静态对象 | 实测数量 | 产物 |
| --- | ---: | --- |
| 世界中的区域定义 | 1,427；`enabled=true` 为 1,336 | `catalogs/map-clusters.jsonl` |
| 出口定义 | 3,223 | `catalogs/map-exits.json` 保留含 `@` 区域目标的出口；所有出口原字段也在区域目录中 |
| 小地图 marker | 1,945 | 区域目录中 `world_sections` |
| 世界地图 marker | 132 | 区域目录中 `world_sections` |
| 市场定义 | 106 | `catalogs/market-locations.json` |
| 引用的模板实例 | 15,384 | 区域目录中 `template_instances` |
| 模板文件 | 1,910 | `catalogs/template-index.json` |
| 模板中 tile/compoundtile 放置记录 | 1,888,023 | `catalogs/template-placements.jsonl` |
| 同名模板存在多个变体的名字 | 427 | template index 的 `duplicate_names` |
| tile 定义 | 159,078 | `catalogs/tile-definitions.jsonl` |
| compoundtile 定义 | 9,272 | `catalogs/compound-tile-placements.jsonl` |
| 有游戏语义子节点的 tile 定义 | 8,497 | `catalogs/tile-semantics.json` |
| 物品直接定义 | 6,049 | 原始 `decoded/items.xml` |
| 展开附魔/日记变体后的唯一物品 ID | 12,237 | `catalogs/market-items.jsonl` |
| 本地化键 | 40,070 | `catalogs/localization-en-zh.jsonl` |

这些计数描述安装包中的定义，包含 Debug、测试场景、岛屿模板、动态场景候选和未启用区域。**1,336 不能解释为当前 Asia 服务实际开放的区域数，1,888,023 也不是世界中当前存在的实体数。**

## 地图数据如何取得

`decoded/cluster/world.xml` 是世界目录。每个 `cluster` 有 `id`、`displayname`、`file`、`enabled`、`type`、`origin`、`size`、部分 `worldmapposition`、小地图 bounds/height range/base level 等原始字段。它还提供 exits、minimapmarkers、worldmapmarkers、territories、outposts、worldbosses、travelpoints、marketplaces、roads、distribution、mobcounts 等分组。`catalogs/map-clusters.jsonl` 保留每个原始分组及所有 XML 属性，没有仅保留几个预选字段。

`catalogs/world-other-sections.jsonl` 保存世界根目录的 `criticalObjects`（1,195 个）和 `staticdungeons`（98 个）条目。地图出口中的 `targetid` 常为 `出口 UUID@区域 ID`；脚本拆出的 `candidate_target_cluster` 是对本地格式的解释，原字符串仍保留。`targettype`、`restricted`、`staticdungeonentrance` 等字段需一并使用，不能把所有出口当作普通无条件通路。

`cluster@file` 指向一个 `.cluster.xml`。1,427 个区域定义引用的布局文件均存在；一些区域共享布局，因此布局目录本身不需要有 1,427 个独立文件。布局包含 `templateinstance@ref`、局部 `pos`、`rot`、`dyn` 和 `activelayer`。所有 15,384 个 `ref` 都能在本地模板目录找到候选；没有缺失布局/模板引用。427 个模板名字在 DEAD/GREEN/RED 等目录有多个变体，脚本将全部候选保留为 `ambiguous_variants`，实际环境/变体的选择仍需客户端规则确认。

模板中 `tile@name` 连接到 `cluster/tiles.xml`。这里能确定 tile 是否声明 `harvestable`、`mobspawnpoint`、`actionbuilding`、`lootchest`、传送门、城堡、鱼区等语义；2,126 个 tile 定义带 `harvestable`，2,070 个带 `mobspawnpoint`。这些是 tile 定义数，不能直接当作采集点或怪物数量。动态 `resourcenode` 等还可能在 `compoundcontents` 中，脚本将相关表单独完整保留。

`harvestables.xml` 保存资源类型/Tier→物品 ID、采集时间、工具等级差修正、charges、respawn 系数等定义。`mobs.xml` 保存怪物定义、Tier、faction、fame、prefab、速度、攻击、生命、重生范围等字段。`randomspawnbehaviors.xml`、`resourcedistpresets.xml`、`resourceprefabmappings.xml`、`dynamictemplates.xml`、`dynamicclusters.xml` 说明候选池、权重、生成规则与动态模板。它们已按 XML 结构输出 JSONL；本地规则表不能证明当前服务器采用的随机结果、charges、附魔状态或重生倒计时。

### 放置与坐标

地图有多套坐标：`worldmapposition` 是世界地图展示位置；cluster 的 `origin/size` 和 `minimapBounds*` 描述局部边界；templateinstance 的 `pos` 是三维放置；模板 tile 的 `pos` 是模板局部位置。不能将世界地图位置直接加到采集点局部位置。

组合一个候选点时，应先确定区域的模板变体和活动层，再递归展开 compoundtile、应用其自身的平移/旋转/scale，最后应用 templateinstance 变换。一般 Unity Y 轴正旋转候选公式如下，应用在每一层嵌套变换：

```text
x' = cos(θ) x + sin(θ) z + tx
y' = y + ty
z' = -sin(θ) x + cos(θ) z + tz
```

公式中 Y 轴约定、原点和渲染坐标方向仍需游戏代码/实际展示验证。`catalogs/placement-example-0217.json` 给出 Drownhorse Basin / `0217` 的 30 个直接 tile 语义放置候选，保存源实例、模板变体、局部位置、active layer、计算结果和推断标签。这个示例没有递归展平 compound，也没有选择动态运行状态，不能作为“地图所有当前实体坐标”。

### 高度、碰撞、地图图像和 Mesh

`cluster/tiles.xml` 中还有 **2,066 height、2,992 collision、1,128 effect 和 5,109 path** 表条目。这里的 `paths` 是 prefab 资源路径，**不能解释为导航网格或可行走路径表**；world 的 `roads@nodes/@links` 是另一份道路图。height/collision/effect 中的 `value` 多为 Base64 字节载荷，已原样保留在 `catalogs/tile-geometry-tables.jsonl`，未臆测它们的网格尺寸或编码。要生成可行走图，仍需结合 width/height/offset、compound/scale、活动层并解析客户端对应消费逻辑。

安装包使用 Unity SerializedFile v22：`globalgamemanagers`、`globalgamemanagers.assets`、`resources.assets`、`level0` 至 `level151`、`sharedassets*.assets` 等。本次离线 metadata 库存覆盖 **307 个文件，全部读取成功**，得到 **2,082,125 对象**、**100,472 ResourceManager 路径**，其中包括 **8,330 Texture2D、20,012 Mesh**。文件表、metadata prefix SHA-256、类型和对象的 offset/size、PPtr 的 external file ID 映射保存在 `unity-assets/`。

重点输入为：

- `resources.assets`：2,151,195,380 字节。
- `resources.assets.resS`：3,140,010,992 字节，纹理等流式载荷。
- `globalgamemanagers`：提供 ResourceManager 的路径→file ID/path ID 映射。
- `StreamingAssets/CustomMeshes/meshes.toc` 与 **11 个 `.pmsh` 包**：TOC 中 **3,943 个 Mesh 切片**，所有 offset/size 均验证在对应文件范围内；自定义 Mesh 内部编码没有解析。

`unity-assets/map-assets.json` 将包含 `clustermap/`、`minimapgen/`、`worldmap/` 的 **2,984 个资源路径全部解析到对象**，其中 **2,930 个是 Texture2D**。其资源名称、path ID、`.assets` offset/size、候选 width/height/format/mips 和 `.resS` 最终 StreamingInfo 样式 offset/size 均保存；这 2,930 个 `.resS` 范围全部存在且有效。例如 `minimapgen/templates/017_l1_m3_s5/017_l1_m3_s5` 对应 930×930 的基础模板图，附近还有 `layer_01` 等图层图，图层 bounds 在 `minimapgendata.xml`。

纹理字段采用本地 Unity 6000.3 载荷观察到的布局，未依赖旧 AssetStudio Texture 基类的错误偏移。它们标注为候选布局，范围检查不证明每个格式的像素解释。本次**没有导出 PNG、还原 Mesh 几何或实际渲染地图**。

下一步离线图像路线明确：用 `map-assets.json` 的 resource path 选择模板基础图和实际 active layer，读取其 Texture2D 及 `.resS` 切片，再用支持 Unity 6000.3 的 UnityPy/AssetRipper 等格式工具解析和转换，按 `minimapgendata` 的 `vMin/vMax` 和实例位置组合。必须带上 `.resS`，并使用 `source+path_id` 作为身份，因为 `Layer_01` 这样的名字大量重复。用当前目录的索引即可避免向工具一次载入全部 2 GB 数据。当前没有安装或运行 UnityPy/AssetRipper，这一转换路线没有作为已验证完成事项。

## Asia 文件和区域补丁边界

本地 GameData **有 `worldsettings_asia.bin`，没有 `cluster/world_asia.bin`、`world_asia_patch.bin` 或 `world_europe_patch.bin`**。本次产物保留共用 world 和三份区域 worldsettings，不凭文件名猜测当前 Asia 服务的有效世界。

公开 `ao-data/ao-bin-dumps` snapshot `1a469e53de028c5f85cdd80505f10cbd0a2515cf` 虽是较新的 repo 快照，但 `cluster/world_asia.xml`、`cluster/world_asia_patch.xml` 的单文件历史仍停留在 2023-10-08。`world_asia.xml` 最后提交 `ae5389007ad171d4fbe72e135748eac977959bf8`（消息 `update20230329`）；patch 最后提交 `5801fa9e1866a9a033be95079cd8618e963e9802`（消息 `update20230221`）。共用 world/worldsettings_asia 最近改变在 2026-09-23。这一陈旧问题由公开资料子任务的 per-file history 确认，见 `../public-data/` 来源记录。

旧 patch 使用类似 `<replace sel="world/clusters/cluster[@id='0004']"><attribute name="enabled" newvalue="false"/></replace>` 的 XPath 属性替换。`sources/HISTORICAL-world_asia_patch.xml` 仅保存语法证据。**没有把 2023 Asia patch 应用到当前本地 world**。通用合并要求先证明 base/region/patch 的版本一致和客户端加载顺序，再对每个 selector 记录匹配数，逐条按 replace/add/remove 操作应用并保存差异。不能连接两个 world XML，也不能把补丁的 `enabled` 改动推广到不同版本。当前 Asia 的最终运行时 world 和 patch 加载选择留待主体代码调查。

## 市场物品和位置

`catalogs/market-items.jsonl` 中 `item_id` 是字符串 ID，例如 `T4_MAIN_SWORD`、`T4_MAIN_SWORD@1`；`attributes` 保留 tier、shopcategory、子类、maxstacksize、weight、craft 等静态字段。名字从本地 `localization.xml` 的 `@ITEMS_*`/`descvariable0` 解析，保存 EN-US、ZH-CN、ZH-TW；**636 个展开 ID 没有这三种语言的匹配名称**，不伪造填充。

`dumper_index` 复现公开 ItemExtractor 的顺序（普通条目、enchantments、结尾日记 `_EMPTY/_FULL`），但没有证明它就是此版本市场网络消息中的 numeric item type index。后续协议字段需要按代码/捕获内容建立数值映射。物品品质不应混入 `@附魔级别`；市场响应中的 quality 需要独立保存。

| 城市名 | 本地城市 cluster ID | 本地普通市场 cluster ID |
| --- | --- | --- |
| Thetford | `0000` | `0007` |
| Lymhurst | `1000` | `1002` |
| Bridgewatch | `2000` | `2004` |
| Martlock | `3004` | `3008` |
| Fort Sterling | `4000` | `4002` |
| Caerleon | `3003` | `3005`；另有 `3013-Auction2` 定义 |
| Brecilien | `5000`、`5001` | `5003` |

`3003` 的市场对象 `type="BLACKMARKET"`，普通 Caerleon Market 的对象位于 `3005`。市场对象还各有 UUID，不能混淆城市 ID、cluster ID 和 market UUID。`market-locations.json` 保存 106 个全部定义及 marker 候选，因此也包含新人市场、portal、rest、hideout、guild island 等配置；不保证它们均被公开价格 API 支持。

本地字段能说明市场在哪里、交易物品是什么、UI 类别是什么。它们**没有给出当前买卖订单、价格、成交量、订单数量、更新时间或玩家身份**。公开行情 API 的 `locations=Thetford` 等字符串要按公开服务文档映射，不从 cluster ID 推断其 API 参数。网络/公开行情分析由主体报告合并。

## 复用命令

在项目根目录运行，Python 3.14 已存在。唯一增加的依赖为工作目录内 `static-data/dependencies` 的 **pycryptodome 3.23.0**；没有安装 .NET SDK 或全局依赖。

```powershell
# 首次缺失本地 Crypto 依赖时
python -m pip install --target investigations\albion-online\static-data\dependencies pycryptodome==3.23.0

# 第一次提取；已有输出时改新路径，或明确 --overwrite 只覆盖提取输出
python investigations\albion-online\static-data\extract_game_data.py --input 'C:\Users\ludy\AppData\Local\AlbionOnline\game\Albion-Online_Data\StreamingAssets\GameData' --output investigations\albion-online\static-data\decoded

# 重建静态目录
python investigations\albion-online\static-data\build_catalogs.py --input investigations\albion-online\static-data\decoded --output investigations\albion-online\static-data\catalogs

# 读取 .assets metadata、资源路径与 Mesh TOC；不读全部像素
python investigations\albion-online\static-data\inventory_unity_assets.py --input 'C:\Users\ludy\AppData\Local\AlbionOnline\game\Albion-Online_Data' --output investigations\albion-online\static-data\unity-assets
python investigations\albion-online\static-data\index_map_assets.py
```

`build_catalogs.py` 产物有较大的 `template-placements.jsonl` 和 `unity-objects.jsonl`；未来应按行筛选，先读 summary/manifest，不将整份大文件塞入上下文。`catalogs/catalog-files.json` 和 `unity-assets/FILES.json` 保留产物字节数与 SHA-256。

## 证据来源与限制

这部分的证据来自 **Python/PowerShell 只读本地格式解析及公开算法来源，属于本地静态证据，未赋予 REA Evidence ID**。主体 REA 证据请见上层报告；不能把此处 XML 解码成功描述成 native 函数执行或反编译结果。

- DES-CBC/PKCS7、Key `30EF724742F20432`、IV `0EA6DC89DBEDDC4F`，再 GZip → XML：公开 [BinaryDecrypter.cs](https://github.com/ao-data/albiondata-bin-dumper/blob/e73a181eafd0db785f889aba8dbe7c69f83b71ad/Extractor/Extractors/BinaryDecrypter.cs)，本地已保存源码。
- 递归 dumper/ID 顺序：[BinaryDumper.cs](https://github.com/ao-data/albiondata-bin-dumper/blob/e73a181eafd0db785f889aba8dbe7c69f83b71ad/Extractor/Extractors/BinaryDumper.cs)、[ItemExtractor.cs](https://github.com/ao-data/albiondata-bin-dumper/blob/e73a181eafd0db785f889aba8dbe7c69f83b71ad/Extractor/Extractors/ItemExtractor.cs)。本脚本保留上游跳过的 profanity，19 个非 XML 结果有诊断。
- SerializedFile v22 header/type/object table、NamedObject、ResourceManager 结构：[AssetStudio SerializedFile.cs](https://github.com/Perfare/AssetStudio/blob/d158e864b556b5970709c2a52e47944d53aa98a2/AssetStudio/SerializedFile.cs) 及同提交的 classes，本地已保存源码；Texture 前缀以实际 Unity 6000.3 数据修正，字段仍标注 candidate。
- [历史 Asia patch](https://github.com/ao-data/ao-bin-dumps/blob/1a469e53de028c5f85cdd80505f10cbd0a2515cf/cluster/world_asia_patch.xml)；陈旧日期与详情见公开资料来源。

已回答：本地 GameData 容器算法、全部文件提取、静态世界字段、物品名与市场位置、模板/资源语义与图像入口。部分回答：地图位置组合（候选变换/层/变体）、碰撞/高度和图片（已定位原始数据，未完成完整语义/像素解码）。未回答：当前 Asia 最终 world 选择、当前在线玩家/资源状态、实时市场订单/行情。这些缺口不能靠静态文件计数弥补。
