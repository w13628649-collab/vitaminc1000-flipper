# 进度

最后更新:2026-09-22

## 当前状态

**全 Go**,Python 已全部删除。抓包 → 服务端 → PostgreSQL → 客户端窗口
这条链路端到端通了。交易模型比第一版的 Python demo 宽得多:执行方式、跨城套利、
风险指标、吃深度、资金分配、实盘记账——详见 [flipper.md](flipper.md)。
137 个测试。

**成员只开两个窗口:游戏,和我们的程序。没有浏览器。**

```
客户端 exe(一个窗口)                     服务端(纯后端)
┌──────────────────────────────┐        ┌──────────────────────┐
│ WebView2 窗口                 │        │ 收数据 / 去重 / 入库   │
│   ↓ http://127.0.0.1:随机端口  │        │ 扫描·套利·组合运算     │
│ 内置本地 HTTP                  │        │ PostgreSQL/Timescale │
│   /        → 编进 exe 的界面   │        └──────────┬───────────┘
│   /api/*   → 反向代理 ─────────┼───────────────────┤
│   /ws      → 反向代理 ─────────┼───────────────────┘
│   /local/* → 抓包状态(本地)   │
│ 抓包线程(后台)                │
└──────────────────────────────┘
```

为什么要在客户端里套一层反向代理,而不是让前端直接打服务端地址:

- **前端代码一个字不用改。** 同源,没有 CORS,WebSocket 也不用换 host
- **CDK 令牌在这一层注入。** 成员永远不需要知道 token 长什么样
- **服务端挂了窗口照样开得起来**,能给出一句人话的错误而不是一片白屏

界面资源(HTML/CSS/JS + 两个可变字体)编在客户端二进制里,
不依赖任何外部 CDN——公司网络挡掉 Google Fonts 也不影响。
服务端也留了一份同样的界面,那是给开发和排查用的。

## 已经能跑的

| 部分 | 状态 | 位置 |
|---|---|---|
| Photon 协议解析 | 从 ADC 迁移,有测试 | `guild/internal/photon/` |
| 抓包(Npcap) | `-tags pcap` 编译,多网卡并行 | `guild/internal/capture/` |
| 市场挂单解析 | 挂单/求购,含 `Amount` | `guild/internal/protocol/` |
| 去重写入 | LRU 去重,实测省掉 83% 写入 | `guild/internal/ingest/` |
| 行情合并推送 | 200ms 一拍,乱序丢弃,不阻塞扇出 | `guild/internal/hub/` |
| 多实例扇出 | NATS 已写好,单实例时留空即本地扇出 | `guild/internal/hub/nats.go` |
| 存储 | 三张表 + 超表 + 压缩/保留策略 | `guild/migrations/` |
| 诊断回传 | 客户端 warn 及以上自动报到服务端 | `guild/internal/diag/` |
| 数据库迁移 | 编进二进制,启动时自动跑 | `guild/internal/store/migrations/` |
| 交易经济学 | 税费、摩擦、吃单量 | `guild/internal/econ/` |
| Troll 过滤 | 五层,20 个回归测试 | `guild/internal/screen/` |
| 历史聚合 | 日均成交量、加权基准均价 | `guild/internal/histagg/` |
| 销量榜 | 回归趋势 + R²、波动率 | `guild/internal/rank/` |
| 物品目录 | 11391 件,中英文名、分类树、图标 | `guild/internal/catalog/` |
| AODP 客户端 | 双窗口限流、URL 分批 | `guild/internal/aodp/` |
| 执行方式建模 | 市价/挂单四种组合,摩擦 4%–9% | `guild/internal/econ/exec.go` |
| 跨城套利 | 两端量约束、运输时间、troll 兜底 | `guild/internal/arb/` |
| 吃深度 | 走盘口算真实均价和滑点 | `guild/internal/depth/` |
| 资金分配 | 风险调整贪心 + 边际收益曲线 | `guild/internal/portfolio/` |
| 交易记账 | 反推 `absorb_ratio` 和模型准确度 | `guild/internal/store/journal.go` |
| 界面 | 总览 / 机会 / 查价 / 销量榜 / 记账 / 实时 | `guild/internal/webui/assets/` |
| 客户端窗口 | WebView2,纯 Go 无 cgo,可交叉编译 | `guild/cmd/client/ui_windows.go` |
| 本地反向代理 | 同源、注入令牌、服务端挂了给人话 | `guild/internal/clientui/` |
| 周转模型 | 按执行方式算一轮耗时,同城跨城共用 | `guild/internal/econ/exec.go` |
| 发布与下载 | manifest + 白名单 + Range 续传 | `guild/internal/api/release.go` |

### 测试服务端

跑在 `10.30.31.30:18420`,二进制在 `/opt/albion-guild/`,日志 `/opt/albion-guild/server.log`。

