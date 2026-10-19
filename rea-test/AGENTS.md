# 本目录的 REA 工作约定

本目录用于用户指定程序的逆向调查和报告。开始 REA 工作时先读
[REA-ENVIRONMENT.md](REA-ENVIRONMENT.md)，按任务查阅已安装的
`C:/Users/ludy/.agents/skills/reverse-engineer-anything/SKILL.md` 和相关 references。

- 使用中文沟通和写报告。
- 环境事实、路径和恢复命令见 `REA-ENVIRONMENT.md`；完整 MCP 参数见
  `.rea/mcp-tools.json`；工具索引见 `.rea/TOOLS.md`；副作用见 `.rea/tool-effects.json`。
- 这些文件是带日期的快照。活动会话实际暴露的工具及其 schema 优先。
  已连接且正常时直接分析目标，不要每次运行 doctor、setup 或重新安装。
- Windows PowerShell 执行策略为 Restricted，命令使用 `rea.cmd`、`npm.cmd`。
  PATH 缺失时使用文档中的绝对路径。不要为此修改系统执行策略。
- 原生 Windows 分析使用已配置的 Ghidra；CLI 显式传 `--provider ghidra`，
  MCP `open_binary` 显式传 `provider_id: "ghidra"`。
- 调查前把用户问题、确切目标路径和版本记入任务目录。保存完整原始 Evidence，
  报告记录 Evidence ID、文件 SHA-256、来源和覆盖范围。区分观察、推断和未知。
- 调查产物保存在本目录下 `investigations/<目标>/`；不要覆盖输入程序。
  关闭自己打开的 REA 会话，复用已有结果和索引。
- 可以并行委派独立的公开资料研究、资源盘点和专题分析；不同工作者使用独立
  输出目录和会话，不同时改动同一活动原生目标。

当前用户目标（2026-10-13）：完成环境记录后分析
`C:/Users/ludy/AppData/Local/AlbionOnline`，出具完整报告，说明如何获取地图上的
所有数据、市场数据及地图人物信息。服务器为 Asia / East。调查过程中按静态
客户端数据、服务器实时数据、公开数据源确认实际可取得的字段和缺口；
当前未提供实时捕获或主动操作游戏步骤。任务进度与范围见
`investigations/albion-online/SCOPE.md`。

现有成果入口：`investigations/albion-online/REPORT.md`，专题目录为
`static-data`、`metadata`、`public-data`、`native`。静态产物较大，先读专题报告和
manifest，按行筛选 JSONL，不重复提取。当前 v39 参数键来自静态排序证据，
native 对象 offset 不等于网络包 offset；公开人物档案没有实时坐标。
本次 Ghidra 调查使用目录内隔离 REA 源码修订，具体发布版边界和启动参数见环境手册。

本次报告已完成：REA overview 与23个选定函数成功，24条完整Evidence已导出，持有会话、
进程及临时目录已清理。先读报告和 `native/results/SUMMARY.json` 复用结果。
`native/rea_session.py` 与 `commands.jsonl` 是本次历史driver和队列，直接重跑会重新导入
并执行旧查询；需要新证据时准备独立的新队列与输出。目录内seeded-noanalysis扩展未使用，
本次默认加四项禁用分析及延迟Java编译的实际来源见 `native/attempt-profile-four/`。
