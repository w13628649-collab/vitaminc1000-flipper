# Claude Code 交接：Albion 更新后 Flipper 抓不到市场挂单

交接日期 2026-11-23（新加坡时间）。我是 **Codex（OpenAI 的编码代理）**。项目作者请我通过本机 REA 工作区比对 Windows 上的 Albion 客户端，并通过 SSH 查看这份 Flipper 仓库。交给 Claude Code 的任务是复现故障，确认市场数据消失在哪一层，并根据证据定位原因。

## 背景与当前结论

- 作者在 **Albion 亚服（East）** 使用自己的 Windows Flipper 客户端，被动读取明文市场报文，取得挂单价格、物品和数量。游戏更新后，作者发现 Flipper 抓不到这些数据，想知道是游戏报文变了，还是 Flipper 的抓包或处理出了问题。
- 游戏版本由 Windows 的 `%LOCALAPPDATA%\AlbionOnline\game\version.txt` 确认，从 `1.32.030.345319` 更新到 `1.32.040.348053`。作者提供的是 `launcher\AlbionLauncher.exe`，实际对照了同一安装目录下游戏本体的 `GameAssembly.dll` 和 IL2CPP v39 的 `Albion-Online_Data\il2cpp_data\Metadata\global-metadata.dat`。
- Linux 项目根目录为 `/data/code/albion/albion-flipper/vitaminc1000-flipper`，Codex 阅读源码时的提交为 `8196da3`。当时找到的 `.pcap` 只有已有的进图、移动和切区夹具。
- **根因还没找到。** Codex 做了静态对照和源码阅读。尚未取得新版市场录包，也没有复现故障。现有证据不支持直接修改 `81/82` 或订单字段，也不能判断游戏更新是否改变了实际传输。

## 已确认的静态结果

| 市场操作 | 两版业务编号 | 两版响应声明 |
| --- | ---: | --- |
| `AuctionGetOffers` 卖单 | 81 | `string[]`、`bool` |
| `AuctionGetRequests` 买单 | 82 | `string[]`、`bool` |
| `AuctionGetItemAverageStats` 历史统计 | 95 | `long[]`、`long[]`、`GameTimeStamp[]` |
| `AuctionGetItemAverageValue` 平均价 | 96 | `long` |
| `AuctionGetLowestOfferPrices` 最低卖价 | 97 | `short[]`、`a7m[]` |

81/82 仍走对应的响应解析器，再把订单字符串交给 `JsonReader.Deserialize<AuctionDTO>`。`AuctionDTO` 的 21 个声明字段名和类型一致，包括 `ItemTypeId`、`UnitPriceSilver`、`Amount` 和 `LocationId`。新版的响应解析代码仍读取业务参数 `253`。一些请求内部的混淆字段名变了，声明类型序列一致。这些结果来自安装文件，线上封装和明文是否到达仍要看实测。

新版 `GameAssembly.dll` 的 SHA-256 为 `9724686c97e02cc7281af9ccb23d91b60d42978fc206b5d9ceb9fce7f9c5adb8`，元数据的 SHA-256 为 `e5674b817a03d3ff8b67ab42e80086f47bd51a8b53b902cddee1a702c779de20`。旧版证据见 [REA 元数据报告](../rea-test/investigations/albion-online/metadata/REPORT.md)。新版对照 JSON 和可复用的 `check_market_schema.py` 保存在 Windows 独立 `rea-test` 工作区的 `investigations/albion-online/market-update/` 下，随时可以向作者索取。

## 推荐排查顺序

1. **复现并录一小段。** 在运行游戏的 Windows 机器上给正在使用的 Flipper 加上 `-record`，保留原有启动参数。在亚服进出一次市场，再搜索或刷新一个确认有挂单的商品。记下触发时间、城市、物品和查看的是买单还是卖单。保存这一轮录包及同一时段的客户端日志（默认 `%APPDATA%\AlbionFlipper\client.log`）。如果刚才的操作已经有录包，直接用现有文件。

   ```powershell
   .\flipper-client.exe -record .\albion-captures
   ```

2. **先回放，定位到一条请求和响应。** 把这轮录包放到 Linux，在仓库的 `guild` 目录运行下列命令。多个网卡通常各有一个录包，可挑对应网卡的文件，或把同一轮的文件一并传给 `pcapdump`。有升级前的正常市场录包时，用同一命令对比。默认仅看 `5056`，如实测换了端口，回放时用 `-port` 指定实测值。

   ```bash
   cd /data/code/albion/albion-flipper/vitaminc1000-flipper/guild
   /usr/local/go/bin/go run ./cmd/pcapdump \
     -dump 'op_request:81,op_request:82,op_response:81,op_response:82,op_response:-1' \
     -out /tmp/albion-market.jsonl /path/to/new.pcap
   ```

   `op_response:-1` 很重要。已有 Photon 解析器会把某种响应中位于调试字段位置的 `[]string` 放进 `params[0]`，这种响应没有 `params[253]`。`pcapdump` 因此显示 `-1`，而 Flipper 的业务处理器已经能接住这条路径。看到 `-1` 时，检查参数 0 是否真的有订单 JSON，并核对触发时间和物品。记录 `-1` 本身无法解释这次故障。相关实现见 [Photon 解析器](../guild/internal/photon/parser.go)、[业务处理器](../guild/internal/protocol/protocol.go) 和 [回放工具](../guild/cmd/pcapdump/main.go)。

