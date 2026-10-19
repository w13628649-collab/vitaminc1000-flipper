# Albion Online 当前客户端元数据专题报告

日期：2026-10-13；游戏版本：1.32.030.345319；用户服务器：亚洲服 East/Asia。

本专题只读解析安装文件，回答地图、人物和市场协议实际包含哪些字段。所有地址均是文件首选 ImageBase `0x180000000` 下的 VA；运行时 ASLR 地址未观察。没有启动目标、调试、注入或收集游戏流量。

## 一、输入身份与覆盖

| 输入 | 大小 | SHA-256 |
| --- | ---: | --- |
| `game/GameAssembly.dll` | 89,043,456 bytes | `19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202` |
| `game/Albion-Online_Data/il2cpp_data/Metadata/global-metadata.dat` | 22,381,812 bytes | `b0cffb9a6945366c0057390c2144edbfb7b9fb0b9a2f327ed8517e3051b7684b` |

目标根目录：`C:/Users/ludy/AppData/Local/AlbionOnline`。Unity 6000.3.12f1，IL2CPP metadata v39。v39 header 每 section 是 offset/size/count 三个 int32，动态索引宽度为 Type 4、TypeDefinition 2、GenericContainer 2、Parameter 4 bytes。

已解析 28,764 types、201,478 methods、134,446 fields、184,141 parameters、31,553 default records、93 images；所有已解析 table stride、字符串范围、method ownership、field/image coverage、token 标签经过结构校验。匹配全部 93 codegen modules，187,278 method slots 非零。泛型方法的零槽不能当成不存在；本调查另外恢复所需 MethodSpec/GenericInst 使用。

格式来源固定到 Cpp2IL commit `b5ad444b82267cb1e4b88b8b373c008105bdea52`，完整 URL/源码 SHA-256 见 [source-manifest.json](sources/source-manifest.json)。Unity compressed signed int 的负数公式为 `-(unsigned >> 1) - 1`，已与固定源码 `ClassReadingBinaryReader.cs` 核对。

本专题 `AO-META-*` 是调查自定义证据号，**不是 REA MCP Evidence ID**。原始文件偏移、字节、token、native offset、模块 pointer slot、SHA 与格式来源保存在 results JSON。主报告可另外引用 REA/Ghidra 证据。

## 二、协议与参数键恢复

### 已观察

1. `ep..ctor(short)` methodDef 76963 是 operation 属性，`eq..ctor(short)` methodDef 76966 是 event 属性。v39 custom attribute 按 image 范围和 token 关联；当前得到 786 operation 类型、702 event 类型，1488 个一-short 属性均完整解码，无跳过。这里是类型数；同一 operation 会有请求/响应类型，不能当成1488种协议。
2. `NewCharacter=29` 直接关联 `atd`，不是字符串猜测。attribute 原 blob offset **20546112**，hex `01a62c0100010000061d00`。
3. `c3s.akw(g7s)` VA `0x180a8d960` 是事件入口：普通 app event 从参数 key **252/0xFC** 取 short subtype；特殊 outer Code==3 分支按 Move=3处理。
4. `c3s.akx(g7q)` VA `0x180a8dc00` 是 operation response 入口：key **253/0xFD** 为 short subtype，**255/0xFF** 为关联请求 int。
5. 泛型 event parser `ci9.b<T>` VA `0x182003eb0` 遍历 `List<ce>` setter，key 从 byte 0 开始，逐 setter 加1；缺 key 跳过。operation response folded parser `0x182004220` 读取253并使用同类 setter 序列。
6. setter cache 静态初始化链 `er..cctor 0x1808e5070 → c0.a(Type) 0x18084da40`：构造父类型链并 Reverse；对每一级使用 `GetFields(0x16=DeclaredOnly|Instance|Public)`，过滤首属性为 `en` 的字段，每级 `OrderBy<FieldInfo,int>(get_MetadataToken)` 后构造 `ce`。
7. 排序 selector methodDef **76164**, VA `0x18084b3a0` 的 virtual offset `0x228` 对应 Il2CppClass vtable base `0x138` + slot15×16；metadata `FieldInfo` slot15正是 `get_MetadataToken`。Type.GetFields slot89→0x6C8、BaseType slot117→0x888同时一致。

### 由以上代码推出

本报告的“参数键”是**当前客户端静态推导的接收参数位置**。父字段先于子字段；`avk.ddn` 是 Move/装备/Health 单事件 key0；`ah0.bgm/bgn` 有 `en` 属性，排除，因此 `afb` key0是其第一个自有字段。`atd` 66个 public instance 字段全部保留，包括标记 `eo` 的 timestamp，故 key0..65与 own metadata ordinal一致。

