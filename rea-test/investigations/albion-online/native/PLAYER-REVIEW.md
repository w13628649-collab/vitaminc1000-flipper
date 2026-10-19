# 人物与小地图 REA 结果交叉审阅

日期：2026-10-13。只读复核已经保存的八个 REA function dossier、伪代码和既有 AO-META 字段/泛型/vtable/反汇编；没有打开新会话、运行游戏、提取新目标内容或执行测试。原始 REA 与 AO-META Evidence 未修改。

## 1. 结论与输入身份

REA 结果支持既有的人物创建、显示名来源、装备物品索引和本地小地图值流。**没有新证据把本地 entity ID 或 NewCharacter key7 Guid 等同于 GameInfo 永久 player ID；也没有取得隐藏、跨区或全服实时人物数据。** 联盟为key51/52，key10/13字符串仍未知；生命值key2/3不能直接命名当前HP/最大HP。

本轮收紧两处措辞：姓名+0x358在特殊分支也会被加工；坐标key16驱动解码并写key17缓冲，不能说两者均为独立输入。新增第二索引数组共用setter关系：NewCharacter key43与装备变更key7都调用0x180bdab90，skin含义仍只是候选。

输入：`C:/Users/ludy/AppData/Local/AlbionOnline/game/GameAssembly.dll`，SHA-256 `19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202`；metadata SHA-256 `b0cffb9a6945366c0057390c2144edbfb7b9fb0b9a2f327ed8517e3051b7684b`；游戏1.32.030.345319，用户East/Asia。全部VA均为文件首选地址，不是运行时ASLR地址。

八份Evidence均为Ghidra12.1.4、`confidence=observed`、`authority=shipped-artifact`，profile digest `96664c5e569b8baa0db21aa3e46d10a6d2eaff5fe5324a1fbaa54ee3a5a61643`。该profile完成的是Ghidra default加禁用 Non-Returning Functions - Discovered、Decompiler Switch Analysis、x86 Constant Reference Analyzer、Function ID；不是未改变配置的默认分析。**本次成功Evidence没有使用seeded-noanalysis。**

## 2. 精确 REA Evidence 与覆盖

body bytes/span bytes及区间来自Ghidra FunctionManager；范围是inclusive。高pcode完整性单独记录，不由完整Function body推定。来源响应的SHA见下一表及 [PLAYER-REVIEW.json](PLAYER-REVIEW.json)。

| 保存名称 / VA | 真实 REA Evidence ID | body bytes / span | native_value_flow |
| --- | --- | ---: | --- |
| [new-character-message-handler](evidence/new-character-message-handler.json) / `0x180aacaf0` | `ev_1432a2360489cab337f425cde48c2fe3f515b2e1eb2919145f4d50970f6e5657` | 838 / 838 | 未报告截断 |
| [new_character_state_application](evidence/new_character_state_application.json) / `0x180becdc0` | `ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3` | 6978 / 6978 | 截断；省略下界1 |
| [character_name_setter](evidence/character_name_setter.json) / `0x180bd9010` | `ev_e6913f7f59d91e7c49745ea72241e03b2f13b76e860498d2bf120b3fd623a34a` | 753 / 753 | 未报告截断 |
| [equipment_indices_setter](evidence/equipment_indices_setter.json) / `0x180bd9770` | `ev_7fb7b0995d7fc471f6c8a8c73f7b0626922f7f0018508a15e5c6d6e718f37c88` | 786 / 786 | 未报告截断 |
| [equipment_changed_handler](evidence/equipment_changed_handler.json) / `0x180be90e0` | `ev_c29961641b5e48548c4f096918dd40095a62c188c27b676dd6bdd5ac36e2b728` | 1087 / 1087 | 未报告截断 |
| [health_update_handler](evidence/health_update_handler.json) / `0x180bc2cb0` | `ev_77566fb7d680d514d7c0ec8f57b221fba9e088470a46d63bcabf4a3d0778dff5` | 303 / 303 | 未报告截断 |
| [minimap-interest-area-position-updates](evidence/minimap-interest-area-position-updates.json) / `0x18145ae00` | `ev_5297bf2d8ea7ce8c8ed7aa8908d17209596678f26bc978e2a8964d5bceb7ee3b` | 585 / 612 | 未报告截断 |
| [minimap-party-member-position-updates](evidence/minimap-party-member-position-updates.json) / `0x18145c270` | `ev_a2e8f7a470169f9315f5f94e1ae1b18763fb01a06b5ab2ea88c78e3ee72f5325` | 2886 / 3132 | 截断；省略下界1 |