3. **按最早出现异常的环节继续。**

   | 回放或客户端状态 | 下一步 |
   | --- | --- |
   | 没生成录包，或与游戏操作相符的流量没有进入回放 | 看客户端日志里的网卡、端口和驱动错误。`-record` 在 BPF 端口过滤之后，录包为空时，先核对实际网卡和端口，必要时用 Wireshark 在该网卡上短录再验证。回放的 `Fed` 表示交给解析器的包数。 |
   | 有流量，未看到 `81/82` 请求或匹配的明文响应 | 核对触发时段的双向流量，再看 Photon 解析是否跳过了响应、参数表是否解出。现有 `pcapdump` 不显示密文计数，Flipper 界面的市场状态按请求时间窗口推断。回放没列到响应时，保留传输和解码两种可能。 |
   | 参数 0 有订单 JSON，Flipper 没显示新价格 | 核对 JSON 解码、地点补全和上传。城市订单的 `LocationId` 为空时，程序会用当前区补；若当前区也未知，会暂存。先看顶栏「位置未知」、本地 `/local/status` 的 `held_orders` 和 `orders_uploaded`，再看上传日志和服务端接收。 |

   源码入口依次是 [网卡与端口过滤](../guild/internal/capture/capture_pcap.go)、[TCP/UDP 载荷交接](../guild/internal/capture/feed.go)、[响应和订单解析](../guild/internal/protocol/protocol.go)、[上传](../guild/cmd/client/upload.go)。已有 [抓包文档](packet-capture.md) 和 [项目约定](../CLAUDE.md) 可补充上下文。

排查完成后，请给作者指出具体在哪个环节失败，并附一条能复核的观察，比如请求已看到、响应含订单但地点未知，或调用进入解析器后在某个字段失败。如果暂时拿不到新版录包，请明确缺的是哪段样本。

## 后续交接：黑区附近人物、附魔资源和特殊怪

这部分来自同一天的后续讨论，对应作者的另一个需求。在亚服黑区的当前区域内，判断附近是否有人，是否观察到附魔采集物（矿、木、纤维、兽皮等），以及是否观察到冠军、小 Boss 或 Boss。Codex 对同一版 `1.32.040.348053` 补查了客户端静态处理链。

| 功能 | 已确认的客户端行为 | 还需验证 |
| --- | --- | --- |
| 附近人物 | `NewCharacter=29` 会登记角色对象，`Leave=1` 的普通分支会从本地表移除对象。`Move=3` 已追到坐标字段的写入。 | 新版实际能观测到的范围、移动坐标的完整解码、提醒时间。角色从本地表移除时，可能仍在同一张地图。 |
| 附魔资源 | 创建事件 38、39、40 汇入同一个资源模型。已追到类别、Tier 与配置的关联。事件 46 更新模型中的 `RareState`，游戏用它选择可见外观。余量界面读取的是模型换算后的数值。可剥皮动物另有事件 47 的附魔状态更新。 | 实测附魔、余量及状态变化。动物首次出现的附魔来源、尸体何时能剥皮仍未追完。 |
| 特殊怪 | `NewMob=123` 提供类型索引，客户端用它查本地怪物定义，分类字段来自配置的 `mobtypecategory`。新版 `mobs.bin` 已直接复核。 | 该区域当次实际刷出的实例、存活状态及同步距离。配置中的 Boss 候选点无法回答当前是否已刷出。 |

上表的编号是 Albion 业务事件号。当前证据来自安装文件，尚无新版亚服黑区实测。**没观察到对象时，写「未观测到」**，整张地图是否为空仍未知。

### 以官方边缘姓名牌作为参照

作者在黑区实际看到，别的玩家还没进画面时，游戏会先在屏幕边缘显示他的姓名牌和大致方向，距离有限。Codex 在新版定位了 `CharacterName.OnWorldAnchoredUpdate → ComputeBorder / UpdateAlphaByDistance`，确认姓名牌位置会被限制在屏幕边界内，透明度也会随距离调整。具体生效距离仍未测量。

公开项目 OpenRadar 的 README 描述了附近人物列表和提醒，以及资源、怪物显示。所查版本未在雷达上绘制玩家的实时位置，也没有给出相对于官方姓名牌的提前距离。它可以作为功能参考，当前亚服覆盖仍要单独验证。 [OpenRadar README](https://github.com/Nouuu/Albion-Online-OpenRadar/blob/bda8121cf9bfc1bc54ba9ef80c89945289119ff6/README.md#common-questions)

### 建议的最小验证

1. 固定镜头缩放，让一位协助复现的玩家沿几个方向靠近、再离开。同步录屏和留存本地样本，分别记下**首次识别该角色**、**官方边缘姓名牌首次出现**和**角色模型首次进入画面**的时间。第一项与第二项的差值才回答第三方提醒能否早于游戏自带提示。
2. 分别记录一处已知附魔资源和一只可识别的冠军或 Boss。对照看到的等级、附魔或怪物类别，记录最早观察时间、状态是否更新，以及何时离开观察范围。人员、资源、怪物分别下结论。
3. 报告注明客户端版本、服务器、区域、镜头与每次复现的结果。当前可先验证「有没有」「是否比官方早」。人物精确坐标、最远距离和全图覆盖需要各自的证据。

详细值流、函数地址及原始字节在作者 Windows 的 `rea-test/investigations/albion-online/radar-roadmap/`，主要文件为 `ENTITY-ANALYSIS-1.32.040.348053.md` 和 `entity-evidence-1.32.040.348053.json`。JSON 证据标识为 `AO-MAP-ENTITIES-1.32.040.348053-STATIC`。官方姓名牌的原生证据见报告内「游戏自带的屏幕边缘姓名牌」一节。没有新版录包时，可据此准备验证，市场抓包故障的根因仍需按前文单独排查。
