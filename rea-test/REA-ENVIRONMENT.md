# REA 本机环境与使用手册

记录日期：2026-10-13，时区 Asia/Singapore（UTC+08:00）。
工作目录：`C:/Users/ludy/Documents/code/rea-test`。
目的：给未来的 Codex 直接恢复工作所需的事实、入口和限制。

## 先看这几条

1. 已安装 npm 稳定版 **rea-agents 6.4.0**，匹配的 REA skill **v37**。
2. 已注册 Codex MCP。独立 stdio 启动、初始化、`tools/list` 和
   `binary_session({})` 成功，真实服务版本 6.4.0，工具 **170** 个。
3. 记录时当前 Codex turn 没有暴露 REA MCP 工具。已注册和独立启动成功不能证明
   当前会话连接成功；重新加载客户端后检查实际工具。未连接期间可用现有 CLI。
4. Ghidra 12.1.4/JDK 21 检查通过。环境准备阶段没有分析任何原生目标，不能把
   doctor 成功当作完成了实际反编译。
5. PowerShell 的 `.ps1` 被执行策略 Restricted 拦截。调用 **`rea.cmd`** 和
   **`npm.cmd`** 即可；Codex MCP 用 Node 直接启动，不受这个 shim 问题影响。
6. Albion 本次原生调查使用独立的源码副本，见下文“Albion 调查的实际版本边界”。
   副本的初始化同样显示 6.4.0，实际代码修订及工具目录与 npm 发布版不同。
   全局安装和 Codex 注册没有随本次调查改变；第四次取得24条成功Evidence，其中23条为函数，
   bundle已导出，会话成功关闭，调查持有的进程及临时目录已清理。

## 路径和版本

| 项目 | 已观察事实 |
| --- | --- |
| 操作系统 | Windows 11 专业版，10.0.26200，x64 |
| 默认终端 | Windows PowerShell，执行策略 Restricted |
| Node | `C:/Program Files/nodejs/node.exe`，22.22.0 |
| npm | `C:/Program Files/nodejs/npm.cmd`，10.9.4 |
| 全局 npm prefix | `C:/Users/ludy/AppData/Roaming/npm` |
| REA CLI | `C:/Users/ludy/AppData/Roaming/npm/rea.cmd` |
| REA 包目录 | `C:/Users/ludy/AppData/Roaming/npm/node_modules/rea-agents` |
| 公共入口脚本 | `C:/Users/ludy/AppData/Roaming/npm/node_modules/rea-agents/scripts/rea.mjs` |
| Codex 配置 | `C:/Users/ludy/.codex/config.toml`；记录时未设置 CODEX_HOME |
| 原有配置备份 | `C:/Users/ludy/.codex/config.toml.rea.backup`，时间 2026-06-15；这是旧快照，不能当作本次安装前的完整当前配置 |
| REA skill | `C:/Users/ludy/.agents/skills/reverse-engineer-anything/SKILL.md`，v37，170 tools |
| Ghidra | `C:/tools/ghidra_12.1.4_PUBLIC`，12.1.4 |
| Ghidra headless | `C:/tools/ghidra_12.1.4_PUBLIC/support/analyzeHeadless.bat` |
| 原生反编译器 | `C:/tools/ghidra_12.1.4_PUBLIC/Ghidra/Features/Decompiler/os/win_x86_64/decompile.exe` |
| JDK/JAVA_HOME | `C:/Program Files/Eclipse Adoptium/jdk-21.0.12.101-hotspot` |
| Java / javac | Temurin 21.0.12.1，64 位完整 JDK |
| Python（处理 JSON 等） | `C:/Users/ludy/AppData/Local/Programs/Python/Python314/python.exe`，3.14；不是 REA 前提 |
| 临时目录 | `C:/Users/ludy/AppData/Local/Temp` |

REA 6.4.0 支持 Node `^22.19.0 || ^24.11.0 || >=26.0.0`。
升级时同时核对注册、包版本、skill 版本和活动服务器，不能只更新使用说明。

## Codex 注册（只保留 REA 部分）