表中的类型是**解析后 CLR 字段类型**。Photon 实际物理编码、转换/压缩/坐标加密与服务器条件省略未逐项恢复，也没有当前网络样本验证；不能直接把 Vector2/GameTimeStamp/Guid 的 native 内存布局当成网络包格式。native_offset 是解析后对象相对偏移，不能当成包字节偏移。

证据：`AO-META-PROTOCOL-ATTRIBUTES-001`、`AO-META-PARAMETER-ORDER-001`；完整 [协议类型属性](results/protocol-attributes.json)、[参数 schema](results/protocol-parameter-schema.json)、[原字段与父类型](results/player-market-protocol-fields.json)。

## 三、地图上的人物

`cjb.d(g7s,short)` VA `0x180aacaf0` 使用固定 MethodSpec `ci9.b<atd>`，按 `atd.cvx`（offset0x10）在 `ciy.a(long)` VA `0x18096f090` 查询本地实体；不存在时创建 `cp0` 并加入 `ciy` 实体表，然后分发事件。因此 key0可确认是**本地实体/对象ID**；其生命周期及是否跨地图复用未观察，不能与公开 GameInfo 的永久角色ID画等号。

创建时把 key1 string `cvy`、key4 int `cv1`、key7 Guid `cv4`、key64/65 strings `cxp/cxq` 传给 `cp0..ctor(long,int,Guid,string,string,string)`。本轮追加追踪已确认 key1 是角色显示名来源；key7 存角色对象 Guid，但未证明与公开 GameInfo 永久角色 ID 对应。key4、64/65 的业务名称仍未知。

`csc.v(g7s)` VA `0x180becdc0` 也使用 `ci9.b<atd>`。REA与已存反汇编交叉显示：key16（offset0x90）非null时，从其byte[]复制两个4-byte片段，经 `e0.a`→`BitConverter.ToSingle`→Vector2 写key14（0x80）；随后把转换后的片段写入key17（0x98）缓冲，再经e0.a/ToSingle重建key15（0x88）。因此本路径由key16驱动，key17是中间缓冲目的地，不能说两者都是独立输入payload。另从key14的两个float进入实体virtual slot20。具体e0.a算法、14/15各自业务含义和Photon物理编码未还原。REA Evidence `ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3`；派生修正见 [REA人物审阅修正](results/player-semantic-rea-review-corrections.json)，原始AO-META Evidence保留。

`MinimapBase.UpdateInterestAreaOnlyMarkerPositions` VA `0x18145ae00` 遍历已有 marker，再以 objectId查询 `ciy` 本地实体并调用 `MapLabel.SetPosition` VA `0x18132f150`。它展示客户端已知实体；不能由该代码推出亚洲服全世界人物列表、所有地图玩家位置或永久身份。服务器没有发送的实体不会从这些静态文件中出现。

以下字段表保留混淆名称；本轮确认或推导的语义见下节。未证明的装备槽位置、Guid 永久身份和 HP/最大HP标量对应保持未知。类型/参数键/offset可作为定位索引。

### 人物语义补充：当前静态证据

本轮把字段值继续追到实体 setter/getter 和命名 UI。**下面仍是当前客户端的静态结论；没有运行时包样本，也没有确认服务器总覆盖范围。** Guid、Vector2 等对象 native offset 不能作为 Photon 网络包字节偏移。

| 事件 | 参数键 | 字段 | 语义结论 | 确认范围 |
| --- | ---: | --- | --- | --- |
| NewCharacter=29 | 0 | `atd.cvx` | 客户端本地实体/对象 ID | 静态值流确认 |
| NewCharacter=29 | 1 | `atd.cvy` | 角色显示名来源（原始字符串） | 静态值流及 UI 用途确认 |
| NewCharacter=29 | 7 | `atd.cv4` | 角色对象 Guid 值；永久身份映射未知 | 静态值流确认，身份范围未知 |
| NewCharacter=29 | 8 | `atd.cv5` | 公会名来源（原始字符串） | 静态值流及 UI 关联确认 |
| NewCharacter=29 | 9 | `atd.cv6` | 公会 Guid | 强静态语义推导（值流与命名 UI 枚举一致） |
| NewCharacter=29 | 51 | `atd.cxc` | 联盟名来源（原始字符串） | 静态值流及 UI 关联确认 |
| NewCharacter=29 | 52 | `atd.cxd` | 联盟 Guid | 强静态语义推导（值流与命名 UI 枚举一致） |
| NewCharacter=29 | 10 | `atd.cv7` | 未知字符串；不是本轮确认的联盟名 | 仅确认值流 |
| NewCharacter=29 | 13 | `atd.cwa` | 未知字符串 | 仅确认值流 |
| NewCharacter=29 | 40 | `atd.cw1` | 装备物品索引数组 | 静态值流与装备变更事件共用处理确认 |
| CharacterEquipmentChanged=90 | 2 | `alv.bw4` | 装备物品索引数组，与 NewCharacter key40 同类 | 静态值流确认 |
| NewCharacter=29 | 43 | `atd.cw4` | bil 对象索引数组；外观/skin 候选 | 值流确认，具体语义未知 |
| NewCharacter=29 | 19 | `atd.cwg` | 未知 float | 仅确认值流 |
| NewCharacter=29 | 20 | `atd.cwh` | 未知 float | 仅确认值流 |
| HealthUpdate=6 | 2 | `ap9.ch6` | 生命值更新状态 float；当前HP/最大HP具体语义未确认 | 生命值处理链确认，标量业务意义未知 |
| HealthUpdate=6 | 3 | `ap9.ch7` | 生命值更新状态 float；当前HP/最大HP具体语义未确认 | 生命值处理链确认，标量业务意义未知 |