```
GET  /                 界面
GET  /ws               行情推送
GET  /api/book         某个物品的盘口
GET  /api/quotes       批量最优价
GET  /api/stats        在线连接数、丢弃数
POST /api/upload       客户端上传挂单
POST /api/diag         客户端上报错误
GET  /api/diag         看成员报上来的错误
GET  /api/scan         最近一次扫描结果(同城机会 + 跨城路线)
POST /api/scan         立刻扫一次
GET  /api/arb          跨城套利路线
GET  /api/portfolio    资金分配方案
GET  /api/depth        吃到指定数量的真实成交均价
GET  /api/trades       交易记录
POST /api/trades       记一笔
POST /api/trades/{id}/close  收口
GET  /api/calibration  实测的 absorb_ratio 和模型准确度
GET  /api/release      更新 manifest(版本、sha256、大小)
GET  /download/{name}  客户端二进制(白名单,支持 Range 续传)
GET  /api/rank         销量榜
GET  /api/lookup       一个物品在所有城市所有品质的价格(旧接口,整格融合,旧客户端在用)
GET  /api/lookup/grid  查价矩阵:城市(含 Brecilien、黑市)× 品质,逐边融合抓包/AODP,带 7/30 日成交和日线
GET  /api/lookup/book  查价右栏:一格两侧完整阶梯(剔上一轮残单),?qty= 时带吃单均价
GET  /api/menu         三级分类菜单
GET  /api/items        搜索 / 按分类浏览
GET  /api/icon/{id}    物品图标(库里缓存)
GET  /api/coverage     数据覆盖情况
POST /api/catalog/sync 重新同步物品目录
```

`/ws` 的协议:

- 客户端 → 服务端:`{"op":"sub"|"unsub","keys":[...],"topics":[...]}`。`keys` 按盘口
  (`item|city|quality|side`)订报价;`topics` 按主题订状态通知,目前只有 `"scan"`。
  两者可以写在同一条里,不认识的主题忽略。老客户端只发 `keys`,行为不变
- 报价消息 `{k,p,d,n,t}`,没有 `type` 字段。`p/d/n` 是剔除幽灵单之后的最优档(价、件数、
  张数),和扫描读簿同一口径;`t` 是这一边的"最近一眼",只进不退。上传落库(flush,至多 2s)
  之后才推,只刷新了 last_seen 的上传也推。`GET /api/quotes` 和推送是同一个来源
- 扫描通知(只发给订了 `scan` 的连接):每次对外发布新的扫描结果(全量扫描、或抓包
  快速重算)之后推一条
  `{"type":"scan","evaluated_at":…,"started_at":…,"digest":"<hex>","opportunities":N,"routes":N,"full":bool}`。
  `digest` 是 `GET /api/scan` **整份输出**的摘要,和那里的 `digest` 是同一个值:相同就不用
  重拉。只剔掉换个时刻再评估就会自己变的几样:`evaluated_at`、所有 `*_age_hours`、
  `coverage[].within_*` 分桶、`stale`/`stale_history`/`future_timestamp` 拒绝的 `detail` 文案。
  所以覆盖率抓包列、`capture` 汇总和错误、被拒明细的来源、物品数和请求数单独变了也会换摘要;
  每次全量 `started_at` 都换,摘要必变。内容没变也照发
  `full=true` 是 AODP 全量。订阅时如果已经发布过,立刻补发最近一条。
  接了 ingest 的服务端还带 `"ingest":{…}`,和 `/api/coverage` 的 `ingest` 段同形状
  (`location_conflicts`、`last_conflict` 等):界面的多开串城横幅跟着通知走,
  不用等下一次全量再去读很贵的 `/api/coverage`
- 主题消息只扇给本实例的连接,不走 NATS(扫描是每个实例各跑各的)。
  背压和报价同一套:连接积压就丢、计进 `/api/stats` 的 `dropped`;
  `/api/stats` 的 `topic_subscribers.scan` 是订阅数,`last_scan_event` 是最近一条通知

重启:

```bash
cd /opt/albion-guild
pkill -x guild-server
nohup ./guild-server -addr 0.0.0.0:18420 \
  -dsn "postgres://postgres:dev@127.0.0.1:55432/flipper" \
  -release-dir /opt/albion-guild/release -release-version "$(date +%Y%m%d)-010" \
  > server.log 2>&1 &
```

建表不用管,迁移编在二进制里,启动时自己跑。常用参数:

| 参数 | 默认 | 说明 |
|---|---|---|
| `-config` | 空 | 扫描器配置 YAML,留空用内置默认值 |
| `-scan` | 30m | 自动扫描间隔,0 表示不自动扫 |
| `-nats` | 空 | 多实例时填,留空走进程内扇出 |
| `-fresh` | 30m | 挂单多久没再看到就不算数 |

配置改完先验一下解析结果:`go run ./cmd/checkconfig config.yaml`

### 编译

```bash
cd guild
go build -tags pcap ./...                      # 本机(需要 libpcap-dev)
go test ./...

# Windows 客户端。两个依赖都不需要 cgo:
#   gopacket 在 Windows 上运行时动态加载 wpcap.dll
#   go-webview2 是纯 Go 的(用 go-winloader 加载 WebView2 loader)
# 所以从 Linux 交叉编译直接过,不用装 mingw。
# -H windowsgui 藏掉控制台窗口,它是个真程序不是脚本
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
  -ldflags "-H windowsgui -X main.version=$(date +%Y%m%d)-010 \
            -X main.defaultServer=http://10.30.31.30:18420" \
  -o ../dist/flipper-client.exe ./cmd/client
```

