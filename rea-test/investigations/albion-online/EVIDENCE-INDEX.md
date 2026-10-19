# 调查证据索引

目标及用户问题见 `SCOPE.md`。所有客户端结论绑定本地版本 `1.32.030.345319`。

| 路线 | 产物 | 证据性质与边界 |
| --- | --- | --- |
| REA 服务信息 | `../../.rea/environment.json`、`../../.rea/mcp-tools.json` | 安装版 6.4.0、实际 170 个 schema，不是目标分析 |
| REA 调查隔离构建 | `native/vendor/rea/`、`native/vendor/rea/native/windows/build/artifact-provenance.json`、`native/initialization.json`、`native/attempt-profile-four/` | Node启动构建基于 d13c9f840d36a321a56917e75c48afcb8d72920e；延迟编译Java读取本地兼容扩展并走default分支。191 tools / 142 CLI / skill v41，不同于全局npm发布构建；addon源码不相同，按ABI及实际能力限定。磁盘seed扩展及hash另见provenance，seeded-noanalysis未使用 |
| 原生 REA | `native/evidence/*.json`、`native/calls.jsonl`、`native/evidence-bundle.json`、`native/results/SUMMARY.json` | 已完成overview与23选定函数，共24条Evidence；完整bundle 28,998,233bytes，真实ev及逐项限制见原响应。summary是派生索引，不能替代原Evidence |
| 人物原生复核 | `native/PLAYER-REVIEW.md`、人物函数原响应 | 结合REA伪代码/汇编及metadata字段/vtable/generic usage，细化姓名特殊分支、坐标16→17缓冲与14/15、装备数组和Health限制；不证明实时字段值或全部人物覆盖 |
| REA结束与清理 | `native/lifecycle-final.json`、`native/evidence/export-bundle.json`、`native/evidence/close-target.json` | 导出24records并close成功，driver8446退出0，持有进程和临时目录清理；导出unknowns=0不是所有业务问题已解 |
| 元数据 | `metadata/results/metadata-header.json`、`native-modules.json`、`all-method-symbols.json`、`all-field-constants.json` | 当前 IL2CPP v39 的名字、类型、常量和原生方法指针；这是脚本证据 `AO-META-*`，不是 REA Evidence |
| 元数据辅助反汇编 | `metadata/results/disassembly-*.json/.txt` | 基于 PE 文件字节、.pdata 和 Capstone 的静态指令，`AO-META-DISASM-*`；共享泛型实例须保留候选，不能当唯一函数名 |
| 当前协议属性与字段 | `metadata/REPORT.md`、`metadata/results/protocol-attributes.json`、`metadata/results/protocol-parameter-schema.json`、`metadata/manifest.json` | 1488 个请求/响应/事件类型属性及所选参数键；AO-META-PARAMETER-ORDER-001 建立静态字段顺序，CLR/native 对象类型及 offset 不等于 Photon 物理包编码 |
| 人物字段语义 | `metadata/results/player-semantic-mapping.json`、`metadata/results/player-semantic-rea-review-corrections.json`、`metadata/REPORT.md` | AO-META-PLAYER-SEMANTICS-001；姓名key1、公会8/9、联盟51/52、装备40/事件90键2；带有字段值流、命名UI、vtable、跳表和范围限制。原始mapping保留，REA审阅修正另存；Guid7公开身份、装备槽、生命标量含义未确认 |
| 本地资源全量解码 | `static-data/extraction-manifest.json`、`static-data/decoded/` | 每文件输入/输出 SHA-256、解码状态、源路径与格式；不含实时实例状态 |
| 地图结构化目录 | `static-data/catalogs/map-manifest.json`、`map-clusters.jsonl`、`template-index.json`、`template-placements.jsonl`、`tile-semantics.json` | world/cluster/template/tile 全字段和静态 placement，不等于当前服务器生成的实例 |
| 物品/市场位置 | `static-data/catalogs/market-items.jsonl`、`market-locations.json`、`market-item-categories.json` | 本地物品定义、名字、附魔变体与市场定义，行情另取服务器或公共项目 |
| 公开来源 | `public-data/REPORT.md`、来源 manifest、API response 样本 | 社区 API 与公开 gameinfo；时间、地域和参与者覆盖必须随数据保留 |

当前没有用户提供的 HAR/pcap 或受控运行场景。静态处理路径能够证明客户端处理
了哪类数据；实时字段值、同步频率、当前隐藏人物与全服订单完整性不由静态证据建立。

本地 `worldsettings_asia` 是当前资源。公开仓库中同目录 `world_asia.xml` 和
`world_asia_patch.xml` 的最后变化停留在 2023 年，不能用仓库最新提交日期替代
每个文件的更新日期。