**显示名。** `cjb.d 0x180aacaf0 → cp0..ctor 0x180bd6ea0 → cp0.af(string) 0x180bd9010` 将key1作为显示名来源。正常分支把传入串存 `cp0.ajf+0x358`，名称处理结果或回退原串存 `cp0.ajg+0x360`；特殊匹配分支先调用0x1809700e0并把返回值存+0x358，再处理+0x360，因此+0x358不能在所有分支都称为未经加工的网络原名。`cp0.aeo 0x1805b9a90`（vtable slot24/offset0x2B8）读取+0x360后由 `CharacterName.UpdateText 0x1814afa00 → UILabel.set_text 0x1819426a0` 显示。REA Evidence `ev_e6913f7f59d91e7c49745ea72241e03b2f13b76e860498d2bf120b3fd623a34a`；原始AO-META mapping保留，派生修正另存 [REA人物审阅修正](results/player-semantic-rea-review-corrections.json)。

**公会与联盟。** `csc.v 0x180becdc0` 的 key8/9 分别进 `cp0.aer 0x180bd8db0` 和 `cp0.aep 0x180bd8da0`，key51/52 分别进 `cp0.aey 0x180bd8ee0` 和 `cp0.aew 0x180bd8ed0`。key9 Guid 存 `cp0.ai8+0x318`，getter `gy 0x180bda8e0` 为slot73；key52 Guid 存 `cp0.ajc+0x338`，getter `gz 0x180bda8f0` 为slot81。`CharacterName.CheckGuildAndAllianceStandingState 0x1814a55e0` 先后比较这两个非空Guid，分别产生standing6和standing5。

这里不再仅凭关系分支猜名称：`GetColorSettingForStanding 0x1814a72f0` 的原始跳表 `0x1814a7604` 把standing6指向 `0x1814a7480 → context9`，standing5指向 `0x1814a746c → context10`；当前命名枚举 `CharacterNameSettingContext.OtherGuild=9`、`OtherAlliance=10` 与值流一致。字符串getter `cp0.aes 0x1805b9b70` 读取处理后的公会名0x308，`cp0.aez 0x1805b9b80` 读取联盟名0x310；UpdateText把两者组合成公会/联盟文字。因此可作公会与联盟字段的强静态语义映射。**key10字符串是另一未知字段，不能再标成联盟名。**

**装备。** NewCharacter key40经 `cp0.af(int[]) 0x180bd9770` 将每个整数索引交给 `bdh.abo(int) 0x180609120` 查物品表，负值清空，得到的 `bc9[]` 存cp0+0x2C8。`CharacterEquipmentChanged=90` 的 key2由handler `crr.aft 0x180be90e0` 调用同一setter，确认两处是同类装备物品索引数组。具体数组index到EquipmentSlot的对应尚未证明。key43走另一 `cp0.x8(int[]) 0x180bdab90 → be7.abp(int) 0x18064db20 → bil[]`（cp0+0x2D0）；REA又确认装备变更key7同样调用0x180bdab90，因此第二数组同类关系明确，外观/skin仍只是候选。完整REA交叉审阅见 [PLAYER-REVIEW.md](../native/PLAYER-REVIEW.md)。

**生命值。** `HealthUpdate=6` 的 key2/3 float 经handler `cpo.aq7 0x180bc2cb0` 传 `a46..ctor 0x1808f16b0`，分别存a46+0x10/+0x18；virtual slot46进 `cpo.x8(a46) 0x180bc5aa0`，继而应用到实体+0x88的 `a44<a46>` 状态模型。`CharacterName.UpdateSliders 0x1814ae840` 通过当前状态函数取float，分母包含 entity+0xA4和状态对象+0x50，最终设置fillAmount。由于状态更新、时间插值和最大值变化的完整计算尚未还原，**不能直接把key2称为当前HP、key3称为最大HP**；NewCharacter key19/20同样保留未知。`HealthUpdates=7` 的并行数组handler `0x180bc2de0` 另存原始反汇编，标量语义边界相同。

