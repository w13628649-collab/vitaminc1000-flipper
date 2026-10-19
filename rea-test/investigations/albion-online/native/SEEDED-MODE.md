# REA 隔离扩展：seeded-noanalysis

2026-10-13，Asia/Singapore。基于隔离源码 HEAD
`d13c9f840d36a321a56917e75c48afcb8d72920e` 的本地未提交改动。
本文件说明实现与使用条件；**没有启动这个模式的REA/Ghidra会话，也没有运行测试**。

## 为什么需要新模式

现有REA公开launcher没有 `-noanalysis` 或选定函数范围参数；即使 `analyze_function`
请求传已知VA，首次仍默认导入整个PE并做全局auto-analysis。Ghidra官方headless支持
`-noanalysis`，仍会执行pre/post脚本。REA已有 `REA_GHIDRA_SEED_FILE` 可以在preScript
按已知entry解码和创建Function，但它原先仍随后执行全局分析。

本扩展只在正式launcher参数中增加一个受约束模式，没有替换正式launcher、
绕过目标SHA/NTFS/DACL/Job Object、放宽Windows数据库修改权限或改动原始游戏文件。

## 调用设置

在会话持有者关闭前一owned会话后，为**新的隔离REA子进程**设置：

```text
REA_GHIDRA_ANALYSIS_MODE=seeded-noanalysis
REA_GHIDRA_SEED_FILE=C:\absolute\chosen-function-seeds.tsv
```

seed格式沿用现成 `rea-ghidra-seeds-v1`：UTF-8，一行
`0xVA<TAB>function[<TAB>caller_name]`；也可有code/label，但必须至少一个function。
设置仅接受Windows、x86_64、native PE、`managed=false`，其它目标或缺失/空function
seed在profile解析/创建client时拒绝。未设置env或显式default保留原来的参数、profile
和handshake行为。无需修改全局npm或Codex注册。

首次只选少量短函数更容易定位失败：例如metadata token selector
`0x18084b3a0`（已保存候选范围45字节），cluster map response
`0x180a9c910`（150字节），人物名称setter `0x180bd9010`（753字节）或确证leaf。
这些字节数来自既有辅助反汇编；不是本模式的限制参数。先读取实际seed report及函数
Evidence，再决定扩大到19项。复杂parser或间接跳转沿流解码仍可耗时。

## 身份与完整性

1. `config/environment.ts`、`parseConfig.ts`、`types.ts`解析新enum。
2. `GhidraProvider.ts`通过现有 `createAnalysisProfile` 将mode、preset
   `ghidra-seeded-noanalysis-v1`、`auto_analysis_performed=false`、
   `global_auto_analysis_complete=false`、seed policy及既有canonical seed SHA/数量
   一起生成新digest。该digest必须从下一次实际bound-session读取，不预造固定值。
3. `GhidraProviderClient.ts`再次核对目标/config/profile一致；复用原seed格式、bounded
   文件读取、realpath及启动前canonical SHA重读检查。每个function/code/label要求成功，
   失败或unmapped拒绝结果；按原流程close清理错误会话。
4. `GhidraLauncher.ts`向正式analyzeHeadless传 `-noanalysis`，descriptor传mode，seed
   preScript传第二参数mode；其它只读及owned生命周期参数保持原有流程。
5. `ReaGhidraApplySeeds.java`仅在新mode显式
   `DisassembleCommand.enableCodeAnalysis(false)`，防止解码提交增量分析；默认分支仍
   使用原 `disassemble(address)`。已有Function在新mode也必须确认entry可解码；
   seed report追加execution_mode。
6. `ReaGhidraBridge.java`核对Windows/TCP、PE/x86/64-bit、headless analysis确实关闭、
   seed report无失败；继续校验导入SHA/provider version。descriptor严格字段、client
   handshake的run/provider/profile/hash/capabilities/只读/语言校验全部保留。
7. 新mode handshake为 `analysis_complete=false`、`analysis_timed_out=false`、
   `selected_mode_complete=true`、`auto_analysis_performed=false`、
   `global_auto_analysis_complete=false`。selected完成只表示seed准备通过；默认完整分析
   布尔没有被伪造为true。`GhidraClientTypes.ts`、`GhidraClient.ts`及
   `GhidraSessionValues.ts`核对预期mode和这组状态，default继续原互斥完整性规则。

## 函数分析及限制

bridge的 `resolveProcedure` 要求FunctionManager中实际已有Function，VA请求不会自动
创建函数。seed创建成功后才可沿既有REA `analyze_function` / pseudocode流程调用独立
`DecompInterface.decompileFunction`。函数结果仍需实际成功Evidence才能写为成功。

该模式**导入完整不变PE，从seed entry沿流解码**，没有仅导入某个VA的loader，也没有
显式字节range。函数body由Ghidra流与既有Function边界推出；相邻.pdata fragments、
folded IL2CPP方法、尾调用、NoReturn和间接switch仍需原字节交叉核对。seed名称为
调用者断言，不是原生符号或Ghidra自动发现。

全局callers/xrefs、库符号、字符串、Address Table、switch和常量传播均未完成；结果
覆盖的是这个有限分析数据库。完整body/reference collection表示该Function/数据库
中采集完整，不表示整PE或真实业务函数边界完整。provider每个结果附明确mode和覆盖
limitations。公开overview若与目标同hash且来源明确可独立保留，但不能冒充本模式
函数结果或补齐其全局覆盖。

## 构建与可审计产物

允许的 `npm.cmd run compile`（TS+runtime bundle）已经退出0，仅原有bundle动态import
提示；扩展作者没有执行测试、doctor或新目标会话。npm compile不编译Java。

**第四次默认模式后来使用了兼容Java，但没有启用seed模式。** 调查持有者的Node进程在
扩展前已经载入旧bundle；Ghidra进入postScript时在2026-10-13 18:43:19（UTC+08）编译
当时磁盘Java，实际Bridge包含seeded-noanalysis常量，随后执行default分支并返回24条成功
Evidence。该次没有设置mode或传`-noanalysis`；既不能称Java从未运行，也不能把其默认
分支成功当作seed模式已验证。实际class、日志与版本解释见
[attempt-profile-four/manifest.json](attempt-profile-four/manifest.json)。最终新增的已有Function
入口解码guard晚于该次class编译；current-final-source是最终磁盘快照，不能证明与该次
class编译输入完全相同。新mode及最终Java完整源码的编译/行为仍未验证。

Ghidra12.1.4发布jar的静态 `javap` 声明确认DisassembleCommand构造函数、
enableCodeAnalysis/applyTo、HeadlessScript.isHeadlessAnalysisEnabled、Program及Language
相关getter均存在；输出见 [seeded-mode-java-api.txt](seeded-mode-java-api.txt)。这是API
静态核对，这个javap步骤没有编译/执行目标脚本；第四次default Java实际使用边界见上文。

完整源码diff在 [seeded-mode.patch](seeded-mode.patch)，文件与运行入口SHA、基础修订、
构建结果在 [seeded-mode-provenance.json](seeded-mode-provenance.json)。dist为生成产物且
未跟踪；使用修改后的副本必须记录这份patch及实际bound profile，不能仅记录git HEAD
或package自报6.4.0。现有Windows addon没有重新编译，出处及能力限制仍见
[环境记录](../../../REA-ENVIRONMENT.md)。本扩展作者没有改活动driver、seed或命令队列。
