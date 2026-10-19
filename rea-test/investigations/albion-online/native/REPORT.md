# Albion Online 原生代码证据

目标：`C:/Users/ludy/AppData/Local/AlbionOnline/game/GameAssembly.dll`。
服务器范围：Asia / East。客户端版本：`1.32.030.345319`，Unity `6000.3.12f1`，
IL2CPP metadata v39。本文与 [静态资源报告](../static-data/REPORT.md)、
[IL2CPP 元数据结果](../metadata/results)配合阅读。

## 当前状态

**第四次导入、REA overview 和23个所选函数查询全部成功，证据包已导出，会话已关闭。**
overview Evidence 为
`ev_fc4c58853615b3102f902b6783f28b6e6a5698d11cfba0399335cb5f805a0e86`，完整返回见
[gameassembly-overview.json](evidence/gameassembly-overview.json)。
`open_binary` 和 `binary_session` 仅确认目标绑定与分析配置。
完整 [evidence-bundle.json](evidence-bundle.json)包含24条记录，共28,998,233 bytes，SHA-256：
`e26c53944189137a041a7239d98dedf3535a9445d0320a9a9a9d58ab5ad30a5c`。
`unknowns=0`仅指该bundle的残余未知ledger为空，不能推定程序所有功能都已还原。

第三次导入于2026-10-13 18:10:42 左右返回 **`provider_timeout`**，耗时1200.74秒，
`failure_message="Ghidra startup deadline elapsed"`，无 `ev_*` ID。
保存完整失败返回与stdout/stderr的目录为 [attempt-profile-one](attempt-profile-one)。
监控曾依次观察 switch analysis、constant propagation、operand reference、FID；
最后一份线程栈在1156.94秒时位于 `FidProgramSeeker.search`，保存在
[last-thread-before-timeout.txt](attempt-no-return-disabled/last-thread-before-timeout.txt)。
这些观察证明导入阶段曾推进，不能作为功能分析结果；不能把这次失败简单归为
constant propagation 死锁。

第四次导入从18:11:30 左右开始，延长startup预算并禁用四个指定全局分析器，
18:43:19的Ghidra日志确认分析成功并进入REA bridge。overview耗时 **1914.01秒**，
Ghidra记录auto-analysis总时间1869秒；已发现 **195,445 procedures、7,480 strings、
8 segments**，首选ImageBase `0x180000000`。包括Ghidra生成的`tdb`地址空间，这些
库存是此profile下的识别结果，不等于元数据方法数或完整全函数语义。
完整导入日志见 [ghidra-import-complete.log](attempt-profile-two/ghidra-import-complete.log)。
随后由会话持有者恢复18个所选函数并追加5个人物状态函数，全部复用同一已导入
会话，成功返回。没有启动游戏或取得实时游戏样本。

## 输入与实际分析配置

| 项目 | 已确认内容 |
| --- | --- |
| GameAssembly SHA-256 | `19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202` |
| global-metadata SHA-256 | `b0cffb9a6945366c0057390c2144edbfb7b9fb0b9a2f327ed8517e3051b7684b` |
| 原生格式 | Windows PE x86-64 DLL |
| Provider | Ghidra `12.1.4`，Temurin JDK `21.0.12.1` |
| 实际 REA 来源 | 本调查隔离副本 `vendor/rea`，Node启动时HEAD `d13c9f840d36a321a56917e75c48afcb8d72920e`；后置Java编译加载兼容本地扩展，见下文 |
| 服务自报版本 | `rea-agents 6.4.0`；`build_commit=null`，不能仅靠版本号识别副本修订 |
| 实际入口 | [rea_session.py](rea_session.py)，独立 stdio，未更改全局 Codex 注册 |
| 第四次分析 preset | `ghidra-default+disabled:Non-Returning Functions - Discovered,Decompiler Switch Analysis,x86 Constant Reference Analyzer,Function ID` |
| 第四次分析 profile digest | `96664c5e569b8baa0db21aa3e46d10a6d2eaff5fe5324a1fbaa54ee3a5a61643` |
| 第四次子进程设置 | startup timeout `2400000 ms`，Ghidra heap `4G`，禁用上述四个分析器 |
| 第四次 run ID | `cf4f6934-1449-4b27-8683-fa7bd05a8566` |
| 输入策略 | REA 私有临时项目；Ghidra `-readOnly -deleteProject`；未运行游戏 |