**版本号补零成三位**(`20260922-010`)。不补的话 `20260922-10`
的字典序小于 `20260922-7`,版本比较只能做"不相等 = 有新版",没法排序。

`defaultServer` 用 ldflags 注入,发给成员的构建自带服务端地址,他们双击就行。

## 还没做

按建议的顺序:

1. **CDK 激活** —— 1 个月有效期,从激活时算起。现在接口全裸奔,
   记账的 owner 也只是个输入框,没有真身份
2. **抓包侧的成交历史** —— `markethistories` 包还没解析,`market_history` 里
   目前只有 AODP 那一路。融合逻辑(同桶优先取抓包)已经写好了,等数据
3. **自更新** —— manifest 接口(`/api/release`)已经有了,客户端那半边还没接:
   下载、校验 sha256、minio/selfupdate 改名替换、重启
4. **`server_min_version` 强制对齐** —— 字段留好了,服务端还没在上传接口拦
5. **实时行情页的物品是写死的** —— 应该跟着用户在查价页看的东西走
6. **跨城的历史价差没存** —— 看不出一条路线是长期存在还是今天才出现

## 踩过的坑

留个记录,免得重新踩。

- **抓包退出挂死**:`pcap.BlockForever` + `gopacket.PacketSource` 的组合,
  ctx 取消后读 goroutine 还卡在驱动里,`handle.Close()` 拿不到锁,进程不退。
  改成自己读 + `SetImmediateMode(true)` + 500ms 读超时。
  另外 Linux 上不开 immediate mode 的话读超时在没流量时根本不触发
- **`live` 表 upsert 只更新价格**:`order_id` 只在单个服务器内唯一,跨服会撞。
  身份字段(item/location/quality/side)必须一起刷新,否则出现
  "T4 的单子带着 T6 的价格"
- **诊断里的 `err` 序列化成 `{}`**:`error` 没有导出字段,`json.Marshal` 出来是空对象。
  而 err 恰恰是排查时最想看的那个,必须先转成文本
- **前端 `nextElementSibling` 是 null**:深度那个 span 是它 td 里唯一的元素,
  得用 `closest("td").nextElementSibling`
- **手动设 `Accept-Encoding: gzip` 会关掉 Go 的自动解压**:一旦自己设了这个头,
  `http.Transport` 就认为调用方要自己处理压缩,拿到的是 gzip 原始字节。
  症状是 JSON 解析报 `invalid character '\x1f'`
- **`toggleAttribute` 设出来的属性值是空串**,CSS 里 `[aria-current=page]` 匹配不上。
  要用 `setAttribute` / `removeAttribute`
- **Go 1.22 的 ServeMux 会拒绝"路径更泛但方法更窄"的组合**:
  `GET /` 和 `/api/` 同时注册会 panic,说 "matches fewer methods but has a
  more general path pattern"。兜底路由不要写方法限定
- **`go:embed` 够不到包目录外面**,所以界面资源必须放在自己的包里
  (`internal/webui/assets/`),不能留在仓库根的 `web/`
- **Go 链接器对不存在的符号静默忽略 `-X`**。服务端之前压根没有 `version`
  变量,构建命令看着对、版本号永远是 dev
- **多客户端并行抓包城市会串**:ADC 整个进程只有一个 `albionState`,
  城市订单的 `LocationId` 在包里是空的、靠它补。我们的 `protocol` 包同样是单例,
  一台机器开多个游戏客户端时要留意

## 还没查的两处可疑(对抗审查挖出来,未修)

都在抓包主链路上,症状都是"日志一切正常、库里没数据",很难从现象反推。

- **`photon.dispatchResponse` 的 `[]string` 分支解出来的挂单可能被静默丢弃。**
  `parser.go` 命中 `[]string` 时构造 `params={0: v}` 就回调并 return,
  里面**没有 253**;而 `protocol.HandleResponse` 第一件事就是 `byteParam(params, 253)`,
  取不到直接 return。两条路径必有一条是死的。这个分支零测试覆盖。
  真要走这条路的话,`protocol` 层应该对"只有 `params[0]` 是 `[]string`"兜底 ——
  `AuctionType` 本来就能判方向,offers/requests 两个 op 不必靠 253 区分
- **分片重组用累加判完成。** `bytesWritten += fragLen` 不做区间去重,
  重传/重复分片会让它提前 `>= totalLength`,于是把还带空洞(全零)的 payload
  当完整消息解析;`fragmentCount` / `fragmentNumber` 读出来后直接丢弃、不校验

## 相关文档

- [倒爷工具口径](flipper.md) —— 为什么这么算,而不是怎么用
- [公会版设计](guild-tool-design.md) —— 表结构、融合算法、架构取舍
- [抓包字段](packet-capture.md) —— 从游戏包里能拿到哪些字段
