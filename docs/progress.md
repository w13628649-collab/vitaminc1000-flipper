# 进度

最后更新:2026-09-22

## 当前状态

第一阶段(Python 单机扫描器)已封版,不再加功能,留作 Go 重写的规格基线。
第二阶段(公会版 Go 客户端/服务端)骨架跑通,**数据链路端到端通了,业务逻辑还没搬**。

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
| Web 行情页 | 极简版,验证 WS 用的,不是最终 UI | `guild/web/index.html` |

### 测试服务端

跑在 `10.30.31.30:8090`,二进制在 `/opt/albion-guild/`,日志 `/opt/albion-guild/server.log`。

```
GET  /              行情页
GET  /api/book      某个物品的盘口
GET  /api/quotes    批量最优价
GET  /api/stats     在线连接数、丢弃数
GET  /ws            行情推送
POST /api/upload    客户端上传挂单
POST /api/diag      客户端上报错误
GET  /api/diag      看成员报上来的错误
```

重启:

```bash
cd /opt/albion-guild
pkill -x guild-server
nohup ./guild-server -addr 0.0.0.0:8090 \
  -dsn "postgres://postgres:dev@127.0.0.1:55432/flipper" > server.log 2>&1 &
```

### 编译

```bash
cd guild
go build -tags pcap ./...                      # 本机(需要 libpcap-dev)
go test ./...

# Windows 客户端。不需要 cgo/mingw:gopacket 在 Windows 上是运行时
# 动态加载 wpcap.dll 的,所以交叉编译直接过
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
  -ldflags "-X main.version=$(date +%Y%m%d) -X main.defaultServer=http://10.30.31.30:8090" \
  -o flipper-client.exe ./cmd/client
```

`defaultServer` 用 ldflags 注入,发给成员的构建自带服务端地址,他们双击就行。

## 还没做

按建议的顺序:

1. **业务算法从 Python 搬过来** —— 这是现在最大的空白。Go 侧一行都还没有:
   `economics`(税费/摩擦)、`filters`(五层 troll 过滤)、`history`(日均成交量)、
   `ranking`(日化收益排序)。Python 那边 531 行 + 74 个测试,照着搬
2. **物品目录同步**,把 `item` 表填上(中英文名、分类树、图标)
3. **AODP 兜底 + 融合** —— 抓包数据优先,没有的用 AODP 补。
   注意 AODP 的 `history` 和抓包的 `markethistories` 是**同一份服务端数据**,
   同一个桶里只能二选一,取平均会重复计数
4. **销量榜** —— 依赖 `market_history` 写入
5. **CDK 激活** —— 1 个月有效期,从激活时算起
6. **Wails 界面** —— 现在客户端是纯命令行的
7. **自更新** —— 单 exe 自更新,manifest 接口自建 + 二进制放 OSS

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
- **多客户端并行抓包城市会串**:ADC 整个进程只有一个 `albionState`,
  城市订单的 `LocationId` 在包里是空的、靠它补。我们的 `protocol` 包同样是单例,
  一台机器开多个游戏客户端时要留意

## 相关文档

- [第一阶段扫描器](stage-one-flipper.md) —— Python 版的完整说明
- [公会版设计](guild-tool-design.md) —— 表结构、融合算法、架构取舍
- [抓包字段](packet-capture.md) —— 从游戏包里能拿到哪些字段
