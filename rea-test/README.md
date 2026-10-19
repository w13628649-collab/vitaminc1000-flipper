# REA 调查工作区

工作目录：`C:/Users/ludy/Documents/code/rea-test`。
记录日期：2026-10-13，Asia/Singapore。沟通与报告使用中文。

## 入口

- [AGENTS.md](AGENTS.md)：未来代理的工作约定和当前任务入口。
- [REA-ENVIRONMENT.md](REA-ENVIRONMENT.md)：安装、注册、工具目录、Windows 使用方法及实际版本边界。
- [Albion Online 亚洲服报告](investigations/albion-online/REPORT.md)：地图、市场、地图人物的获取方法、现有产物和覆盖限制。
- [证据索引](investigations/albion-online/EVIDENCE-INDEX.md)：原始结果、hash、公开来源和协议字段。

## 使用现成目录

在此目录的 PowerShell 中运行：

```powershell
python .\investigations\albion-online\query_catalog.py cluster '0217'
python .\investigations\albion-online\query_catalog.py item 'T4_BAG' --limit 5
python .\investigations\albion-online\query_catalog.py markets
```

公开查询工具与命令在主报告第二节。大型 JSONL 按行读取；优先复用现有解码和索引。