配置来源保存在 [bound-session.json](evidence/bound-session.json)，输入绑定保存在
[open-gameassembly.json](evidence/open-gameassembly.json)。它们没有函数级 `ev_*` ID。
Windows addon 复用了已安装 npm 6.4.0 产物，其来源、SHA-256 与 ABI 范围详见
[环境记录](../../../REA-ENVIRONMENT.md)。副本与全局 npm 的原生源码不同；不能声明
已由该修订重新编译 addon，或把副本结果归给全局默认配置。

先前默认导入曾重复停在
`FindNoReturnFunctionsAnalyzer.repairDamagedLocations → ClearFlowAndRepairCmd`。
该尝试被持有它的 driver 取消，**没有成功的首个 Ghidra 查询**，诊断见
[attempt-default/diagnostic.json](attempt-default/diagnostic.json)。本次调整分析 profile
后的结果必须保留配置差异，不推定与默认分析完全等价。

第四次Node启动时读取的是上述HEAD构建；**Java bridge为后置脚本，直到18:43:19
才编译，当时读到含本地兼容扩展的Java源码**。已保存的实际
`ReaGhidraBridge.class` SHA-256为
`f06399352cecd4b77307437542b353dcdae4474f9962edf03ecd6b5defe73c35`，含
`seeded-noanalysis`常量；当前descriptor/profile仍走default分支，**该备选模式没有运行**。
因此本次不能归为未修改HEAD Java，也不能归为按种子地址跳过auto-analysis。
实际class、HEAD对照、后来最终源码及各自hash见
[attempt-profile-four/manifest.json](attempt-profile-four/manifest.json)；最终磁盘源码
未证明与当时编译的源文本完全相同。此边界不靠相同版本号或ABI推断。

overview由REA composed workflow生成，自身profile digest为
`86bb5b15c4ca832d9ef4524828c5a11792857659dacad123858bc5c35a521aa4`，其中的
`upstream_analysis_profile`才是Ghidra `96664c5e…`。23个函数Evidence直接使用后者。

第三次profile digest为 `e735fcd6ab90f3b111c6d760b0d78c8bc569abdcd8caaff0468855d8ebfd2fa1`，
只禁用发现式 Non-Returning，startup预算1200000ms。第四次配置增加的覆盖限制：

| 禁用分析器 | 对本报告的实际限制 |
| --- | --- |
| `Non-Returning Functions - Discovered` | 自动发现不返回函数与其后流修复不完整；边界需与返回/尾调用/原始指令交叉确认 |
| `Decompiler Switch Analysis` | 不做全局 dynamic instruction/switch 补建；大事件路由的case、默认分支和间接目标可能未恢复完整 |
| `x86 Constant Reference Analyzer` | 缺少该阶段跨指令计算地址和LEA等自动引用恢复；data xref、指针与间接流引用可能减少 |
| `Function ID` | 不做已知函数的hash识别与FID命名/bookmarks；匿名函数名不证明无库实现或无调用目标 |

独立目标函数decompiler仍可能生成伪代码，具体以每次返回为准。全局auto-analysis
完成也不保证所选路由分支、虚调用目标、完整图或字段含义全部恢复。精确选项名与
安装版源码依据保存在 [analysis-profile-sources](analysis-profile-sources)。

## 完成的查询与准确身份