完整值流、字段token/native offset、vtable原始条目、跳表原字节、命名枚举、泛型usage和引用文件SHA见 [player-semantic-mapping.json](results/player-semantic-mapping.json)，自定义Evidence `AO-META-PLAYER-SEMANTICS-001`。其中每个映射列出观察、语义结论与未决事项；原始mapping的姓名+0x358措辞需结合上文及派生 [REA审阅修正](results/player-semantic-rea-review-corrections.json) 读取，不修改原始Evidence。

### NewCharacter = 29，当前类型 `atd`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `atd.cvx` | `long` | `0x10` |
| 1 | `atd.cvy` | `string` | `0x18` |
| 2 | `atd.cvz` | `int` | `0x20` |
| 3 | `atd.cv0` | `int` | `0x24` |
| 4 | `atd.cv1` | `int` | `0x28` |
| 5 | `atd.cv2` | `int[]` | `0x30` |
| 6 | `atd.cv3` | `int[]` | `0x38` |
| 7 | `atd.cv4` | `System.Guid` | `0x40` |
| 8 | `atd.cv5` | `string` | `0x50` |
| 9 | `atd.cv6` | `System.Guid` | `0x58` |
| 10 | `atd.cv7` | `string` | `0x68` |
| 11 | `atd.cv8` | `Albion.Common.GameData.FounderState` | `0x70` |
| 12 | `atd.cv9` | `Albion.Common.GameData.FounderState` | `0x74` |
| 13 | `atd.cwa` | `string` | `0x78` |
| 14 | `atd.cwb` | `Albion.Common.Math.Vector2` | `0x80` |
| 15 | `atd.cwc` | `Albion.Common.Math.Vector2` | `0x88` |
| 16 | `atd.cwd` | `byte[]` | `0x90` |
| 17 | `atd.cwe` | `byte[]` | `0x98` |
| 18 | `atd.cwf` | `Albion.Common.Time.GameTimeStamp` | `0xa0` |
| 19 | `atd.cwg` | `float` | `0xa8` |
| 20 | `atd.cwh` | `float` | `0xac` |
| 21 | `atd.cwi` | `long` | `0xb0` |
| 22 | `atd.cwj` | `float` | `0xb8` |
| 23 | `atd.cwk` | `float` | `0xbc` |
| 24 | `atd.cwl` | `float` | `0xc0` |
| 25 | `atd.cwm` | `float` | `0xc4` |
| 26 | `atd.cwn` | `Albion.Common.Time.GameTimeStamp` | `0xc8` |
| 27 | `atd.cwo` | `float` | `0xd0` |
| 28 | `atd.cwp` | `float` | `0xd4` |
| 29 | `atd.cwq` | `float` | `0xd8` |
| 30 | `atd.cwr` | `float` | `0xdc` |
| 31 | `atd.cws` | `Albion.Common.Time.GameTimeStamp` | `0xe0` |
| 32 | `atd.cwt` | `float` | `0xe8` |
| 33 | `atd.cwu` | `float` | `0xec` |
| 34 | `atd.cwv` | `float` | `0xf0` |
| 35 | `atd.cww` | `float` | `0xf4` |
| 36 | `atd.cwx` | `Albion.Common.Time.GameTimeStamp` | `0xf8` |
| 37 | `atd.cwy` | `float` | `0x100` |
| 38 | `atd.cwz` | `Albion.Common.Time.GameTimeStamp` | `0x108` |
| 39 | `atd.cw0` | `byte` | `0x110` |
| 40 | `atd.cw1` | `int[]` | `0x118` |
| 41 | `atd.cw2` | `long` | `0x120` |
| 42 | `atd.cw3` | `int` | `0x128` |
| 43 | `atd.cw4` | `int[]` | `0x130` |
| 44 | `atd.cw5` | `int` | `0x138` |
| 45 | `atd.cw6` | `int` | `0x13c` |
| 46 | `atd.cw7` | `int` | `0x140` |
| 47 | `atd.cw8` | `int` | `0x144` |
| 48 | `atd.cw9` | `int` | `0x148` |
| 49 | `atd.cxa` | `float` | `0x14c` |
| 50 | `atd.cxb` | `float` | `0x150` |
| 51 | `atd.cxc` | `string` | `0x158` |
| 52 | `atd.cxd` | `System.Guid` | `0x160` |
| 53 | `atd.cxe` | `byte` | `0x170` |
| 54 | `atd.cxf` | `atd+a` | `0x171` |
| 55 | `atd.cxg` | `int[]` | `0x178` |
| 56 | `atd.cxh` | `Albion.Common.GameData.FlaggingStatus` | `0x180` |
| 57 | `atd.cxi` | `int` | `0x184` |
| 58 | `atd.cxj` | `bool` | `0x188` |
| 59 | `atd.cxk` | `int` | `0x18c` |
| 60 | `atd.cxl` | `float` | `0x190` |
| 61 | `atd.cxm` | `bool` | `0x194` |
| 62 | `atd.cxn` | `bool` | `0x195` |
| 63 | `atd.cxo` | `Albion.Common.GameData.EquipmentSlot` | `0x198` |
| 64 | `atd.cxp` | `string` | `0x1a0` |
| 65 | `atd.cxq` | `string` | `0x1a8` |

