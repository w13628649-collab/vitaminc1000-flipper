# albion-guild — 公会版第一版

设计见 [../docs/guild-tool-design.md](../docs/guild-tool-design.md)。
这一版是**端到端能跑通的骨架**:客户端上传 → 去重 → 落库 → 实时推给所有在线成员,
浏览器里价格自己跳,不刷新页面。

## 跑起来

```bash
# 1. 数据库(开发用,生产换成阿里云 RDS)
docker run -d --name flipper-pg -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=flipper \
  -p 55432:5432 timescale/timescaledb:latest-pg16
docker exec -i flipper-pg psql -U postgres -d flipper < migrations/0001_init.sql
docker exec -i flipper-pg psql -U postgres -d flipper < migrations/0002_policies.sql

# 2. 服务端
go run ./cmd/server

# 3a. 灌假数据(没有 Windows + Npcap + 游戏时,用它验证整条链路)
go run ./cmd/simulate

# 3b. 或者真抓包(Windows,需要 Npcap;抓包能力要 -tags pcap)
go build -tags pcap -o flipper-client.exe ./cmd/client
./flipper-client.exe -server http://你的服务器:8080

# 4. 打开 http://127.0.0.1:8080
```

`-dsn` 或 `FLIPPER_DSN` 改数据库地址,`-tick` 改行情合并间隔(默认 200ms),
`-fresh` 改"多久没再看到的挂单不算数"(默认 30 分钟)。

## 这一版做了什么

| 模块 | 内容 |
|---|---|
| `internal/photon` | Photon Protocol18 解析,**从 albiondata-client 迁移**(MIT) |
| `internal/pcapdriver` | Windows 的 Npcap/WinPcap 检测,**同上** |
| `internal/capture` | 抓包循环,基于其 `listener.go` 改写,去掉了 dashboard 和自有 log |
| `internal/protocol` | 市场消息 → `model.MarketOrder`,opcode 做成变量(会随版本漂移) |
| `cmd/client` | 命令行客户端:抓包 → 解析 → 批量上传 |
| `internal/model` | 客户端服务端**共享**的类型。放一起是为了消灭字段漂移 |
| `internal/store` | PG 读写。live 表 upsert + event 表 COPY,订单簿和批量快照查询 |
| `internal/ingest` | **内存 LRU 去重**——200 人规模真正的瓶颈在这,不在数据库 |
| `internal/hub` | WS 连接、订阅、**conflation**、乱序丢弃、背压 |
| `internal/api` | 上传/查询/快照接口 + WS,带心跳 |
| `cmd/simulate` | 模拟公会成员翻市场,刻意制造大量重复观测 |
| `web/` | 最简界面,验证价格实时跳动 |

实测:模拟器跑 25 轮,**去重率 83%**——每次上传 130 条里只有十来条是真变化。
这正是设计文档第六节说的"写入放大掉一个数量级"。

## 还没做

- CDK 激活与鉴权(设计文档第八节)
- AODP 兜底拉取与两个来源的融合(第三节)
- 成交历史 `market_history` 的写入与销量榜
- 物品目录同步(`item` 表建好了但还是空的,界面暂时显示 item_id)
- 客户端的 Wails 界面(现在只有命令行版;抓包/解析/上传已经能跑)
- 自动更新、Npcap 引导安装
- NATS 多实例广播——`hub.Broadcaster` 接口已经留好,
  现在装的是 `LocalBroadcaster`,换成 NATS 实现时 `Hub.Fanout` 一行都不用动

## 验证时发现的两个真问题

**① live 表 upsert 必须刷新身份字段。**
最初只更新了价格和数量,结果 order_id 撞号时库里出现"T4 的单挂着 T6 的价"——
数据静默错掉,查起来极难。`order_id` 只在单个服务器内唯一,跨服可能重复,
客户端解析出错时也会撞。

**② 前端取"深度"那一格取错了。**
`el.nextElementSibling` 拿的是 span 的兄弟,而 span 是那个 td 里唯一的元素,
所以永远是 null。得先 `closest("td")` 再取下一个 td。
这个 bug 表现为整片 `Cannot set properties of null`,但页面看起来还在更新
(因为异常后走了整表重绘的兜底路径)。

## 测试

```bash
go test ./...        # 16 个
```

覆盖的是三处最容易出错的逻辑:去重的判断边界、conflation 的合并、
以及扇出时的乱序丢弃和背压——这三个错了都不会崩,只会让数据悄悄不对。


## 关于从 albiondata-client 迁移的部分

没有 fork 整个仓库,只把**核心能力**搬了过来(MIT 许可,归属见根目录 `NOTICE`):

- `internal/photon` —— 1041 行,**零外部依赖**,只用标准库。它的自带测试也一起搬了
- `internal/pcapdriver` —— 170 行,Npcap 四状态检测(装没装、是不是 admin-only、
  有没有遗留的 WinPcap、进程有没有提权),每次启动都查一遍
- `internal/capture` —— 抓包循环按它的 `listener.go` 改写

没搬的:它的 Svelte 界面、上传到 AODP 的逻辑、GitHub Releases 自更新、NATS 客户端。
这些要么我们自己重写,要么不需要。

**opcode 会随游戏版本漂移。** `protocol.DefaultOpCodes()` 里的值对应
albiondata-client `12ff34e`(2026-09-16),并和 OpenRadar 的实测抓包交叉核对过
(`evNewCharacter=29`、`evMove=3` 两边一致)。做成变量而不是常量,
将来可以由服务端下发,不用重新发客户端。

### 抓包能力要 build tag

```bash
go build ./...              # 服务端,不需要 libpcap/Npcap
go build -tags pcap ./...   # 带抓包,需要系统的 pcap 库
```

不带 tag 时 `capture` 包走 stub 返回 `ErrNotBuilt`。
这样服务端镜像不用装 libpcap,也不会因为缺库编译不过。