以下地址来自本版本元数据到原生函数的映射；方法语义需与真实函数结果交叉确认。
导入等待期间，活动 [commands.jsonl](commands.jsonl) 只保留 overview。其余18个函数保存在
[pending-function-commands.jsonl](pending-function-commands.jsonl)，overview成功后由会话
持有者恢复；避免首个查询超时后，后续函数盲目逐个重新导入。完成调用记录见
[calls.jsonl](calls.jsonl)。选择或保存命令不表示调用完成。
身份对照来源为 `AO-META-SYMBOLS-001`，完整方法与 pointer slot 保存在
[all-method-symbols.json](../metadata/results/all-method-symbols.json)。
小型索引 [selected-method-identities.json](selected-method-identities.json)保留源文件 SHA-256、
所选方法 token、pointer slot 和地址别名数量；它是调查脚本派生产物 `AO-META-SELECTED-NATIVE-001`。

以上23个函数全部成功。完整Evidence ID表见下文；伪代码、assembly、函数体范围、
引用与限制留在每个原始JSON里。`results/*.c`是为阅读生成的伪代码副本，不是原始源码。

两个早期选点的命令标签包含`router`，实际身份已修正：

- `0x18097f270`、[photon-event-router.json](evidence/photon-event-router.json)是
  `ci9.b<am2>` 的 **Died=164 处理函数**，不是所有事件的分派器。
- `0x18097d820`、[photon-operation-response-router.json](evidence/photon-operation-response-router.json)
  是 `ci9.b<ahi>` 的 **Join=2 响应处理函数**，不是所有操作响应分派器。
- 固定generic usage slot分别为`0x184dc2ff0`/`0x184db9160`，MethodSpec
  193051/192933；身份依据 `AO-META-GENERIC-USAGES-001` 与
  `AO-META-PROTOCOL-ATTRIBUTES-001`。两份REA函数结果证实相应typed value flow，
  没有建立完整业务号到全部handler的路由表。

其中 `0x180492120` 被 **605 个元数据方法**共享，含
`DragonAreaSceneTemplateMetaData`、`HellDungeonsSceneTemplateMetaData`、
`MistsSceneTemplateMetaData` 的 `GetMiniMapData`。单独反编译该地址不能确定唯一
接收类型或小地图用途；需要各类型字段、调用位置和相应 method pointer slot。
`0x18084b3a0` 也被四个 selector 方法共享；确认其用于协议排序需保留特定
generic usage、methodDef 76164 和相关调用链。其余所选地址各映射一个元数据方法。

## 解释结果时的证据边界

- 原生伪代码描述已发布客户端的静态行为，不证明服务器发送了某条消息、客户端
  当前收到某个玩家，或 Asia 全区域的数据可查询。
- Photon 外层包装码与 Albion 内部事件/操作枚举是不同层。只有具体函数对参数
  字典的访问和转换能建立两层关系，不能把元数据枚举直接当作外层 wire code。
- 元数据中的字段类型、原生偏移和候选参数键由分析脚本输出，标记为 `AO-META-*`；
  REA 函数 Evidence 才使用工具返回的 `ev_*`。两类 ID 不混用。
- 辅助脚本已建立人物显示名称、公会和联盟关系的静态语义链，保存在
  `AO-META-PLAYER-SEMANTICS-001`
  [player-semantic-mapping.json](../metadata/results/player-semantic-mapping.json)及
  [元数据报告](../metadata/REPORT.md)。这类结果无需等REA导入才能引用，但应准确
  说明其脚本分析来源；角色Guid与公开永久ID、装备槽映射、Health数值完整语义仍未确认。
- `NewCharacter`、interest area、party member 等名字不足以证明同地图全部玩家、
  隐身玩家、精确兴趣半径或全服务器坐标可见。需要具体路由与更新处理，并保留
  缺少运行时观察的限制。
- 市场 DTO 字段与界面处理可以解释客户端如何消费报价；完整实时订单总量、
  分页完整性、未访问市场，以及公开数据库覆盖率仍需相应服务或实际观察证据。