### Move = 3，当前类型 `as2`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `avk.ddn` | `long` | `0x10` |
| 1 | `as2.csx` | `Albion.Common.Time.GameTimeStamp` | `0x18` |
| 2 | `as2.csy` | `Albion.Common.Math.Vector2` | `0x20` |
| 3 | `as2.csz` | `float` | `0x28` |
| 4 | `as2.cs0` | `float` | `0x2c` |
| 5 | `as2.cs1` | `Albion.Common.Math.Vector2` | `0x30` |
| 6 | `as2.cs2` | `long` | `0x38` |
| 7 | `as2.cs3` | `bool` | `0x40` |
| 8 | `as2.cs4` | `byte[]` | `0x48` |

### Leave = 1，当前类型 `arp`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `arp.cnd` | `long` | `0x10` |

### CharacterEquipmentChanged = 90，当前类型 `alv`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `avk.ddn` | `long` | `0x10` |
| 1 | `alv.bw3` | `Albion.Common.Time.GameTimeStamp` | `0x18` |
| 2 | `alv.bw4` | `int[]` | `0x20` |
| 3 | `alv.bw5` | `long` | `0x28` |
| 4 | `alv.bw6` | `int` | `0x30` |
| 5 | `alv.bw7` | `Albion.Common.Time.GameTimeStamp` | `0x38` |
| 6 | `alv.bw8` | `byte` | `0x40` |
| 7 | `alv.bw9` | `int[]` | `0x48` |
| 8 | `alv.bxa` | `bool` | `0x50` |
| 9 | `alv.bxb` | `bool` | `0x51` |

### HealthUpdate = 6，当前类型 `ap9`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `avk.ddn` | `long` | `0x10` |
| 1 | `ap9.ch5` | `Albion.Common.Time.GameTimeStamp` | `0x18` |
| 2 | `ap9.ch6` | `float` | `0x20` |
| 3 | `ap9.ch7` | `float` | `0x24` |
| 4 | `ap9.ch8` | `byte` | `0x28` |
| 5 | `ap9.ch9` | `byte` | `0x29` |
| 6 | `ap9.cia` | `long` | `0x30` |
| 7 | `ap9.cib` | `short` | `0x38` |
| 8 | `ap9.cic` | `bool` | `0x3a` |

### HealthUpdates = 7，当前类型 `aqa`

| 参数键 | 混淆字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `avk.ddn` | `long` | `0x10` |
| 1 | `aqa.cid` | `Albion.Common.Time.GameTimeStamp[]` | `0x18` |
| 2 | `aqa.cie` | `float[]` | `0x20` |
| 3 | `aqa.cif` | `float[]` | `0x28` |
| 4 | `aqa.cig` | `byte[]` | `0x30` |
| 5 | `aqa.cih` | `byte[]` | `0x38` |
| 6 | `aqa.cii` | `long[]` | `0x40` |
| 7 | `aqa.cij` | `short[]` | `0x48` |

其它相关当前事件：GuildUpdate=103、GuildPlayerUpdated=104、GuildMemberWorldUpdate=106、PartyPlayerUpdated=238、MountHealthUpdate=312；属性/字段已保存。公会/队伍订阅具有不同范围，不能把它们当成所有人的全球信息。人物装备请求 GetCharacterEquipment=143 的 request `td` 两字段：Guid、bool；response `ae5`：Guid、long[]、byte[]、float。Guid与NewCharacter Guid的对应尚未证明。

## 四、市场

### 订单数据链：已观察

`AuctionGetOffers=81` response `ad1` key0 string[] `am5`、key1 bool `am6`；`AuctionGetRequests=82` response `ad2` key0 string[] `am7`、key1 bool `am8`。