| 原始响应 | SHA-256 |
| --- | --- |
| `evidence/new-character-message-handler.json` | `fa36b5cb30c6f2e5772219305889583608ebe2a489f35b023183d457e281159f` |
| `evidence/new_character_state_application.json` | `e5e832ade71eaf518876b09b677e9fba01fb10f39c7a675f3524fed9da01b9a1` |
| `evidence/character_name_setter.json` | `b5632d10bf7ad792a8e1efcb8bec937c8550a497c3e309f11a39fcdda1d4bbba` |
| `evidence/equipment_indices_setter.json` | `ca41fd33f3ff069a381702594460972280f4a702c86f9e74b117139ad7b86b14` |
| `evidence/equipment_changed_handler.json` | `0c3e16b92e3ac3e828345a17027bdd3baf79000f71c7bf2b9a2421573f578dcf` |
| `evidence/health_update_handler.json` | `806eb9e3731043b608ad9455c382a2f5f6455c664bc61514c1b4952fe7f3e41e` |
| `evidence/minimap-interest-area-position-updates.json` | `367c3516ae2093b2497d1c9d79150b91affc9239df12b3324eaaee888e3260f2` |
| `evidence/minimap-party-member-position-updates.json` | `54b38f78c8e86eec15b5ea4361f89703c7d57786ea72b9ecb953bec42ad1b253` |

## 3. 人物创建与状态应用

`new-character-message-handler`（0x180aacaf0）用 `FUN_182003eb0` 和usage slot0x184dd6228恢复消息。既有MethodSpec将其锁定为`ci9.b<atd>`。代码将atd+0x10交给本地实体查询0x18096f090；已有匹配实体走virtual0x238，否则创建cp0，构造参数含key0/key4/key7/key1/key64/key65，加入本地表后再调virtual0x238。该观察支持“创建或复用本地实体”，不能推定跨会话ID永久性。

`new_character_state_application`（0x180becdc0）使用同一atd usage。key7 Guid从0x40..0x4C复制到实体0x1F0..0x1FC，key10/13从0x68/0x78存实体0x208/0x218。key8/9进入公会setter0x5F8/0x5D8，key51/52进入联盟setter0x678/0x658；元数据vtable解析和此前standing/color枚举证据支持公会/联盟语义。非空公会字符串分支才继续写联盟信息，字段赋值存在条件；本次没有服务器样本证明每次响应填充或清除所有关系字段。

**坐标细化。** 如果atd+0x90（key16）非null，代码从该缓冲复制两个4byte片段，经0x1808e2a40（既有符号e0.a）和0x1830c8fa0（BitConverter.ToSingle）构建Vector2，写atd+0x80（key14）。随后把转换后的片段写入atd+0x98（key17）缓冲，再经e0.a/ToSingle构建Vector2写atd+0x88（key15）。本分支中key16是解码源、key17是中间缓冲目的地，不能描述为两种独立坐标payload。e0.a的完整算法、网络物理编码、14/15各自意义及坐标投影仍未知。

证据：REA `ev_1432a2360489cab337f425cde48c2fe3f515b2e1eb2919145f4d50970f6e5657`、`ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3`；交叉 `AO-META-PLAYER-SEMANTICS-001`、`AO-META-GENERIC-USAGES-001`、`AO-META-PARAMETER-ORDER-001` 及原始 `AO-META-DISASM-180becdc0-adjacent`。

## 4. 姓名与保存字段

`character_name_setter`（0x180bd9010）正常分支将传入key1来源字符串存实体+0x358，把处理结果或回退原串存+0x360。特殊匹配分支先调0x1809700e0，把返回值写+0x358，再进行显示加工写+0x360，并设置0x2D8标记。因此**+0x358不能在所有分支都称为未经加工的网络原名**。key1是人物显示名称来源的结论不变；UI getter0x1805b9a90读取+0x360的用途由此前vtable与CharacterName.UpdateText链支撑，本轮没有另分析该UI函数。

证据：REA `ev_e6913f7f59d91e7c49745ea72241e03b2f13b76e860498d2bf120b3fd623a34a`；交叉原始 `AO-META-DISASM-180bd9010-adjacent`、`AO-META-LEAF-1805b9a90` 与 `AO-META-PLAYER-SEMANTICS-001`。

## 5. 装备索引与第二数组

`equipment_indices_setter`（0x180bd9770）逐int元素处理输入数组。负值清空对应bc9[]位置；非负值经0x180609120查物品表，查询失败或不满足目标类型也清空；匹配对象存实体+0x2C8。不能把数字值直接当成本地item_id或公开unique name；index到EquipmentSlot仍未恢复。

`new_character_state_application`将atd+0x118（key40）交给同一setter。`equipment_changed_handler`（0x180be90e0）以usage0x184dc2688恢复`ci9.b<alv>`，把alv+0x20（key2）交给0x180bd9770，交叉确认同类装备物品索引数组。