- 全局导入日志包含无法反编译部分函数的警告。最终应逐个记录所选函数的返回
  限制、引用完整性和伪代码状态，不能将日志警告笼统视为全部函数失败或全部成功。

离线派生索引 [results/SUMMARY.json](results/SUMMARY.json)由
[summarize_native_results.py](summarize_native_results.py)生成，保留原始返回文件的hash、
Evidence ID、profile与限制，不重新运行分析。

## 协议：直接观察与关联结果

### Photon入口

事件入口 `c3s.akw` (`0x180a8d960`) 的REA伪代码直接观察到：

1. 读取事件对象`+0x10`的outer Code；Code=2走一条特殊监听器路径。
2. Code=3时设置业务号3；其它普通app event从`+0x18`参数字典取 **key0xFC/252**，
   检查boxed类型、unbox后取两字节业务号。
3. 把事件对象与业务号传给已有监听器，不从安装文件生成实体。

证据：`ev_69c27a66bb638371ce82cf0a1a6606a85e68720fbcee137980a92d6a5d5754c8`。
两字节类型对应`short`、Code3对应Move=3的关联由本版本元数据佐证。

响应入口 `c3s.akx` (`0x180a8dc00`) 直接观察到：

1. 从响应对象`+0x20`的参数字典取 **key0xFD/253**，boxed检查后unbox两字节。
2. 遍历监听器传递响应和操作号。
3. 可取 **key0xFF/255** 的四字节值，在已有请求表中查找、移除条目，保存响应并
   调用callback；缺少该值时走其它监听器处理。

证据：`ev_63b88a60dd86d13f0eab1e35b651715eee6ab6e231b059512fc60b044419233e`。
四字节值作为请求关联号的用途由这条查表/移除/callback链支持。

两份函数均含terminal flow的来源未知等限制。事件入口函数体是两个非连续范围，
共578 bytes、跨度666 bytes；不能以首尾地址之间所有字节作为函数体。
这建立客户端的接收入口语义，**不建立当前网络物理编码、所有业务handler分派表或服务器发送覆盖**。

### 两个“code wrapper”实际上只转发

`cpo.af`、`ctp.af`直接观察到读取接收对象`+0x160`/`+0x568`，将两字节值与第三参数
传给`FUN_182ae4910`/`FUN_182ad4100`。缺接收对象走异常路径。它们本身没有访问
key252/253；不能拿它们独立证明Photon wire封装。被调用函数名称和完整语义未在
这两个结果中还原。Evidence见完整表，terminal call/fallthrough依据未知保留。

### 反射参数排序

`c0.a(Type)` (`0x18084da40`) 的REA结果观察到构造类型/字段集合、父链访问
virtual`+0x888`、reverse调用、`GetFields`对应virtual`+0x6C8`传入`0x16`、属性过滤、
经selector构造delegate并排序，再为字段构造setter。

`0x18084b3a0`的REA结果只直接证明virtual **`+0x228`** 调用；Ghidra没有独立命名
`get_MetadataToken`，还给出“Could not recover jumptable / Treating indirect jump as call”。
排序为MetadataToken需要 `AO-META-PARAMETER-ORDER-001`中明确的methodDef76164、
generic usage和FieldInfo vtable slot15佐证。相同原生地址的四个selector别名必须保留。

因此当前参数键恢复来自REA与元数据/限定反汇编的合并证据，而不是selector伪码
单独“看出”反射方法。REA反射函数的high p-code值流截断，至少一个operation省略；
不能据其图声称全部def-use链接已保存。byte参数键递增的泛型parser没有本次新REA
函数查询，仍引用已保存的 `AO-META-PARAMETER-ORDER-001`辅助证据。

## 市场：订单进入客户端的具体链

### 订单响应与DTO