`dgv.aov(g7q)` VA `0x180b27010` → `ci9.b<ad1>` folded parser `0x182004220` → 遍历响应 string[] → `dgc..ctor(string)` VA `0x180b20e70` → `JsonFx.Json.JsonReader.Deserialize<AuctionDTO>` folded code `0x181edb0f0`（非泛型Deserialize主体 `0x182f5acc0`）→市场列表缓存。`dgw.aov(g7q)` VA `0x180b27340` 对 `ad2` 走对应链。UI `MarketPlaceBuyView.OnOffersUpdated(ddc)` VA `0x180fb3840`、`MarketPlaceBuyOrdersView.OnRequestsUpdated(ddh)` VA `0x180fb2df0` 接受通知。

Deserialize 泛型精确证据：usage slot `0x184dca358`, file offset81559384, encoded `0xc005d633`, MethodSpec191257（file offset70824140），methodDef198875，泛型参数typeDef15578 `Albion.Common.Auction.AuctionDTO`。因此 JSON订单类型已有静态调用链证明。

### AuctionDTO 21项字段

这些字段在客户端 DTO 中存在；服务器一次响应是否填满、买卖双方哪些字段对观察者可见、空值与脱敏行为都未观察。

| 属性 | CLR 类型 | native backing-field offset |
| --- | --- | --- |
| `Id` | `long` | `0x10` |
| `UnitPriceSilver` | `long` | `0x18` |
| `DistanceFee` | `long` | `0x20` |
| `TotalPriceSilver` | `long` | `0x28` |
| `Amount` | `int` | `0x30` |
| `Tier` | `int` | `0x34` |
| `IsFinished` | `bool` | `0x38` |
| `AuctionType` | `string` | `0x40` |
| `HasBuyerFetched` | `bool` | `0x48` |
| `HasSellerFetched` | `bool` | `0x49` |
| `SellerCharacterId` | `string` | `0x50` |
| `SellerName` | `string` | `0x58` |
| `BuyerCharacterId` | `string` | `0x60` |
| `BuyerName` | `string` | `0x68` |
| `ItemTypeId` | `string` | `0x70` |
| `ItemGroupTypeId` | `string` | `0x78` |
| `EnchantmentLevel` | `int` | `0x80` |
| `QualityLevel` | `int` | `0x84` |
| `Expires` | `System.DateTime` | `0x88` |
| `ReferenceId` | `System.Guid` | `0x90` |
| `LocationId` | `string` | `0xa0` |

### 其它当前市场操作

| 操作 | 当前code | response字段（继承的错误字段被过滤） |
| --- | ---: | --- |
| AuctionGetItemAverageStats | 95 | adw：long[] amw、long[] amx、GameTimeStamp[] amy |
| AuctionGetItemAverageValue | 96 | adx：long amz |
| AuctionGetLowestOfferPrices | 97 | adz：short[] am0、a7m[] am1 |

请求类型 `aj2`（Offers）17参数、`aj3`（Requests）15参数，包含继承的 long `aj8.bqs` 和物品/筛选/页码相关候选字段；完整当前表见参数schema。筛选串、排序enum、分页终止规则与数量上限语义未逐项证明，不能从这些类型声称一个请求能读取全服订单。DTO UnitPriceSilver 的int64存储已证明；与公开AODP整银单位的转换不由本专题单独确认。

获取完整市场订单需要知道当前服务器、市场地点、物品/品质/附魔过滤和分页范围，并实际观察对应响应。安装目录没有市场实时数据库；静态 metadata是结构，不包含当前价格或当前挂单。公开AODP价格聚合与游戏内全量订单属于不同数据产品，由主报告核对公开接口。

## 五、GetClusterMapInfo 地图实时信息

当前 OperationCodes.GetClusterMapInfo=197：request `tj` 参数key0 `mr:string`；response `afb` 92个公开实例字段，`ah0`父错误字段被过滤。

`Albion.PhotonClient.dll:n.b(g7q)` VA `0x180a9c910` 检查 response.ReturnCode offset0x12 ==0，再调用 `ci9.b<afb>` folded `0x182004220`，把结果交给 callback。属性识别和泛型调用一起证明这是当前地图信息响应类。

观察的类型类别包含 cluster string、timestamp、Guid/名称数组、Vector2/float[][]位置数组、AccessRights.Preset、FactionFortressState、FlaggingStatus、LootChests.ChestState、FixedRotations。每一数组与“领地/藏身处/堡垒/箱子/入口”等具体对象的语义对应仍需追 callback。不能仅凭字段类型将它们解释为地图上全部实时实体，也没有发现此响应是全服玩家清单的证据。