两者的第二数组亦共用setter：atd+0x130（key43）和alv+0x48（key7）均交给0x180bdab90。此前AO-META定位该setter返回bil[]到实体+0x2D0；这里只确认它们是同类第二对象索引数组，**没有新证据确认skin/外观用途或槽位对应**。

证据：REA `ev_7fb7b0995d7fc471f6c8a8c73f7b0626922f7f0018508a15e5c6d6e718f37c88`、`ev_c29961641b5e48548c4f096918dd40095a62c188c27b676dd6bdd5ac36e2b728`、`ev_905c916ab1b812d7bb7d64cca4d302e59fe5290bcce60f9fc90f597acbe37fb3`；交叉 `AO-META-PARAMETER-ORDER-001`、`AO-META-PLAYER-SEMANTICS-001`。

## 6. 生命相关状态：限制不变

`health_update_handler`（0x180bc2cb0）调用parser0x182003eb0，usage0x184dcaf78固定为`ci9.b<ap9>`。读取消息+0x20/+0x24（元数据CLR float，key2/3）并传0x1808f16b0构造a46，然后通过vtable0x418间接跳转。既有metadata slot46解析为cpo.x8(a46)，此前ctor反汇编确认两个float存a46+0x10/+0x18。REA伪代码将它们显示为`undefined4`，不是新的业务类型判断。

这里的 `UNRECOVERED_JUMPTABLE` 是Ghidra没有恢复该间接控制流的警告；结合vtable证据可识别virtual调用，不能把警告当作已解析业务switch。本轮dossier没有包含a44状态演进和滑条公式全部函数，仍不能将key2直接称当前HP、key3称最大HP，也未确认伤害增量或插值行为。

证据：REA `ev_77566fb7d680d514d7c0ec8f57b221fba9e088470a46d63bcabf4a3d0778dff5`；交叉 `AO-META-DISASM-1808f16b0-adjacent`、`AO-META-PLAYER-SEMANTICS-001`中的slot46与health_generic_usages。

## 7. 小地图与队伍标记

`minimap-interest-area-position-updates`（0x18145ae00）枚举已有marker容器（实例+0x58），用objectId经0x18096f090查本地实体，匹配实体类型后经virtual0x268取位置并调用MapLabel.SetPosition（0x18132f150）。不存在实体时跳过；该函数没有拉取全服角色或隐藏对象的入口。

`minimap-party-member-position-updates`（0x18145c270）依赖已有队伍状态和传入cluster字符串条件，遍历队伍成员，用成员记录+0x48查本地实体，对可匹配对象创建或更新marker，调用位置setter/平滑更新，并清理不在队伍集合内的旧marker。这是队伍集合与已有实体的标记管理路径；本次没有确定队伍跨区位置的网络订阅范围，不能推定全部队伍或全地图位置可取得。

证据：REA `ev_5297bf2d8ea7ce8c8ed7aa8908d17209596678f26bc978e2a8964d5bceb7ee3b`、`ev_a2e8f7a470169f9315f5f94e1ae1b18763fb01a06b5ab2ea88c78e3ee72f5325`；交叉此前Minimap/MapLabel metadata命名与限定反汇编。

## 8. 两个“router”保存名称的范围修正

这是对已有usage记录的只读核对，不是新增函数调查。保存名`photon-event-router`的0x18097f270使用slot0x184dc2ff0（encoded0xC005E437），MethodSpec193051固定`ci9.b<am2>`，typeDef12072，当前协议属性 **Died=164**。保存名`photon-operation-response-router`的0x18097d820使用slot0x184db9160（encoded0xC005E34B），MethodSpec192933固定`ci9.b<ahi>`，typeDef11868，当前协议属性 **Join=2 response**。两者是特定typed handler，不能作为整个event/operation总路由的证据。

依据：`AO-META-GENERIC-USAGES-001`的两函数记录、`AO-META-PROTOCOL-ATTRIBUTES-001`的am2/ahi属性；原始保存文件名称保持不变，native报告应说明此界限。

## 9. 审阅限制和修正存放

- Ghidra伪代码是provider表示，不是原始C#代码或运行行为。`undefined`参数、重叠globals、terminal call与未知no-return可能影响类型和边界解读；原始dossier均保留限制。
- `new_character_state_application`与队伍marker函数的高pcode报告截断，各省略operation下界1；未报告遗漏inputs/edges不等于不存在全部未知遗漏。完整Function body和未截断伪代码不等于完整跨函数def-use。
- native offset指解码后对象偏移，不能当作网络字节偏移。byte键来自已恢复setter排序；服务器实际条件省略、网络编码和兴趣半径仍未测量。
- 原始 `player-semantic-mapping.json`不改；姓名与坐标措辞细化单独保存为 [player-semantic-rea-review-corrections.json](../metadata/results/player-semantic-rea-review-corrections.json)，引用原始文件SHA及准确REA Evidence ID。它是派生审阅修正，不是新的REA Evidence。
