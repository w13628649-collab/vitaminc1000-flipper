# Albion Online 调查范围

用户指定安装目录：`C:/Users/ludy/AppData/Local/AlbionOnline`。
服务器：**Asia / East**。2026-10-13，Asia/Singapore。

需要出具完整报告说明获取方式：

1. 地图上的数据：区域定义、连接、地形/模板、标记、资源定义和实时状态的区别。
2. 市场数据：物品、市场位置、报价、订单深度、历史和公开覆盖范围。
3. 地图人物信息：身份、名称、公会/联盟、装备、位置/移动/状态，以及可见范围限制。

证据路线：本地静态 GameData 解码、IL2CPP v39 元数据解析、REA/Ghidra 对选定函数
的分析、可公开读取的数据项目/API 文档与少量公开接口请求。

本地版本：`1.32.030.345319`；Unity `6000.3.12f1`；IL2CPP 元数据 v39。
主原生目标：`game/GameAssembly.dll`。
SHA-256：`19a684ad23103f0b9a12b005c0d31609a55e3624699dc1b0e6fd509166c77202`。
元数据：`game/Albion-Online_Data/il2cpp_data/Metadata/global-metadata.dat`。
SHA-256：`b0cffb9a6945366c0057390c2144edbfb7b9fb0b9a2f327ed8517e3051b7684b`。

输出目录：`static-data`（原始资源与结构化目录）、`metadata`（元数据与方法映射）、
`native`（REA Evidence）、`public-data`（公开文档和样本）。最终报告在此目录。
任务请求没有提供 HAR/pcap、实时客户端窗口或主动操作游戏步骤。

进度：环境记录、本地 3278 个 GameData 解码、地图/物品/Unity 资源目录、v39
元数据和 93 个 CodeGenModule 方法映射、选定人物/拍卖/地图消息的静态参数键及
公开市场/人物接口样例均已保存。主报告为 `REPORT.md`，四个专题已完成本次调查与报告，
并逐项保留获取范围和未确认条件。
REA/Ghidra 第四次导入成功，23 个选定函数成功返回，已导出24条Evidence并关闭调查
持有的会话、清理进程及临时目录；实际分析profile和运行代码来源以 `native/REPORT.md`
及调用记录为准。目录内seeded-noanalysis扩展未在本次使用。
新增查询助手未运行测试；没有运行游戏或取得实时人物样本。
公开仓库的 world_asia.xml/patch 为 2023 年历史文件，不能套入当前客户端作为实际亚服世界。