```toml
[mcp_servers.rea]
command = "C:\\Program Files\\nodejs\\node.exe"
args = ["C:\\Users\\ludy\\AppData\\Roaming\\npm\\node_modules\\rea-agents\\scripts\\rea.mjs", "mcp"]
startup_timeout_sec = 30

[mcp_servers.rea.env]
GHIDRA_INSTALL_DIR = "C:\\tools\\ghidra_12.1.4_PUBLIC"
JAVA_HOME = "C:\\Program Files\\Eclipse Adoptium\\jdk-21.0.12.101-hotspot"
```

上面是阅读用快照；修复时读取实际配置，不覆盖其他设置。
原生 CLI 单独运行时需要继承相同环境：

```powershell
$env:GHIDRA_INSTALL_DIR = 'C:\tools\ghidra_12.1.4_PUBLIC'
$env:JAVA_HOME = 'C:\Program Files\Eclipse Adoptium\jdk-21.0.12.101-hotspot'
& 'C:\Users\ludy\AppData\Roaming\npm\rea.cmd' --version
& 'C:\Users\ludy\AppData\Roaming\npm\rea.cmd' --help
```

优先公共 launcher `rea.cmd` 或 `scripts/rea.mjs`，不要依赖内部 `dist/cli.js`。

## 实际工具与能力范围

- [工具索引](.rea/TOOLS.md)：170 个工具名称和实际描述。
- [精确 MCP schema](.rea/mcp-tools.json)：真实 `tools/list` 返回的输入 schema。
- [副作用和 catalog identity](.rea/tool-effects.json)：CLI/MCP/prompts 数量、digest 和工具 effects。
- [CLI 帮助](.rea/cli-help.txt)：实际安装版的命令名称和全局参数。
- [环境快照](.rea/environment.json)：限定后的 REA 配置、provider 检查、版本和连接结果。
- [空会话基线](.rea/session-baseline.json)：未选择目标的实际 `binary_session` 返回。

工具目录包括：原生函数、汇编、伪代码、调用图和引用；JavaScript/Electron 静态分析；
托管 PE/CLI 元数据和 CIL；包和 PE 资源；浏览器/Electron/Node 运行时；Android/JADX/ADB；
JEB；固件；Evidence、重建图和对比。**列出工具不代表所需引擎已安装或本机支持该操作。**

已检查可用的原生 provider 是 **Ghidra**。Hopper 缺失；IDA 未配置。
JADX、ADB、Apktool、JEB、Binwalk、Unblob、pwntools 等没有在本任务安装或验证。
静态 JavaScript 和托管元数据不需要 Ghidra；需要用真实目标结果确认具体操作。

### Windows Ghidra 范围

6.4.0 的 Windows Ghidra 为实验性 P0，只读原生 x86/x64 PE EXE/DLL；
不能把 Linux/macOS 的 ELF、Mach-O、DOS 或所有修改操作当作本机已支持。
输入和临时路径需要固定本地 NTFS，UNC、reparse、alternate stream 等路径受限制。
REA 使用自己的临时项目、Job Object 和清理流程，分析不运行被分析的程序。

`open_binary` 只绑定目标；首次 Ghidra-backed 查询才开始 import/auto-analysis。
默认原生启动期限 **330000 ms**，与 Codex MCP 的 **30 秒初始化期限**、单次 tool
调用期限分别计算。大文件首次查询可能很久，避免客户端超时后重复创建会话。
如实际需要提高期限，检查并设置 `REA_GHIDRA_STARTUP_TIMEOUT_MS`；
支持范围 1..2147483647，修改后对新启动的 provider 生效。

### Albion 调查的实际版本边界（2026-10-13 补记）

本节是指定目标 `C:/Users/ludy/AppData/Local/AlbionOnline/game/GameAssembly.dll`
的实际调查记录，不替换上述 npm 6.4.0 环境基线。目标 SHA-256 为
`19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202`。