| 参数键 | 字段 | CLR 类型 | native offset |
| ---: | --- | --- | --- |
| 0 | `ati` | `string` | `0x20` |
| 1 | `atj` | `Albion.Common.Time.GameTimeStamp` | `0x28` |
| 2 | `atk` | `int` | `0x30` |
| 3 | `atl` | `string[]` | `0x38` |
| 4 | `atm` | `System.Guid[]` | `0x40` |
| 5 | `atn` | `System.Guid[]` | `0x48` |
| 6 | `ato` | `string[]` | `0x50` |
| 7 | `atp` | `string[]` | `0x58` |
| 8 | `atq` | `byte[]` | `0x60` |
| 9 | `atr` | `byte[]` | `0x68` |
| 10 | `ats` | `long[]` | `0x70` |
| 11 | `att` | `byte[]` | `0x78` |
| 12 | `atu` | `Albion.Common.Time.GameTimeStamp[]` | `0x80` |
| 13 | `atv` | `Albion.Common.Time.GameTimeStamp[]` | `0x88` |
| 14 | `atw` | `a7m[]` | `0x90` |
| 15 | `atx` | `Albion.Common.Time.GameTimeStamp[]` | `0x98` |
| 16 | `aty` | `long[]` | `0xa0` |
| 17 | `atz` | `int[]` | `0xa8` |
| 18 | `at0` | `float[][]` | `0xb0` |
| 19 | `at1` | `float[]` | `0xb8` |
| 20 | `at2` | `long[]` | `0xc0` |
| 21 | `at3` | `long[]` | `0xc8` |
| 22 | `at4` | `long[]` | `0xd0` |
| 23 | `at5` | `long[]` | `0xd8` |
| 24 | `at6` | `long[]` | `0xe0` |
| 25 | `at7` | `string[]` | `0xe8` |
| 26 | `at8` | `System.Guid[]` | `0xf0` |
| 27 | `at9` | `System.Guid[]` | `0xf8` |
| 28 | `aua` | `System.Guid[]` | `0x100` |
| 29 | `aub` | `System.Guid[]` | `0x108` |
| 30 | `auc` | `bool[]` | `0x110` |
| 31 | `aud` | `Albion.Common.SimulationObjects.AccessRights.Preset[]` | `0x118` |
| 32 | `aue` | `bool[]` | `0x120` |
| 33 | `auf` | `long[]` | `0x128` |
| 34 | `aug` | `long[]` | `0x130` |
| 35 | `auh` | `long[]` | `0x138` |
| 36 | `aui` | `float[][]` | `0x140` |
| 37 | `auj` | `float[][]` | `0x148` |
| 38 | `auk` | `bool[]` | `0x150` |
| 39 | `aul` | `long[]` | `0x158` |
| 40 | `aum` | `System.Guid[]` | `0x160` |
| 41 | `aun` | `System.Guid[]` | `0x168` |
| 42 | `auo` | `long[]` | `0x170` |
| 43 | `aup` | `Albion.Common.Math.Vector2[]` | `0x178` |
| 44 | `auq` | `int[]` | `0x180` |
| 45 | `aur` | `int[]` | `0x188` |
| 46 | `aus` | `bool[]` | `0x190` |
| 47 | `aut` | `System.Guid[]` | `0x198` |
| 48 | `auu` | `string[]` | `0x1a0` |
| 49 | `auv` | `System.Guid[]` | `0x1a8` |
| 50 | `auw` | `string[]` | `0x1b0` |
| 51 | `auy` | `int[]` | `0x1b8` |
| 52 | `auz` | `int[]` | `0x1c0` |
| 53 | `au0` | `Albion.Common.Time.GameTimeStamp[]` | `0x1c8` |
| 54 | `au1` | `bool[]` | `0x1d0` |
| 55 | `au2` | `bool[]` | `0x1d8` |
| 56 | `au3` | `a7m[]` | `0x1e0` |
| 57 | `au4` | `Albion.Common.Time.GameTimeStamp[]` | `0x1e8` |
| 58 | `au5` | `bool[]` | `0x1f0` |
| 59 | `au6` | `long` | `0x1f8` |
| 60 | `au7` | `int` | `0x200` |
| 61 | `au8` | `Albion.Common.GameData.FlaggingStatus` | `0x204` |
| 62 | `au9` | `Albion.Common.GameData.FactionWarfare.FactionFortressState` | `0x205` |
| 63 | `ava` | `Albion.Common.GameData.FlaggingStatus` | `0x206` |
| 64 | `avb` | `bool` | `0x207` |
| 65 | `avc` | `System.Guid[]` | `0x208` |
| 66 | `avd` | `long[]` | `0x210` |
| 67 | `ave` | `long[]` | `0x218` |
| 68 | `avf` | `byte[]` | `0x220` |
| 69 | `avg` | `byte[]` | `0x228` |
| 70 | `avh` | `System.Guid[]` | `0x230` |
| 71 | `avi` | `int[]` | `0x238` |
| 72 | `avj` | `byte[]` | `0x240` |
| 73 | `avk` | `System.Guid` | `0x248` |
| 74 | `avl` | `System.Guid` | `0x258` |
| 75 | `avm` | `bool` | `0x268` |
| 76 | `avn` | `bool` | `0x269` |
| 77 | `avo` | `Albion.Common.GameData.LootChests.ChestState` | `0x26c` |
| 78 | `avp` | `string` | `0x270` |
| 79 | `avq` | `long[]` | `0x278` |
| 80 | `avr` | `Albion.Common.Math.Vector2[]` | `0x280` |
| 81 | `avs` | `a9u+FixedRotations[]` | `0x288` |
| 82 | `avt` | `int[]` | `0x290` |
| 83 | `avu` | `System.Guid[]` | `0x298` |
| 84 | `avv` | `byte[]` | `0x2a0` |
| 85 | `avw` | `Albion.Common.Time.GameTimeStamp[]` | `0x2a8` |
| 86 | `avx` | `Albion.Common.Math.Vector2[]` | `0x2b0` |
| 87 | `avy` | `long[]` | `0x2b8` |
| 88 | `avz` | `float[]` | `0x2c0` |
| 89 | `av0` | `Albion.Common.Math.Vector2[]` | `0x2c8` |
| 90 | `av1` | `long[]` | `0x2d0` |
| 91 | `av2` | `bool[]` | `0x2d8` |