- `dgv.aov` (`0x180b27010`) 调用folded parser`0x182004220`，从返回对象 **`+0x20`**
  取数组；逐元素创建对象并调用 **`dgc..ctor 0x180b20e70`**。它把结果存入市场缓存，
  并发送包含本次列表count与响应`+0x28`布尔值的通知。
- `dgw.aov` (`0x180b27340`) 走对应数组循环和DTO构造链，存入对应缓存并通知。
- `dgc..ctor`取输入字符串调用 **`0x181edb0f0`**，将返回对象保存到`this+0x10`。
  REA直接给出此调用/写入；具体generic实参 **AuctionDTO** 来自usage slot
  `0x184dca358`、MethodSpec191257、typeDef15578的元数据关联。

`AO-META-PARAMETER-ORDER-001`进一步对应AuctionGetOffers=81的`ad1`和
AuctionGetRequests=82的`ad2`：key0为string[]，key1为bool。该bool的分页/查询语义
仍不能仅由此位置确定。返回通知中的本次count不等于服务器总订单数。

对应REA Evidence：

- Offers：`ev_d852ad07d58d9aa6f03c8d4c591b3ac3015540e58fe6969bdbba33bacf5da7fc`
- Requests：`ev_86195ba6ed6c07b982a5701b9a0929720c6a2b007e4db6f70733c677ce2c699c`
- DTO：`ev_d5eba8610f42f8bad8341201880b2a098503b94a5ef7f1547e6de1c0b641fcc2`

DTO函数体是113 bytes的两个非连续范围，跨度300 bytes。两个响应处理为连续函数体，
分别812/788 bytes；这三份值流未标截断，但业务字段、wire单位和实际服务器空值
不能由函数体连续性证明。AuctionDTO的21字段表见 [元数据报告](../metadata/REPORT.md)。

### 市场弹窗

`MarketPlacePopUpGui.OnAuctionGetOffersUpdatedBuy` (`0x180fd3820`)直接观察到减少
待处理计数并更新等待控件；只在特定界面状态执行后续处理。空列表清空/隐藏
部分控件，非空时取 **第一个元素**更新弹窗值，然后调用刷新。
Evidence：`ev_78bf045a42f0cefcfaeb776949a25ede3c581fcca0810129f1560f722cc29785`。
它说明当前弹窗如何消费查询结果，不能解释成遍历全市场、公开API或自动查询所有城市。

本地订单处理可配合公开East行情与社区库建立统一记录，但仍要保存server、
市场/cluster/UUID、item ID与品质、观察时间、原始单位及查询范围。公开REST、
NATS、备份的覆盖与访问结果以 [公开数据报告](../public-data/REPORT.md)为准。

## 地图：显示数据与实时实体

### GetRegionMesh只选择渲染资源

`WorldmapGeneration.GetRegionMesh` (`0x180e9b6c0`)的REA结果观察到取得现有mesh候选，
从region对象两个字段计算尺寸因子，使用helper筛选/排列候选，对候选bounds相关
值作差异阈值比较，最后在一定数量候选中选择并返回引用。名字和参数类型由
`AO-META-SYMBOLS-001`佐证。
Evidence：`ev_35f8f41ad156a7f52ee6b16106e0b71dfce707a6b1ce9b55478e19a152958aa0`。
伪码中的`0.5*0.01*1.2`或差异阈值`0.6`尚未建立世界坐标单位；不能将它们当作
通用地图投影公式。它没有读取服务器资源点列表，也没有解包Mesh、碰撞或导航。
本地具体资源路径与stream range仍使用 [静态资源索引](../static-data/REPORT.md)。

### GetMiniMapData地址共享

`0x180492120`只有5 bytes，直接返回 **`this+0x30`** 的指针。
Evidence：`ev_bc1d176273996f02a7797325c105d6af339aa118c9f16e79d0d902b406ad5d14`。
605个元数据方法共用它，故没有“此函数导出全部地图数据”的证据。

### ClusterMapInfo成功响应与callback