| 层级 | 本次事实 |
| --- | --- |
| 全局 npm / Codex 注册 | 仍为既有 npm `rea-agents@6.4.0` 和全局 `scripts/rea.mjs`；本次未修改 |
| 隔离源码副本 | [native/vendor/rea](investigations/albion-online/native/vendor/rea)，git HEAD `d13c9f840d36a321a56917e75c48afcb8d72920e`；早先读取时干净，随后增加本地seed模式扩展，见下文实际运行边界 |
| 实际调查入口 | [rea_session.py](investigations/albion-online/native/rea_session.py) 读取当前 Codex REA 注册的 Node 路径/环境，在 Python 进程内把 args 指向隔离副本的 `scripts/rea.mjs mcp`；不写回 config.toml |
| 独立服务身份 | [bound-session.json](investigations/albion-online/native/evidence/bound-session.json) 确认 root_path/command_path 为隔离副本；package/server version 均仍报6.4.0，build_commit=null |
| 副本工具目录 | 实际会话为191 MCP工具、142 CLI命令、6 prompts，期望skill41；这是副本身份，不能替换全局170工具/127命令/skill37基线 |
| 第四次导入前设置 | 仅调查子进程设置 `REA_GHIDRA_STARTUP_TIMEOUT_MS=2400000`、`GHIDRA_HEADLESS_MAXMEM=4G`；driver单次请求期限2500秒；四项禁用见下文 |
| 原生addon来源 | Windows x64 `.node` 从已安装npm6.4.0产物复用；来源记录将npm修订记为 `a56f7bb668ab23fec8d33f7af353ac7ed2fe1895` |
| addon ABI范围 | manifest为ABI1/Node-API8、win32/x64、packageVersion6.4.0；SHA-256 `c4fd9f377e567bcf15ba3fec3e0b0ed965d82b765a2adcdac49e46f14524b6d5`，与实际文件一致 |
| 当前结果 | 第四次概览1914.01秒成功，随后23条函数查询成功；24条真实ev_*已导出bundle，owned close成功，实时游戏状态仍未观察 |

**npm 发布版6.4.0没有实现 `REA_GHIDRA_DISABLED_ANALYZERS`。** 对全局版本仅设置
这个环境变量不会取得副本的行为。隔离修订新增配置解析、分析profile记录和
`ReaGhidraAnalysisOptions.java` preScript，在默认auto-analysis前禁用指定分析器；
不是全面关闭分析，也不是允许使用尚未完成的部分结果。

默认分析曾重复停在 `FindNoReturnFunctionsAnalyzer.repairDamagedLocations` /
`ClearFlowAndRepairCmd`，该尝试已通过停止调查持有的driver取消。范围和下一profile见
[attempt-default/diagnostic.json](investigations/albion-online/native/attempt-default/diagnostic.json)；
保存的Ghidra日志和线程栈在同目录。

第三次导入只禁用 `Non-Returning Functions - Discovered`，startup期限1200000ms。
首次 `binary_overview` 实际运行 **1200.74秒**，返回 `provider_timeout`，底层
operation为 `list_documents`，未产生成功函数Evidence。完整原始错误、diagnostics、
日志、线程栈及绑定保存在
[attempt-profile-one](investigations/albion-online/native/attempt-profile-one)；diagnostic记录
`failure_kind=timeout`、`failure_message=Ghidra startup deadline elapsed`，profile digest为
`e735fcd6ab90f3b111c6d760b0d78c8bc569abdcd8caaff0468855d8ebfd2fa1`。
会话持有者随后成功 `close_binary`；其exec session69825退出0。该次关闭是早先记录的事实，
当前evidence/close-target.json已经由第四次最终关闭结果替换。此session号和退出码属于driver执行记录，
不表示Ghidra diagnostic中仍为null的进程退出码。

第四次仍由原会话持有者启动，2026-10-13 18:11:28（UTC+08）绑定记录确认profile为：

```text
ghidra-default+disabled:Non-Returning Functions - Discovered,Decompiler Switch Analysis,x86 Constant Reference Analyzer,Function ID
```