GetClusterData=40 response `ae9` key0 `atg:byte[]`；SubscribeToCluster=141 request `abl` key0 `agl:string[]`；ChangeCluster=41 response `aec`13字段；GetPartyMemberCluster=546 request string/response string。当前字段和证据保存，payload具体格式、订阅权限和服务器实际覆盖未观察。

## 六、证据索引与恢复

| 自定义Evidence ID | 文件 | 支持内容 |
| --- | --- | --- |
| AO-META-HEADER-001 | results/metadata-header.json | metadata身份、v39结构与校验 |
| AO-META-MODULES-001 | results/native-modules.json | binary身份、93 codegen modules |
| AO-META-REGISTRATION-001 | results/native-metadata-registration.json | type/fieldOffsets/MethodSpec注册表 |
| AO-META-CONSTANTS-001 | results/all-field-constants.json | 当前协议常量原值及原字节 |
| AO-META-PROTOCOL-ATTRIBUTES-001 | results/protocol-attributes.json | 1488类型的code属性与原始blob |
| AO-META-PLAYER-MARKET-FIELDS-001 | results/player-market-protocol-fields.json | 字段类型、token、原序号、native offset |
| AO-META-PARAMETER-ORDER-001 | results/protocol-parameter-schema.json | 选定字段的静态参数键，排序证据链 |
| AO-META-PLAYER-SEMANTICS-001 | results/player-semantic-mapping.json | 姓名、公会/联盟、装备与Health值流；语义和未知边界 |
| AO-META-GENERIC-USAGES-001 | results/generic-usage-resolution.json | 泛型usage/MethodSpec/参数类型 |
| AO-META-GENERIC-CALLERS-001 | results/focused-generic-parser-callers.json | afb/ad1/ad2/atd调用候选和原指令 |
| AO-META-DISASM-〈VA〉-adjacent | results/disassembly-〈VA〉-adjacent.json | 限定PE unwind范围的原字节、反汇编、SHA |
| AO-META-LEAF-〈VA〉 | results/leaf-〈VA〉.json | 无.pdata的短getter/setter候选原字节与SHA；最多64字节并停止于首RET/JMP |

反汇编来自仅安装于本目录 `dependencies/` 的 Capstone5.0.9。`--adjacent` 包含下一metadata命名方法之前连续.pdata fragments，fragment是否属于同一逻辑函数是一项推断；符号来自metadata pointer slots，folded实现可能有多个名称。线性反汇编没有重建完整CFG，也不是运行证据。

恢复命令（不会执行游戏）：

```powershell
$metadataPython = 'C:/Users/ludy/AppData/Local/Programs/Python/Python314/python.exe'
& $metadataPython investigations/albion-online/metadata/parse_metadata.py
& $metadataPython investigations/albion-online/metadata/protocol_attributes.py
& $metadataPython investigations/albion-online/metadata/wire_schema.py
& $metadataPython investigations/albion-online/metadata/generic_usages.py
& $metadataPython investigations/albion-online/metadata/write_semantic_mapping.py
& $metadataPython investigations/albion-online/metadata/write_report.py
```

脚本只接受本次v39结构；客户端更新后 SHA/enum/code/type/参数布局均须重新定位。已有解析结果可直接复用，无需重新运行。完整产物SHA索引见 `manifest.json`。