`0x180a9c910`直接读取响应`+0x12`；零值分支调用folded parser`0x182004220`并
把返回值传callback，非零分支给callback传null。元数据关联把该parser实例
映射为GetClusterMapInfo=197的`afb`（92个静态字段）。
Evidence：`ev_fa6bb27327d58d6be58cb7b58ad91541df1995056aa27601a65dfd57d744b96c`。
该函数有两处间接跳表恢复警告，callback实际目标未确定；它不证明不经游戏上下文
即可请求任意区域，更不证明所有92字段每次填满。

### 小地图实体位置

interest marker函数 `0x18145ae00`遍历已存在的marker字典，用ID调用本地实体查询
`0x18096f090`；找到并通过类型检查后，取得虚方法位置值，调用
`MapLabel.SetPosition 0x18132f150`。Evidence：
`ev_5297bf2d8ea7ce8c8ed7aa8908d17209596678f26bc978e2a8964d5bceb7ee3b`。
它使用本地已知实体与已有marker，不能由该循环推出全区域隐藏人物或兴趣半径。

队友marker函数 `0x18145c270`具有多个可用性/类型/关系条件及多个位置来源；
其high p-code值流截断，至少一个operation省略。不能把它化简为“所有队友每帧
都有当前世界坐标”。人物创建、名称、公会/联盟、装备、生命处理的逐项REA复核
见 [PLAYER-REVIEW.md](PLAYER-REVIEW.md) 与 [元数据报告](../metadata/REPORT.md)。

## 全部函数Evidence与覆盖诊断

以下ID均为REA成功返回的完整ID。简短标签仅便于查找；原始JSON路径在
[results/SUMMARY.json](results/SUMMARY.json)，没有把标签当成行为证明。

| 地址 / 标签 | 完整Evidence ID |
| --- | --- |
| `0x180b20e70` DTO构造 | `ev_d5eba8610f42f8bad8341201880b2a098503b94a5ef7f1547e6de1c0b641fcc2` |
| `0x180b27010` Offers响应 | `ev_d852ad07d58d9aa6f03c8d4c591b3ac3015540e58fe6969bdbba33bacf5da7fc` |
| `0x180b27340` Requests响应 | `ev_86195ba6ed6c07b982a5701b9a0929720c6a2b007e4db6f70733c677ce2c699c` |
| `0x180bd9010` 人物名称setter | `ev_e6913f7f59d91e7c49745ea72241e03b2f13b76e860498d2bf120b3fd623a34a` |
| `0x180a9c910` ClusterMapInfo响应 | `ev_fa6bb27327d58d6be58cb7b58ad91541df1995056aa27601a65dfd57d744b96c` |
| `0x180be90e0` 装备变化处理 | `ev_c29961641b5e48548c4f096918dd40095a62c188c27b676dd6bdd5ac36e2b728` |
| `0x180bd9770` 装备索引setter | `ev_7fb7b0995d7fc471f6c8a8c73f7b0626922f7f0018508a15e5c6d6e718f37c88` |
| `0x180bc2cb0` Health处理 | `ev_77566fb7d680d514d7c0ec8f57b221fba9e088470a46d63bcabf4a3d0778dff5` |
| `0x180fd3820` 市场弹窗 | `ev_78bf045a42f0cefcfaeb776949a25ede3c581fcca0810129f1560f722cc29785` |
| `0x180492120` 共用getter | `ev_bc1d176273996f02a7797325c105d6af339aa118c9f16e79d0d902b406ad5d14` |
| `0x18145ae00` interest marker | `ev_5297bf2d8ea7ce8c8ed7aa8908d17209596678f26bc978e2a8964d5bceb7ee3b` |
| `0x18145c270` 队友marker | `ev_a2e8f7a470169f9315f5f94e1ae1b18763fb01a06b5ab2ea88c78e3ee72f5325` |
| `0x180aacaf0` NewCharacter创建 | `ev_1432a2360489cab337f425cde48c2fe3f515b2e1eb2919145f4d50970f6e5657` |
| `0x180becdc0` NewCharacter状态应用 | `ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3` |
| `0x180bc19b0` 事件号转发 | `ev_b01227b0097253bf249ed53e19d8c29f07aa2df4ea54c15ce90636d2e8a6f29c` |
| `0x180a8d960` Photon事件入口 | `ev_69c27a66bb638371ce82cf0a1a6606a85e68720fbcee137980a92d6a5d5754c8` |
| `0x18097f270` Died164 handler | `ev_13c394eb5f8aab059323af5d95a83177413e5b29b72fd1f6638d829a269efe55` |
| `0x180998d30` 操作号转发 | `ev_577005b78666fa424ba90d66f60699b2dfcd70f945faeda333b6a91bb4cc0e7e` |
| `0x180a8dc00` Photon响应入口 | `ev_63b88a60dd86d13f0eab1e35b651715eee6ab6e231b059512fc60b044419233e` |
| `0x18097d820` Join2 response handler | `ev_51db60ff8be058f6eb7fc5e61dc843a366f73aa06c0311ec6f4bc3083a0ac7e5` |
| `0x18084b3a0` 共用selector | `ev_8b515996b4f5c2c0db1f572420b0f4397a9ef2d06b89027aea28c320205bbb08` |
| `0x18084da40` 反射字段排序 | `ev_f7bd866e632a4d64320a6a27f4c8dfeb542f441f9c1169f19edba050b0bd6327` |
| `0x180e9b6c0` region mesh选择 | `ev_35f8f41ad156a7f52ee6b16106e0b71dfce707a6b1ce9b55478e19a152958aa0` |

