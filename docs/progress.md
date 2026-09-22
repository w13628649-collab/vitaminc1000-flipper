# 进度

最后更新:2026-09-22

## 当前状态

**全 Go**,Python 已全部删除。抓包 → 服务端 → PostgreSQL → 浏览器这条链路
端到端通了。交易模型比第一版的 Python demo 宽得多:执行方式、跨城套利、
风险指标、吃深度、资金分配、实盘记账——详见 [flipper.md](flipper.md)。
137 个测试。

```
Windows 客户端(抓包)──HTTP──> Go 服务端 ──> PostgreSQL/TimescaleDB
                                    │
                                    └──WS──> 浏览器(行情跳动)
```

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
| Web 界面 | 总览 / 机会 / 查价 / 销量榜 / 记账 / 实时 | `guild/web/` |

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
GET  /api/rank         销量榜
GET  /api/lookup       一个物品在所有城市所有品质的价格
GET  /api/menu         三级分类菜单
GET  /api/items        搜索 / 按分类浏览
GET  /api/icon/{id}    物品图标(库里缓存)
GET  /api/coverage     数据覆盖情况
POST /api/catalog/sync 重新同步物品目录
```

重启:

```bash
cd /opt/albion-guild
pkill -x guild-server
nohup ./guild-server -addr 0.0.0.0:18420 \
  -dsn "postgres://postgres:dev@127.0.0.1:55432/flipper" > server.log 2>&1 &
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

# Windows 客户端。不需要 cgo/mingw:gopacket 在 Windows 上是运行时
# 动态加载 wpcap.dll 的,所以交叉编译直接过
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
  -ldflags "-X main.version=$(date +%Y%m%d) -X main.defaultServer=http://10.30.31.30:18420" \
  -o flipper-client.exe ./cmd/client
```

`defaultServer` 用 ldflags 注入,发给成员的构建自带服务端地址,他们双击就行。

## 还没做

按建议的顺序:

1. **CDK 激活** —— 1 个月有效期,从激活时算起。现在接口全裸奔,
   记账的 owner 也只是个输入框,没有真身份
2. **抓包侧的成交历史** —— `markethistories` 包还没解析,`market_history` 里
   目前只有 AODP 那一路。融合逻辑(同桶优先取抓包)已经写好了,等数据
3. **Wails 界面** —— 客户端现在是纯命令行的,双击只有一个黑窗口
4. **自更新** —— 单 exe 自更新,manifest 接口自建 + 二进制放 OSS
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
- **多客户端并行抓包城市会串**:ADC 整个进程只有一个 `albionState`,
  城市订单的 `LocationId` 在包里是空的、靠它补。我们的 `protocol` 包同样是单例,
  一台机器开多个游戏客户端时要留意

## 相关文档

- [倒爷工具口径](flipper.md) —— 为什么这么算,而不是怎么用
- [公会版设计](guild-tool-design.md) —— 表结构、融合算法、架构取舍
- [抓包字段](packet-capture.md) —— 从游戏包里能拿到哪些字段