Ghidra provider profile digest为
`96664c5e569b8baa0db21aa3e46d10a6d2eaff5fe5324a1fbaa54ee3a5a61643`。
启动期限增至2400000ms，driver请求期限2500秒，Java堆预算4G；实际配置见
[rea_session.py](investigations/albion-online/native/rea_session.py)和当前
[bound-session.json](investigations/albion-online/native/evidence/bound-session.json)。第四次概览在
**1914.01秒**后成功，Evidence为
`ev_fc4c58853615b3102f902b6783f28b6e6a5698d11cfba0399335cb5f805a0e86`；随后23条
函数查询成功。概览Evidence自身是REA composed workflow，digest为
`86bb5b15c4ca832d9ef4524828c5a11792857659dacad123858bc5c35a521aa4`，其
`parameters.upstream_analysis_profile.digest`才是上述Ghidra digest；23条函数Evidence直接使用
上述Ghidra digest。结果合计24条，包括人物创建/名称/装备/生命、小地图、地图响应、
拍卖解析、所选事件/Join响应handler和参数排序；完整成功ID与实际函数结果见bundle和原生专题。
历史文件名`photon-event-router`与`photon-operation-response-router`分别保存Died164和Join2
的所选handler，文件名不证明通用router实现。

最终[export-bundle.json](investigations/albion-online/native/evidence/export-bundle.json)原始响应为
24 records、28,998,233 bytes、unknowns=0；实际bundle含24条records，SHA-256为
`e26c53944189137a041a7239d98dedf3535a9445d0320a9a9a9d58ab5ad30a5c`。
[close-target.json](investigations/albion-online/native/evidence/close-target.json)返回`result:null`且无
isError；calls记录关闭0.46秒。driver exec session8446退出0；关闭后只读检查确认持有的
Java52304/wrapper48672不存在、临时runtime目录不存在。独立生命周期记录见
[lifecycle-final.json](investigations/albion-online/native/lifecycle-final.json)。unknowns=0是bundle
注册表计数，不表示报告没有语义或运行时未知项。

这些选项改变分析覆盖：禁用发现式Non-Returning函数修复；减弱全局decompiler
switch/jump-table补建；缺少x86常量传播带来的计算地址、指针和自动xref恢复；
缺少Function ID的已知库函数哈希匹配及符号命名帮助。REA仍会对请求的具体函数独立
执行反编译，但函数结果及case/xref完整性需逐项读取真实Evidence，不能宣称与默认profile
等价。安装版Ghidra源码依据见
[analysis-profile-sources/NOTES.md](investigations/albion-online/native/analysis-profile-sources/NOTES.md)。

**第四次实际Node/Java来源须分开记录。** Node服务在第四次启动时载入隔离HEAD的
既有编译bundle，profile仍为默认分析加四项禁用。等待期间，本地增加了
`REA_GHIDRA_ANALYSIS_MODE=seeded-noanalysis` 的可选扩展；Ghidra在进入postScript时
于2026-10-13 18:43:19重新编译了当时磁盘Java，因此第四次实际Java包含这个兼容扩展，
执行其默认分支。没有设置seed模式、没有传 `-noanalysis`，不能把第四次成功归给seed模式，
也不能称实际Java是未修改HEAD。runtime class、日志及分开的HEAD/最终Java源码保存于
[attempt-profile-four](investigations/albion-online/native/attempt-profile-four)。实际
`ReaGhidraBridge.class` SHA为 `f06399352cecd4b77307437542b353dcdae4474f9962edf03ecd6b5defe73c35`。
原descriptor受immutable lease拒读，未绕过；白名单摘要不保存auth token。

磁盘扩展及TS/bundle编译结果见
[SEEDED-MODE.md](investigations/albion-online/native/SEEDED-MODE.md)、
[seeded-mode.patch](investigations/albion-online/native/seeded-mode.patch)和
[seeded-mode-provenance.json](investigations/albion-online/native/seeded-mode-provenance.json)。
最终patch SHA为 `2200c67fe93eb5b9479bdfabab515145e99a518fcba71ecb55323fe7f3a73b63`。
它限定win32/x64/native PE及非空function seed，明确全局auto-analysis未完成；当前未启用。
第五次seed模式没有执行，其行为仍未得到会话验证。不要把当前磁盘dist哈希当作第四次
已载入Node的哈希；新的启动将载入磁盘扩展并需要记录新的profile身份。