共同限制：Ghidra引用覆盖其识别的完整FunctionBody AddressSet，但未解析的targetless
间接流没有可引用边；callers=0不证明没有运行时调用。high p-code不是执行记录，
def-use不跨函数边界，也不完全建模内存别名或调用副作用。

| 函数 | 非连续函数体 / 值流截断 / warning |
| --- | --- |
| DTO构造 | 非连续113 bytes，跨度300；值流未截断 |
| Photon事件入口 | 非连续578 bytes，跨度666；值流未截断 |
| interest marker | 非连续585 bytes，跨度612；值流未截断 |
| 队友marker | 非连续2886 bytes，跨度3132；值流截断，omitted operation下界1 |
| 反射字段排序 | 非连续2202 bytes，跨度2316；值流截断，omitted operation下界1 |
| NewCharacter状态应用 | 连续6978 bytes；值流截断，omitted operation下界1 |
| ClusterMapInfo callback | 两处jumptable/indirect-jump-as-call warning |
| 共用selector | 一处jumptable/indirect-jump-as-call warning |
| Health处理 | 一处jumptable/indirect-jump-as-call warning；人物复核说明语义缺口 |

其余函数值流未标截断。所有terminal flow/no-return未知和各函数详细限制均保留在
原始返回中，未因调用成功移除。

## 会话结束与复用

`export_evidence_bundle`成功写出24记录包，见 [export-bundle.json](evidence/export-bundle.json)。
`close_binary`成功，见 [close-target.json](evidence/close-target.json)；会话driver退出0。
持有者随后确认本次owned Java52304和wrapper48672已退出、临时root
`rea-ghidra-59893a1afd1af82e342cb1e72073036d`已删除。未操作其它已有CLI进程。

未来优先读此报告、`results/SUMMARY.json`、所需单份Evidence和元数据字段表；
完整bundle为约29 MB。它保存可携带的结果，**不是可重新打开的Ghidra数据库**。
若需新的原生函数查询须新导入；不要把workflow snapshot或旧外部overview当作
已恢复的活动数据库。此次23函数成功来自四项禁用的default auto-analysis分支，
seeded-noanalysis备选扩展未用于任何结果。
