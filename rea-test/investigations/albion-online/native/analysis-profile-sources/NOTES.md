# Ghidra 分析阶段与恢复依据

来源：本机已安装 Ghidra `12.1.4` 发布包中的 source ZIP；抽取路径与 SHA-256
在 [manifest.json](manifest.json)。本记录不改变任何游戏文件或分析选项。

## Constant propagation 的精确分析器名

实际分析选项名为 **`x86 Constant Reference Analyzer`**。

- [ConstantPropagationAnalyzer.java](ConstantPropagationAnalyzer.java:40) 定义
  `NAME = " Constant Reference Analyzer"`；构造函数第 117 行使用 `processorName + NAME`。
- [X86Analyzer.java](X86Analyzer.java:34) 设置 `PROCESSOR_NAME="x86"`，第 37 行传给父构造函数。
- 本次 Java 线程栈出现 `X86Analyzer.flowConstants → ConstantPropagationAnalyzer.analyzeLocation`，
  因此对应此选项。2026-10-13 18:08 左右已经进入 `OperandReferenceAnalyzer`，说明本次
  已过这个阶段；不能因早期等待就判定其死锁。

如未来确实需禁用该分析器，候选 preset 是
`ghidra-default+disabled:Non-Returning Functions - Discovered,x86 Constant Reference Analyzer`。
这是第三次导入等待期间提出的候选。第三次随后在1200秒总startup期限超时；
第四次由会话持有者应用四项禁用和2400秒预算，实际profile详见 [原生报告](../REPORT.md)。

### 覆盖会减少的内容

该分析器跨多条指令跟踪常量，补建计算地址的引用，包括 x86 `LEA` 数据引用。
父类也准备参数、返回值和存储值的指针引用检查；x86 子类通过自己的 evaluator
执行常量传播。禁用后这些自动引用恢复会缺少，相关 data xref、指针、间接流目标
和后续分析可能变少。基础指令解码与独立 decompiler 仍可工作，但具体函数质量
需按真实结果判断，不能声称与默认 profile 等价。

源码另有 `Max Threads` 描述：“Too many threads causes thrashing in DB.”
默认字段值为 2。本次线程等待部分发生在程序数据库锁，但这既不证明死锁，也不
证明更改线程数必然有效；当前 REA profile 配置只提供禁用分析器列表，未实现
任意子选项修改，不能把未经实施的性能策略当成现有功能。

## 第四次追加禁用项

- **`Decompiler Switch Analysis`**：安装源码
  [DecompilerSwitchAnalyzer.java](DecompilerSwitchAnalyzer.java:46)直接给出name；第48行说明
  `Creates switch statements for dynamic instructions using Decompiler.`。禁用的是全局
  switch补建，不代表独立decompiler不能识别任何switch；具体case/间接目标完整性仍需核实。
- **`Function ID`**：[FidAnalyzer.java](FidAnalyzer.java:36)定义name，第37行说明
  `Finds known functions by hashing.`；它应用已知函数标签及候选/bookmarks。
  禁用后不能指望这些命名帮助，也不能把匿名函数当成无库实现。

第四次2026-10-13 18:11:30左右开始，实际preset为
`ghidra-default+disabled:Non-Returning Functions - Discovered,Decompiler Switch Analysis,x86 Constant Reference Analyzer,Function ID`。
profile digest `96664c5e569b8baa0db21aa3e46d10a6d2eaff5fe5324a1fbaa54ee3a5a61643`。
本记录作者没有创建该次session或修改其命令。

## Address table 阶段的观察

第四次elapsed约496秒位于 `AddressTableAnalyzer.removeDefined`，611秒位于
`findPossibleStrings`，749–937秒位于 `getFunctionEntries` 下的 `PseudoDisassembler`。
这些是同一大阶段中的不同代码路径；线程栈没有局部地址变量，不能据此证明
重复扫描同一个范围或推算剩余时间。

该类的主选项实际叫 **`Create Address Tables`**，
[AddressTableAnalyzer.java](AddressTableAnalyzer.java:99)；源码第103–106行设默认
enablement为false。本次已经运行，随后按第203–205行把尚未消费的范围再次调度
为one-shot。`AutoAnalysisManager.scheduleOneTimeAnalysis`在第226–235行设置子选项
并调度，不检查主enablement；所以事后单纯把该name加入disabled列表，不保证
消除所有已调度的one-shot。未确定最初调度来源，不猜测。

它扫描未定义数据，检查Unicode误判和指针一致性，生成地址表/data引用/bookmarks；
表里全部指向有效代码时，反汇编目标（第266–290行）。源码第279–296行明确不从
该表主动建立函数。REA bridge的 `resolveProcedure`（1836–1842）要求查询地址已经有
`Function`或包含于函数体，不能把尚未识别的裸地址临时造为函数。因此跳过地址表
会减少指针引用和某些代码发现，但无法承诺所选18个函数都仍可直接解码。

当前第四次没有改变此选项。继续等待实际结果；若需要恢复，依据实际调用失败
和完整profile确定策略，不能仅凭阶段名称相同重新导入。

## Startup timeout 后为何暂存后续命令

以下来自隔离 REA HEAD `d13c9f840d36a321a56917e75c48afcb8d72920e` 的只读源码审阅。

- `GhidraClient.ts`：startup 失败重置 `#startPromise` 与 controller（209–228）；
  `#startupFailure` 先 cleanup（463–486），连接/握手 timeout 走该路径（665–678）。
- `callTool` 每次先 `start`（289）；provider 每次 execute 调用 `callTool`。
  AnalysisTimeout 不会把分析 provider 永久设为不可用。
- cleanup 成功后下一条函数调用可以重新触发完整导入。本调查 driver 对 `isError`
  返回继续循环，故不应让未完成函数队列在首次失败后连续重试。
- cleanup 会停止 owned 进程并删除私有 runtime（791–821）；cleanup 本身失败则
  保留 runtime（805–809），后续 start 拒绝已启动状态，需会话持有者显式 close
  处理。不能创建并行重试绕过该状态。

因此会话持有者把18条函数命令保存到 `pending-function-commands.jsonl`，活动队列
保留首次 overview；导入成功后复用同一会话继续。本文作者只监控，没有改命令
队列、创建重试或终止 Java。