该隔离修订在startup超时且owned资源清理成功后，下一条查询可以重新触发完整导入；
driver收到tool错误仍继续读队列。会话持有者已把18条函数命令暂存到
[pending-function-commands.jsonl](investigations/albion-online/native/pending-function-commands.jsonl)，
首次概览成功后，会话持有者已恢复函数查询并取得上述23条结果。若cleanup失败，应由持有者
显式close清理；不要创建平行会话绕过资源状态。

原生addon的出处及兼容策略见
[artifact-provenance.json](investigations/albion-online/native/vendor/rea/native/windows/build/artifact-provenance.json)
和[manifest.json](investigations/albion-online/native/vendor/rea/native/windows/build/manifest.json)。
记录明确 `native_sources_identical=false`：不能因为ABI1/Node-API8未变就声称addon
来自隔离修订或两版原生源码相同。隔离修订的loader仍校验manifest、SHA、PE架构、
addon自检和本地NTFS/DACL/Job Object能力；可选protocolStdin/jobMemoryLimit只有在
addon实际声明时才能使用，不绕过provider的能力检查。本任务仍限定Windows原生
x86/x64 PE的只读Ghidra范围，不推及所有新目录功能。

过程与后续真实结果入口：
[原生调查报告](investigations/albion-online/native/REPORT.md)、
[calls.jsonl](investigations/albion-online/native/calls.jsonl)、
[commands.jsonl](investigations/albion-online/native/commands.jsonl)、
[evidence目录](investigations/albion-online/native/evidence)、
[evidence-bundle.json](investigations/albion-online/native/evidence-bundle.json)。
命令入队、open_binary成功或已有bundle文件都不能替代成功函数Evidence。当前调查driver
已结束；后续优先读bundle、
[SUMMARY.json](investigations/albion-online/native/results/SUMMARY.json)和报告。
现有commands.jsonl包含历史概览、23条函数查询和finish；重跑rea_session.py会新导入并重做
这些查询，不是恢复已结束会话。确需新查询时准备独立新队列和新输出目录，记录新会话与profile。

## 按目标选入口

| 目标 | MCP 入口/后续 |
| --- | --- |
| 原生 EXE/DLL | `open_binary`，`provider_id: "ghidra"` → `binary_overview` / `analyze_function` / 函数、字符串和引用查询 |
| ASAR / JavaScript 目录 | `analyze_javascript_application`，大或未知规模用 `detail: "summary"`，之后 `inspect_analysis_view` |
| ZIP/APK/IPA/MSIX 等包清单 | `open_binary` → `inspect_artifact`，代码分析另用目标专属入口 |
| .NET PE/CLI | `inspect_managed_artifact` → `inspect_managed_members` / `inspect_managed_native_boundaries`；静态 CIL 不等于恢复 C# 源码 |
| PE 图标及资源树 | `inspect_pe_resources`，明确路径，不需要活动原生会话 |
| 已打开的网站 | `list_browser_targets`，按返回 target 和 origin 选择，需可用 CDP endpoint |
| 已打开的 Electron | `list_electron_targets`，需所选程序的 CDP endpoint |
| 保留的 HAR | `inspect_web_network_capture`，离线读取，不访问记录里的 URL |

具体字段先查活动 schema；本表不是可复制的完整参数对象。
若工具不可用，查询 `binary_session({}).result.tool_availability` 和 remediation。
用户没有选目标时不擅自选择附近 EXE 或报告中旧目标。

## CLI 常用写法

以下输入路径是占位符，运行时换成用户指定的真实路径；输出放入任务目录。
分析类命令采用帮助确认的 `--format json`。`doctor/setup` 支持 `--json`。

```powershell
# JavaScript / Electron（目录也可用 --artifact-format directory）
rea.cmd analyze-javascript-application 'C:\path\app.asar' --artifact-format asar --format json

# .NET 静态元数据、成员和 CIL 边界
rea.cmd inspect-managed-artifact 'C:\path\assembly.dll' --format json
rea.cmd inspect-managed-members 'C:\path\assembly.dll' --format json
rea.cmd inspect-managed-native-boundaries 'C:\path\assembly.dll' --format json

# 原生；先设置上面的 GHIDRA_INSTALL_DIR / JAVA_HOME
rea.cmd inspect 'C:\path\program.exe' --provider ghidra --format json
rea.cmd function 'C:\path\program.exe' '0x140001000' --provider ghidra --format json
rea.cmd decompile 'C:\path\program.exe' '0x140001000' --provider ghidra --format json
```

CLI 调用会有独立生命周期。需要反复查询大型原生文件时用一个持久 MCP 会话，
避免每条 CLI 命令重新 import/auto-analysis。

### Evidence 保存和读取

MCP 保存整个会话用 `export_evidence_bundle`；结束时 `close_binary`。
CLI 分析通常返回**单条 Evidence**。`evidence-export` 和 `evidence-import` 接受
canonical **bundle**，不能把单条分析 JSON 当作 bundle 传入。

使用 Python subprocess 将完整 stdout 直接写入 UTF-8 文件，可避免 Windows
PowerShell 重定向生成 UTF-16 或客户端输出截断。不要把大结果先输出到上下文。
`inspect-analysis-view` 的输入是包装 JSON，例如：

```json
{
  "source": {"kind": "inline", "evidence": "这里应放完整 Evidence 对象，不是文件名"},
  "view": {"kind": "summary"}
}
```

把 `evidence` 替换为解析后的完整对象，写到 `view-input.json`，再执行
`rea.cmd inspect-analysis-view view-input.json --format json`。
JS modules 分页：`view: {kind: "page", collection: "modules", offset: 0, limit: 32}`。
原生函数伪代码分页：`view: {kind: "native", facet: "pseudocode", offset: 0, limit: 64}`。
bundle 读取使用 `inspect-evidence-bundle`，包装对象
`{"path":"C:\\absolute\\bundle.json","detail":"summary"}`。

## 只在出现问题时诊断

| 症状 | 行动 |
| --- | --- |
| `rea` 报脚本执行策略错误 | 改用 `rea.cmd`，或公共 launcher 的绝对路径 |
| 当前会话无 REA 工具 | 检查注册；注册一致时重新加载/连接客户端，不循环 setup；可继续授权的 CLI 调查 |
| `provider_unavailable` | 读对应 provider 的 availability/diagnostics，仅解决所需前提 |
| MCP approval 拒绝 | 是客户端策略，不等同引擎问题；不修改审批/沙箱来掩盖它 |
| 输出截断或 transport resource_constraint | 使用 retained Evidence ID 的 summary/page/item 或导出，不重新运行完整分析 |
| 第一次原生查询慢 | Ghidra 正在惰性启动和 auto-analysis，确认已有生命周期与超时后再决定恢复 |

```powershell
rea.cmd doctor --client codex --provider ghidra --skill --json
rea.cmd setup --client codex --dry-run --json
# 只有修复范围已获授权且已展示计划时再 apply：
rea.cmd setup --client codex --yes --json
```

### 更新本记录

`python .rea/refresh_environment.py` 会执行 scoped doctor，启动一个空目标 stdio 服务，
获取初始化、工具 schema 和 `binary_session`，然后刷新 `.rea` 中的快照和索引。
它不分析目标，不安装软件，不修改 Codex 配置，不启动 Ghidra。
脚本记录的 `active_agent_tools_exposed` 仍需未来 agent 按自己的实际工具更新。
该刷新脚本针对全局注册基线；Albion隔离副本的补记位于环境JSON的
`albion_native_investigation`，重做基线刷新时需另行保留或按实际调查证据更新该节。

官方来源：[仓库](https://github.com/morluto/rea)、
[安装指南](https://github.com/morluto/rea/blob/main/docs/installation.md)、
[CLI](https://github.com/morluto/rea/blob/main/docs/cli.md)、
[Windows Ghidra 范围](https://github.com/morluto/rea/blob/main/docs/windows-ghidra-p0.md)。
仓库 main 可能比 npm 6.4.0 新，实际安装版帮助和 schema 优先。
