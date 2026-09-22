# 公会版设计:数据模型、融合算法、架构选型

前置事实见 [packet-capture.md](packet-capture.md)。这份只谈怎么搭。

## 一、先说清楚背包和仓库能拿到什么

**能拿到,明文,但有个硬条件:必须你在游戏里打开过那个界面。**

跟市场完全一样的机制——`AttachItemContainer` 是你点开容器时服务端推给你的,
不点开就没这个包。所以:

| 想要 | 条件 | 事件 |
|---|---|---|
| 背包内容 | 一直有(背包常驻) | `NewSimpleItem` / `NewEquipmentItem` + `InventoryPutItem` / `DeleteItem` |
| 身上装备 | 一直有 | `opJoin` / `CharacterEquipmentChanged` |
| **某个城市的仓库** | **人得在那个城市、点开那个仓库** | `BankVaultInfo` + `AttachItemContainer` |
| 银币 / 金币 | 登录和切区时(`opJoin`),银币变动时(`UpdateMoney`) | — |

**"各大主城的仓库内容"不能一次拿全**:游戏里你看不到别的城市的仓库,
所以抓包也拿不到。要集齐就得**每个城市跑一遍、各点开一次仓库**。
好处是每次点开拿的是完整列表,不是增量。

这跟 AODP 覆盖率是同一个道理:抓包只能抓到"你眼睛看过的东西"。
公会版的价值就在于**人多、眼睛多**——20 个人分散在 6 座城,覆盖面自然就上来了。

## 二、表设计

分四组:身份、角色私有数据、交易、市场共享数据。

### 1. 身份

一个人多个角色(多账号方案),所以成员和角色分开。

```sql
CREATE TABLE guild_member (
    member_id    INTEGER PRIMARY KEY,
    display_name TEXT    NOT NULL,          -- 公会里怎么叫他
    upload_token TEXT    NOT NULL UNIQUE,   -- 客户端上传用
    active       INTEGER NOT NULL DEFAULT 1,
    joined_at    TEXT    NOT NULL
);

CREATE TABLE character (
    character_guid TEXT PRIMARY KEY,        -- opJoin param 1,跨服唯一
    member_id      INTEGER REFERENCES guild_member(member_id),
    name           TEXT NOT NULL,           -- param 2
    guild_name     TEXT,                    -- param 58
    alliance_name  TEXT,                    -- param 79
    server_id      INTEGER,                 -- 1 西 / 2 东 / 3 欧
    first_seen     TEXT NOT NULL,
    last_seen      TEXT NOT NULL
);
```

### 2. 角色私有数据(时序)

银币、金币、焦点都是**会变的量**,存快照而不是原地覆盖——不然就看不出变化了。
不同事件填不同字段,用 `source` 标明这条是哪来的。

```sql
CREATE TABLE character_snapshot (
    id               INTEGER PRIMARY KEY,
    character_guid   TEXT NOT NULL REFERENCES character(character_guid),
    observed_at      TEXT NOT NULL,
    source           TEXT NOT NULL,   -- 'join' | 'update_money' | 'take_silver'
    location_id      TEXT,            -- param 8
    silver           INTEGER,         -- param 33,已 ÷10000
    gold             INTEGER,         -- param 34
    focus            REAL,            -- param 27
    focus_max        REAL,            -- param 28
    learning_points  INTEGER,         -- param 37
    reputation       REAL,            -- param 41
    respec_points    INTEGER,         -- param 43[1]
    is_respec_active INTEGER,         -- param 98
    pos_x            REAL,            -- param 9
    pos_y            REAL,
    UNIQUE (character_guid, observed_at, source)
);
CREATE INDEX idx_snap_char_time ON character_snapshot(character_guid, observed_at DESC);
```

字段大片为 NULL 是正常的:`update_money` 只带银币,`join` 才带全套。
查"当前银币"就是 `WHERE silver IS NOT NULL ORDER BY observed_at DESC LIMIT 1`。

技能单独一张,因为一次事件推全部技能:

```sql
CREATE TABLE character_skill (
    character_guid TEXT NOT NULL,
    observed_at    TEXT NOT NULL,
    skill_id       INTEGER NOT NULL,
    level          INTEGER NOT NULL,
    pct_next       REAL,
    fame           INTEGER,
    PRIMARY KEY (character_guid, observed_at, skill_id)
);
```

### 3. 容器与物品

背包、装备、各城仓库、箱子统一成"容器"。容器内容是**整体刷新**的
(一次 `AttachItemContainer` 给完整列表),所以用快照 + 明细两张表。

```sql
CREATE TABLE container (
    container_guid TEXT PRIMARY KEY,     -- AttachItemContainer / BankVaultInfo 的 guid
    character_guid TEXT NOT NULL REFERENCES character(character_guid),
    kind           TEXT NOT NULL,        -- 'inventory'|'equipment'|'vault'|'chest'
    location_id    TEXT,                 -- 哪座城
    name           TEXT,                 -- 仓库名 BankVaultInfo param 3
    last_seen      TEXT NOT NULL
);

CREATE TABLE container_snapshot (
    snapshot_id    INTEGER PRIMARY KEY,
    container_guid TEXT NOT NULL REFERENCES container(container_guid),
    observed_at    TEXT NOT NULL,
    UNIQUE (container_guid, observed_at)
);

CREATE TABLE container_item (
    snapshot_id  INTEGER NOT NULL REFERENCES container_snapshot(snapshot_id),
    slot         INTEGER NOT NULL,
    item_id      TEXT    NOT NULL,
    quality      INTEGER,
    amount       INTEGER NOT NULL,
    est_value    INTEGER,       -- NewSimpleItem param 4,游戏自己的估价
    crafter_name TEXT,          -- NewEquipmentItem param 5
    PRIMARY KEY (snapshot_id, slot)
);
CREATE INDEX idx_citem_item ON container_item(item_id);
```

**为什么不做"当前持有量"单表**:快照能回答"上周三我仓库里有多少钢锭",
单表只能回答"现在有多少",而现在这个值在你没点开仓库之前本来就是不可信的。
要当前值就在快照上取最新,一个视图的事。

公会总库存 = 所有成员最新快照的并集。这正是公会版最实用的功能之一:
**谁手里有多少货,不用满世界问。**

### 4. 自己的挂单与成交

这是 SPEC 第二阶段那张手工表的自动版。

```sql
CREATE TABLE my_order (
    order_id       INTEGER PRIMARY KEY,   -- 游戏的订单 ID
    character_guid TEXT NOT NULL,
    item_id        TEXT NOT NULL,
    quality        INTEGER,
    location_id    TEXT NOT NULL,
    side           TEXT NOT NULL,         -- 'offer'卖 | 'request'买
    unit_price     INTEGER NOT NULL,
    amount_total   INTEGER NOT NULL,
    amount_left    INTEGER,               -- 每次看到"我的订单"就更新
    created_at     TEXT,
    expires_at     TEXT,
    status         TEXT NOT NULL,         -- 'open'|'filled'|'expired'|'cancelled'
    first_seen     TEXT NOT NULL,
    last_seen      TEXT NOT NULL
);

CREATE TABLE my_trade (
    trade_id       INTEGER PRIMARY KEY,
    character_guid TEXT NOT NULL,
    order_id       INTEGER REFERENCES my_order(order_id),  -- 对得上就填
    mail_id        INTEGER UNIQUE,        -- 邮件来源的天然去重键
    item_id        TEXT NOT NULL,
    location_id    TEXT,
    side           TEXT NOT NULL,
    amount         INTEGER NOT NULL,      -- 实际成交
    amount_ordered INTEGER,               -- 当初挂了多少(过期邮件才有)
    unit_price     INTEGER NOT NULL,
    total_price    INTEGER,
    tax            INTEGER,               -- 邮件正文里直接带税额
    settled_at     TEXT NOT NULL
);
```

**成交率** = `amount / amount_ordered`,**成交时长** = `settled_at - created_at`。
跑一个月就能把 `sizing.absorb_ratio` 那个拍脑袋的 0.20 换成真值,
而且能按物品、按城市分开算。

`mail_id UNIQUE` 是关键:同一封邮件被多个客户端读到(比如你换了台机器),
插入会撞唯一约束,天然去重。

### 5. 市场共享数据 —— 两个来源分开存

**这是整个设计最要紧的一处:不要把 AODP 和抓包塞进同一张表再用 source 区分。**
它们的**字段集不一样**,硬塞会让一半字段永远是 NULL。

```sql
-- 抓包挂单:有数量、有订单 ID
CREATE TABLE market_order (
    order_id     INTEGER NOT NULL,
    observed_at  TEXT    NOT NULL,
    item_id      TEXT    NOT NULL,
    location_id  TEXT    NOT NULL,
    quality      INTEGER NOT NULL,
    enchant      INTEGER NOT NULL DEFAULT 0,
    side         TEXT    NOT NULL,
    unit_price   INTEGER NOT NULL,
    amount       INTEGER NOT NULL,      -- AODP 给不了的东西
    expires_at   TEXT,
    reporter     TEXT,                  -- 哪个角色抓到的
    PRIMARY KEY (order_id, observed_at)
);
CREATE INDEX idx_mo_lookup ON market_order(item_id, location_id, quality, side, observed_at DESC);

-- AODP REST 价格:只有价格和时间戳,没有数量
CREATE TABLE aodp_price (
    item_id      TEXT    NOT NULL,
    location_id  TEXT    NOT NULL,
    quality      INTEGER NOT NULL,
    sell_min     INTEGER,
    sell_min_at  TEXT,
    buy_max      INTEGER,
    buy_max_at   TEXT,
    fetched_at   TEXT    NOT NULL,
    PRIMARY KEY (item_id, location_id, quality, fetched_at)
);
CREATE INDEX idx_ap_lookup ON aodp_price(item_id, location_id, quality, fetched_at DESC);

-- 成交历史:字段集一致,可以同表,source 进主键
CREATE TABLE market_history (
    item_id      TEXT    NOT NULL,
    location_id  TEXT    NOT NULL,
    quality      INTEGER NOT NULL,
    timescale    INTEGER NOT NULL,      -- 0 时/1 日/2 周(抓包)或 24(AODP)
    bucket       TEXT    NOT NULL,      -- 这一格的起始时刻
    item_count   INTEGER NOT NULL,
    silver_total INTEGER,               -- 抓包有,AODP 没有
    avg_price    INTEGER NOT NULL,
    source       TEXT    NOT NULL,      -- 'aodp' | 'capture'
    observed_at  TEXT    NOT NULL,
    PRIMARY KEY (item_id, location_id, quality, timescale, bucket, source)
);
```

## 三、融合算法

你说的"两种数据一起算平均"要分情况——**先看清哪两份数据是同源的。**

| 我们要的 | AODP | 抓包 | 两边关系 |
|---|---|---|---|
| 当前买卖价 | `prices`,只有价格 | `marketorders`,价格 + **数量** + 订单 ID | **同一市场状态的两次观测**,时间点不同 |
| 挂单深度 | 没有 | 有 | 只能靠抓包 |
| 成交历史 | `history`,日聚合 | `markethistories`,同一批数据但带 `silver_total` | **同一份服务端数据的两个抄本** |
| 自己的成交率 | 没有 | `my_trade` | 只能靠抓包 |

### 当前价:按时间取最新,不做平均

同一时刻的市场只有一个真实状态,两个来源是对它的两次观测。所以**取最新那次**,
不是取平均——平均只会把一条 15 分钟前的真价和一条 18 小时前的旧价搅成一个假价。

```sql
-- 每个 (物品,城市,品质,方向) 各取两个来源里最新的一条,再择一
WITH cap AS (
    SELECT item_id, location_id, quality, side,
           MIN(CASE WHEN side='offer' THEN unit_price END) AS sell_min,
           MAX(CASE WHEN side='request' THEN unit_price END) AS buy_max,
           SUM(amount) AS depth,
           MAX(observed_at) AS at
    FROM market_order
    WHERE observed_at > :since
    GROUP BY item_id, location_id, quality, side
),
aodp AS (
    SELECT item_id, location_id, quality, sell_min, buy_max, NULL AS depth,
           MAX(fetched_at) AS at
    FROM aodp_price
    WHERE fetched_at > :since
    GROUP BY item_id, location_id, quality
)
-- 然后按 at 取大的那条,把 source 和 at 一起返回给界面
```

界面上必须把**来源**和**数据龄**跟价格一起显示(现在的扫描器已经这么做了)。
抓包赢的时候还能多给一列**深度**(该价位挂单量合计),这是 AODP 永远给不了的。

### 成交历史:同一格优先抓包,不要相加

`market_history` 里同一个 `(item, city, quality, timescale, bucket)` 可能有两行
(`source='aodp'` 和 `'capture'`)。**它们是同一批成交的两种记录,相加就是重复计数。**

取数规则:

```
优先 capture(有 silver_total,能算精确加权均价)
capture 缺这一格 → 用 aodp
两边都有 → 只用 capture,把 aodp 那行留着做对账
```

"两种数据一起算"真正成立的地方是**时间轴上的并集**:抓包在 T1 观测到、
AODP 在 T2 观测到,两个点都保留,时序就比单用一边更密。
**同一时间格上是择一,不是相加。**

### 现有代码怎么改

`src/flipper/history.py` 的 `aggregate` 和 `ranking.py` 的 `rank` 现在直接读
`history_daily` 单表。加 source 后只需改取数那一步:

```sql
-- 同一格按来源优先级择一
SELECT * FROM market_history h
WHERE NOT EXISTS (
    SELECT 1 FROM market_history b
    WHERE b.item_id=h.item_id AND b.location_id=h.location_id
      AND b.quality=h.quality AND b.timescale=h.timescale AND b.bucket=h.bucket
      AND b.source='capture' AND h.source='aodp'
)
```

三个口径(剔当天、分母是有数据的天数、成交量加权)保持不变。

## 四、架构:别自己写客户端,fork albiondata-client

我去翻了它的源码,**你想要的那个壳它已经做完了**:

| 你的顾虑 | ADC 现状 |
|---|---|
| 抓包 | `gopacket` + Npcap,Windows/Linux/macOS 三平台的网卡枚举都写好了 |
| 协议解析 | Protocol18 反序列化、分片重组、加密包识别 |
| "只开一个软件" | **它是 Wails v3 桌面应用**:Go 后端 + Svelte 前端跑在原生 webview 里,带系统托盘 |
| 打包 | `//go:embed all:frontend/dist` 把前端塞进单个二进制,一个 exe |
| **自动更新** | `go-githubupdate` + `minio/selfupdate`,每小时轮询 GitHub release,**原地替换单文件** |
| 上传到自己的服务端 | `-p` 参数本来就是干这个的,支持 `http://` / `nats://` |
| 发版 | 多平台构建脚本、资产命名约定都有现成的 |

从零写一个 C#/Electron 客户端,上面每一行都要重做一遍,而且抓包和 Protocol18
这两块最容易踩坑。

### 建议的形态

```
[公会成员的 Windows 机器]
  fork 的 albiondata-client (单个 exe,自动更新)
    ├── 抓包 → 解析 → 双发:AODP 公共 + 你的服务端(-p)
    └── Wails 窗口
          ├── 本地页:抓包状态、上传计数、当前角色(这些数据在本地)
          └── 查询页:直接开服务端的网页

[你的服务器]
  Python 服务端(复用现有 FastAPI + 扫描器/查价/销量榜)
    ├── 接收上传(/marketorders, /marketnotifications, ...)
    ├── 定时拉 AODP 兜底
    └── Web 界面 ← 成员在客户端窗口里看到的就是这个
```

**为什么查询界面放服务端而不是打进客户端**:UI 和算法改一次就要让 20 个人
更新一次客户端,太重。放服务端的话客户端只在**抓包逻辑或协议变了**才需要发版——
而协议变化频率(每次游戏大更新)本来就远低于你调 UI 的频率。

客户端要加的活:

1. 补 handler:`MyOpenOffers` / `FinishedAuctions` / `AttachItemContainer` /
   `BankVaultInfo` / `NewSimpleItem` / `UpdateMoney` / `CraftItemFinished`,
   以及把 `evSkillData` 重新接线(找新 event code)
2. 修过期邮件那个 `body[1]` 的 bug,不然 `Sold` 拿不到
3. 上传时带 `upload_token` 和 `character_guid`
4. 一个"我在哪座城、点开过哪些仓库"的提示,引导成员把覆盖面刷全

### 语言

- **客户端 Go** —— 因为是 fork,没得选,也不需要选
- **服务端 Python** —— 现有的扫描/查价/销量榜、troll 过滤、经济学模型全都能直接用,
  这些是这个项目真正的资产

### 库的选择

SQLite 起步。粗估:20 人 × 每天翻 500 个物品 × 7 城 ≈ 7 万条挂单/天,
一年 2500 万行。SQLite 撑得住,但要做两件事:

- `market_order` 按月分表或定期归档(只保留最近 N 天的明细,老数据压成 history)
- 索引按 `(item_id, location_id, quality, side, observed_at DESC)` 建

真扛不住再换 PostgreSQL。查询形态是典型时序,到时候 TimescaleDB 的
hypertable + 连续聚合会比手写归档省事。**但现在别上,SQLite 一个文件的运维成本
优势在公会这个规模下压倒一切。**


---

# 200 人规模的补充决策

## 五、时序数据库:别上 Prometheus

我知道这个直觉从哪来——SRE 看到"时序"两个字就想到 Prometheus。但这里不合适,
原因不是性能,是**数据性质不对**。

### Prometheus 存的是指标,我们存的是事实

| | 指标(Prometheus 的活) | 事实(我们的数据) |
|---|---|---|
| 精度要求 | 可采样、可丢点,聚合后原始值无所谓 | **一条挂单就是一条,不能丢不能糊** |
| 典型问题 | "过去 5 分钟 QPS 趋势" | "9 月 3 日 14:22 谁在 Martlock 挂了 200 个钢锭,什么价" |
| 维度关联 | 标签自包含 | 要 JOIN 物品名、分类、角色、公会成员 |
| 写入模型 | pull | push |

Prometheus 对第二列每一行都是错的工具。

### 更硬的理由:基数

把 `item_id` 当 label 是教科书级的高基数反模式:

```
11391 物品 × 8 城 × 5 品质 = 455,640 series
再乘买卖两个方向        = 911,280 series
```

这还没算附魔。Prometheus 单实例在百万 series 量级就要开始为内存操心,
而这些 series 里绝大多数**一天只有几个点**——典型的稀疏高基数,正是它最不擅长的形态。

而 PG 里这就是一张表的三个索引列,45 万个组合毫无压力。

### SLS 呢

能跑,但不划算:按量计费在这个写入量下成本不可控,查询灵活性不如 SQL,
还锁厂商。**真要用它,合理位置是存原始包日志做回溯审计,不是当主库。**

### 结论

**PostgreSQL + TimescaleDB,一个库全包。**

```sql
SELECT create_hypertable('market_order', 'observed_at', chunk_time_interval => INTERVAL '1 day');

-- 连续聚合:代替手写归档
CREATE MATERIALIZED VIEW market_daily
WITH (timescaledb.continuous) AS
SELECT item_id, location_id, quality,
       time_bucket('1 day', observed_at) AS day,
       min(unit_price) FILTER (WHERE side='offer')   AS sell_min,
       max(unit_price) FILTER (WHERE side='request') AS buy_max,
       sum(amount)                                    AS depth
FROM market_order
GROUP BY item_id, location_id, quality, day;

-- 明细压缩 + 过期
SELECT add_compression_policy('market_order', INTERVAL '14 days');
SELECT add_retention_policy('market_order', INTERVAL '180 days');
```

明细保 14 天热的、180 天压缩的,更老的只留连续聚合。
真要做监控(上传 QPS、解析失败率、在线客户端数),**那才是 Prometheus 的活**,
跟业务库分开,别混。

## 五之二、澄清:挂单数据仍然是时序,而且是更好的时序

这个疑问值得单独说清楚,它决定了整个库怎么设计。

### 两种时序形态

| | **点型(快照)** | **区间型(实体生命周期)** |
|---|---|---|
| 长什么样 | "T 时刻,钢锭在 Martlock 最低卖价 1000" | "order#123 是钢锭 1000 银 50 件,10:00 出现,10:30 还在" |
| 一行代表 | 一次观测 | 一张挂单的一段状态 |
| 谁能给 | AODP `prices`、我们的抓包都能 | **只有抓包能给** |
| 存在哪 | `aodp_price` | `market_order` |

**两张表都在,没有谁替代谁。** AODP 只能给点型,所以它单独一张表;
抓包能给区间型,信息量更大,所以单独一张。

### 去重去掉的是重复观测,不是价格变化

```
10:00  A 看到  order#123  1000银 × 50件   → 写入新行
10:05  B 看到  order#123  1000银 × 50件   → 完全一样,只更新 last_seen
10:12  C 看到  order#123  1000银 × 50件   → 同上
10:30  A 看到  order#123  1000银 × 30件   → 数量变了,写入新行
11:00  没人再看到它                        → last_seen 停在 10:30
```

库里留下两行,但**时序信息一点没丢**:

- 这张单 10:00 出现,价 1000
- 10:00–10:30 之间被吃掉 20 件
- 11:00 前后消失(卖完、撤单或过期)

被去掉的只是 10:05 和 10:12 那两次"什么都没发生"的重复观测。
**如果不去重,那两行写进去也推不出任何新信息,只是把库撑大。**

### 当前价和历史价都是查询,不是存储

有了挂单明细,快照是**推导出来的**:

```sql
-- 此刻的最低卖价和深度
SELECT min(unit_price), sum(amount)
FROM market_order
WHERE item_id = 'T4_METALBAR' AND location_id = 'Martlock'
  AND side = 'offer' AND last_seen > now() - interval '15 minutes';

-- 昨天下午三点的市场长什么样 —— 那一刻还活着的挂单
SELECT unit_price, amount
FROM market_order
WHERE item_id = 'T4_METALBAR' AND location_id = 'Martlock' AND side = 'offer'
  AND observed_at <= '2026-09-21 15:00' AND last_seen >= '2026-09-21 15:00'
ORDER BY unit_price;
```

**反过来推不了。** 存快照只知道"那一刻最低价 1000",存挂单才知道
"1000 的只有 5 件、1050 的有 200 件、1100 的有 3000 件"——
这条深度曲线正是现在这套 troll 过滤费半天劲想绕开的东西。

所以不是"降级成静态数据",是**升级成更底层的时序**:
点型是区间型的一个投影,反之不成立。

### 一个必须记住的边界:挂单消失 ≠ 成交

`last_seen` 之后这张单不见了,可能是卖掉了,也可能是**撤单或到期**。
AODP 官方文档自己也说,订单没有成交事件,只能"记录最后出现时间,
超过 X 小时未再出现视为已成交"——那是启发式,不是事实。

真实成交量还是得看:

- `market_history`(服务端算好的成交聚合)
- `my_trade`(自己的成交邮件,这个是准的)

**别拿挂单消失去算成交量。** 这两件事在库里是分开的表,别在查询里混。

## 六、200 人真正的瓶颈是写入放大,不是数据库

这条比选什么库重要得多。

**同一张挂单会被很多人看到。** 200 人里假设 60 个活跃,大家都在几座主城翻同样
那批热门材料。一张挂在 Martlock 的钢锭卖单,一天可能被 30 个人各看到 5 次
= 150 条一模一样的记录。

不去重的话:

```
60 活跃 × 每天 100 次市场查询 × 每次返回 30 张挂单 = 18 万条/天
其中真正不同的挂单状态可能只有 1-2 万条
```

**写入放大 10 倍以上,而且全是垃圾。**

### 三层去重

1. **客户端内**:ADC 已有 5 分钟窗口去重,保留
2. **上传前**:客户端本地记住 `(order_id, unit_price, amount)`,三者都没变就不发
3. **服务端**:唯一约束兜底,变化才写新行

```sql
-- 只在挂单状态真的变了才留新行
CREATE UNIQUE INDEX uq_mo_state
    ON market_order (order_id, unit_price, amount);

-- 插入时忽略重复,只更新"最后一次看到"
INSERT INTO market_order (...) VALUES (...)
ON CONFLICT (order_id, unit_price, amount)
DO UPDATE SET last_seen = EXCLUDED.observed_at;
```

加 `last_seen` 字段,一张挂单的生命周期就是 `observed_at`(第一次看到)到
`last_seen`(最后一次看到)。**这顺带给了 AODP 永远给不了的东西:挂单存活时长,
也就是这个价位到底多久才被吃掉。**

做完这层,写入量掉一个数量级,一年 PG 里也就几千万行,压缩后几个 GB。

## 七、Go 全栈:可行,成本比想象小

我量了一下现有 Python 的核心算法:

| 模块 | 纯逻辑行数 | 内容 |
|---|---|---|
| `economics.py` | 63 | 税费、摩擦、吃单量 |
| `filters.py` | 132 | 五层 troll 过滤 |
| `history.py` | 100 | 三个聚合口径 |
| `ranking.py` | 236 | 排行 + 最小二乘趋势 |
| **合计** | **531** | |

**531 行,不是重写一个系统。** 加上 870 行测试(74 个用例)——
那些测试是**规格说明**,移植时逐条对照跑,比从零写安全得多。

### 全 Go 的真正好处不是性能,是共享类型

客户端和服务端用**同一份 struct 定义**:

```go
// 放在 shared/ 里,客户端解析后直接发,服务端直接收
type MarketOrder struct {
    OrderID    int64  `json:"order_id"`
    ItemID     string `json:"item_id"`
    LocationID string `json:"location_id"`
    Quality    uint8  `json:"quality"`
    Side       string `json:"side"`
    UnitPrice  int64  `json:"unit_price"`
    Amount     int32  `json:"amount"`
    ObservedAt int64  `json:"observed_at"`
}
```

两边字段漂移是这类系统最常见的线上事故来源,同语言同定义直接消灭这个问题。
跨语言的话就得靠 JSON Schema 或 protobuf 再加一套生成流程。

Go 侧要补的库:中位数、线性回归自己写(各 20 行),`gonum` 也行。

### 建议的迁移顺序

现有 Python **别急着删**,让它当对照:

1. Go 服务端先跑起来,接收 + 存储 + AODP 拉取
2. 算法逐个移植,每移一个就拿 Python 的测试用例当黄金标准跑一遍
3. 两边输出对得上了,Python 那份留作文档和回归基线

## 八、CDK 与访问控制

200 人、1 个月有效期,要能发、能停、能查谁在用。

```sql
CREATE TABLE cdk (
    code         TEXT PRIMARY KEY,           -- 发给成员的激活码
    batch        TEXT,                       -- 哪一批发的,便于整批作废
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_days   INTEGER NOT NULL DEFAULT 30,
    max_devices  INTEGER NOT NULL DEFAULT 2, -- 一个人可能两台机器
    note         TEXT,                       -- 发给谁了
    revoked_at   TIMESTAMPTZ                 -- 拉黑
);

CREATE TABLE activation (
    activation_id BIGSERIAL PRIMARY KEY,
    code          TEXT NOT NULL REFERENCES cdk(code),
    device_id     TEXT NOT NULL,             -- 客户端首次启动生成并持久化
    member_id     BIGINT REFERENCES guild_member(member_id),
    activated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,      -- activated_at + valid_days
    last_seen_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    UNIQUE (code, device_id)
);
CREATE INDEX idx_act_expiry ON activation(expires_at) WHERE revoked_at IS NULL;
```

### 关键决策:有效期从**激活**算起,不是从**生成**算起

从生成算的话,你提前发一批码、有人两周后才装,他就只剩两周。
`expires_at = activated_at + valid_days` 才是符合直觉的。

### 客户端流程

```
首次启动
  → 生成 device_id(机器码 hash,持久化到本地)
  → 让用户输入 CDK
  → POST /activate {code, device_id}
  → 服务端校验:码存在、未撤销、该码激活设备数 < max_devices
  → 返回 access_token(JWT,内含 expires_at)
  → 本地存 token

之后每次启动
  → 带 token 调 /session
  → 服务端返回 {ok, expires_at, server_min_version}
  → 剩余 < 7 天 → 客户端显著提示续期
  → 已过期 → 只读模式或直接停,但**别停抓包上传**(见下)
```

### 一个建议:过期了停查询,别停上传

过期的人继续上传数据对公会是净收益,停掉反而损失覆盖率。
把"能不能看"和"能不能传"两个权限分开:

```
token 过期 → 查询接口 403,上传接口照收
```

### 版本协商,别让 200 人版本参差

`/session` 顺便返回 `server_min_version`:

```
客户端版本 < server_min_version
  → 强制走自动更新,更新完再进
```

协议一变(游戏更新导致 opcode 漂移),你把 `server_min_version` 一提,
所有人下次启动自动跟上。**这是 200 人规模下最省心的一招**,
比在群里喊"大家更新一下"可靠得多。

## 九、客户端 UI 放哪:你的方案可行

我上一轮建议把查询界面放服务端,理由是"改 UI 就要发版"。
**你的自动更新方案把这个理由消掉了**——全自动更新的话,发版不再是痛点。

剩下的权衡:

| | UI 打进客户端 | UI 在服务端 |
|---|---|---|
| 响应 | 本地渲染,快 | 每次拉页面 |
| 离线 | 能看已缓存的数据 | 打不开 |
| 改一次 UI | 发版 + 200 人更新(自动,但有滞后) | 立即生效 |
| 调试 | 要问用户版本号 | 所有人同一份 |

**实际建议:骨架打进客户端,数据全走 API。**

Wails 的 Svelte 前端做布局和交互,所有数值从服务端 API 取。
这样"改阈值、改算法、改排序逻辑"这类高频迭代不用发版,
只有"加一个新页面、改布局"才需要。多数迭代属于前者。


---

# PostgreSQL 落地(阿里云 RDS)

## 十、所有商品一张表,按时间分区,不按商品分

你的理解是对的:**逐单明细,不是"T 时刻最低价多少件"那种聚合**。
每张买单、每张卖单各自一条,带价格、数量、物品、品质、附魔、城市、时间。

至于分表——**不要按商品分。** 按时间分。

### 为什么不按商品分

| 做法 | 后果 |
|---|---|
| 一个商品一张表 | 11391 张表。加一个新物品要 DDL,游戏更新一次加几十张 |
| 跨商品查询 | 排行榜、分类浏览要 UNION 上万张表,规划器直接跪 |
| 索引维护 | 11391 套索引,`ANALYZE` 和 `VACUUM` 全部乘以 11391 |
| 数据倾斜 | T4_CLOTH 千万行,某个冷门神器 3 行,分区毫无意义 |

**商品是查询条件,不是分区键。** 分区键要选"查询总是带范围、且数据随之增长"
的维度——那是时间。物品用复合索引解决。

### 表结构(PG + TimescaleDB)

分两张:一张当前态、一张历史流。这是时序系统的常规做法,别合成一张。

```sql
-- ① 当前活跃挂单:普通表,可 upsert,查"现在什么价"走这张
CREATE TABLE market_order_live (
    order_id     BIGINT      PRIMARY KEY,
    item_id      TEXT        NOT NULL,
    location_id  TEXT        NOT NULL,
    quality      SMALLINT    NOT NULL,
    enchant      SMALLINT    NOT NULL DEFAULT 0,
    side         SMALLINT    NOT NULL,      -- 0=卖单 offer, 1=买单 request
    unit_price   BIGINT      NOT NULL,      -- 已 ÷10000 的银币
    amount       INTEGER     NOT NULL,
    expires_at   TIMESTAMPTZ,
    first_seen   TIMESTAMPTZ NOT NULL,
    last_seen    TIMESTAMPTZ NOT NULL,
    reporter     TEXT
);

-- 查当前价和深度的主力索引
CREATE INDEX idx_live_book
    ON market_order_live (item_id, location_id, quality, side, unit_price)
    INCLUDE (amount, last_seen);

-- 清理:很久没再看到的挂单,搬去历史表
CREATE INDEX idx_live_stale ON market_order_live (last_seen);
```

```sql
-- ② 历史变更流:hypertable,纯 append,查"当时什么价"走这张
CREATE TABLE market_order_event (
    order_id     BIGINT      NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,
    item_id      TEXT        NOT NULL,
    location_id  TEXT        NOT NULL,
    quality      SMALLINT    NOT NULL,
    enchant      SMALLINT    NOT NULL DEFAULT 0,
    side         SMALLINT    NOT NULL,
    unit_price   BIGINT      NOT NULL,
    amount       INTEGER     NOT NULL,
    reporter     TEXT
);

SELECT create_hypertable('market_order_event', 'observed_at',
                         chunk_time_interval => INTERVAL '1 day');

CREATE INDEX idx_evt_lookup
    ON market_order_event (item_id, location_id, quality, side, observed_at DESC);

SELECT add_compression_policy('market_order_event', INTERVAL '14 days');
SELECT add_retention_policy('market_order_event', INTERVAL '365 days');
```

**一张表管所有商品。** `item_id` 是索引的第一列,选择性足够
(11391 个值),PG 走 index scan 毫无压力。

### 写入路径:内存去重 → 不要指望数据库 ON CONFLICT 兜底

hypertable 上的唯一约束必须包含时间列,`ON CONFLICT (order_id, unit_price, amount)`
根本建不出来。所以去重放在服务端内存:

```go
// 活跃挂单的最后已知状态。几十万条,几十 MB,够用
type orderState struct{ price int64; amount int32 }
var seen = lru.New[int64, orderState](500_000)

func ingest(o MarketOrder) {
    prev, ok := seen.Get(o.OrderID)
    if ok && prev.price == o.UnitPrice && prev.amount == o.Amount {
        // 状态没变:只刷 last_seen,不写历史
        batchTouch(o.OrderID, o.ObservedAt)
        return
    }
    seen.Put(o.OrderID, orderState{o.UnitPrice, o.Amount})
    batchUpsertLive(o)     // ① 当前态
    batchAppendEvent(o)    // ② 历史流
}
```

`batchTouch` 攒一批再 `UPDATE ... WHERE order_id = ANY($1)`,
几百个 id 一次,比逐条 update 快两个数量级。

这层做完,200 人的写入落到库里只剩真实的状态变化。

### 买单卖单同一张表

字段完全一样,而且算价差时**本来就要同时看两边**——分表只会让每个查询多一次 JOIN。
用 `side` 区分,`SMALLINT` 比 `TEXT` 省空间也快。

### 典型查询

```sql
-- 某物品在某城的当前订单簿(买卖两侧 + 深度)
SELECT side, unit_price, sum(amount) AS depth, count(*) AS orders
FROM market_order_live
WHERE item_id = 'T5_METALBAR' AND location_id = 'Martlock' AND quality = 1
  AND last_seen > now() - interval '30 minutes'
GROUP BY side, unit_price
ORDER BY side, unit_price;

-- 全服某物品的最低卖价(扫描器要的)
SELECT location_id, min(unit_price) AS sell_min, sum(amount) AS depth
FROM market_order_live
WHERE item_id = 'T5_METALBAR' AND quality = 1 AND side = 0
  AND last_seen > now() - interval '30 minutes'
GROUP BY location_id;
```

## 十之二、数据量:你的直觉对,但变量不是物品数

拿库里的真实样本外推(164 物品 × 7 城 × 31 天 = 17,068 条日线):

- 实际有成交的 `(物品,城市,品质)` 组合:**856** 个
- 每个物品平均只产生 **5.2** 个活跃组合(挂了 7 城 × 5 品质,但绝大多数格子是空的)
- 有数据率 64%(一个组合平均 31 天里有 20 天成交)

按这个密度外推全目录:

| 范围 | 粒度 | 行/年 | 容量/年(未压缩) |
|---|---|---|---|
| 3000 个有交易的物品 | **日级** | 3.7 M | **0.3 GB** |
| 全部 11391 个物品 | **日级** | 14 M | **1.0 GB** |
| 3000 个物品 | 小时级 | 88 M | 6.2 GB |
| 全部 11391 个物品 | 小时级 | **335 M** | **23 GB** |

**你说得对:物品记全了也没多少量——前提是日级。**
真正让它"越来越大"的不是物品数,是**时间粒度**,差 24 倍。

### 两类数据的增长性质完全不同

| | 历史成交 `market_history` | 挂单事件 `market_order_event` |
|---|---|---|
| 写入方式 | **幂等 upsert**,同一格只有一行 | **append**,每次状态变化一条 |
| 增长驱动 | 活跃组合数 × 时间格数 | 观测次数 × 变化频率 |
| 有没有上界 | **有**。200 人传和 2000 人传,行数一样 | 没有,人越多传得越多(但去重后只记真实变化) |

历史成交这张表,**再多人上传也不会变大**——大家看到的是同一份服务端聚合,
重复写只是 upsert。这是它和挂单流最根本的区别。

挂单事件粗估:3000 物品 × 5.2 组合 × 每组合 10 张活跃挂单 ≈ 15 万张,
按 2 天周转、每张生命周期内 4 次状态变化算,**约 30 万条/天 ≈ 1.1 亿行/年、8 GB**。
这个是去重之后的数(没去重要乘 10)。

### 分层保留

```sql
-- 小时级只留 90 天(看日内波动够用了)
SELECT add_retention_policy('market_history_hourly', INTERVAL '90 days');

-- 连续聚合:小时级自动降采样成日级,日级永久留着
CREATE MATERIALIZED VIEW market_history_daily
WITH (timescaledb.continuous) AS
SELECT item_id, location_id, quality,
       time_bucket('1 day', bucket) AS day,
       sum(item_count)              AS item_count,
       sum(silver_total)            AS silver_total,
       sum(silver_total) / NULLIF(sum(item_count), 0) AS avg_price
FROM market_history_hourly
GROUP BY item_id, location_id, quality, day;

-- 挂单明细:14 天后压缩,180 天后扔掉(那时聚合已经存下来了)
SELECT add_compression_policy('market_order_event', INTERVAL '14 days');
SELECT add_retention_policy('market_order_event', INTERVAL '180 days');
```

跑下来常驻大概:日级历史 1 GB/年(永久)+ 小时级 1.5 GB(滚动 90 天)
+ 挂单明细 4 GB(滚动 180 天,压缩后)。**阿里云最小规格的 RDS 都够跑几年。**

真正该担心的从来不是容量,是**索引和查询计划**——4 GB 数据上一个没走索引的
全表扫描,比 40 GB 数据上走对索引慢得多。

## 十之三、一条数据进来之后走哪几步

先纠正一个类比:**NATS 不是缓存,它和 Redis 做缓存完全不是一回事。**

| | Redis 当缓存 | NATS Pub/Sub |
|---|---|---|
| 本质 | **存储**。写进去,之后能读出来 | **传输**。发出去就没了,不能查 |
| 为什么用 | 读得比 PG 快 | 把消息送到**别的进程** |
| 没有它会怎样 | 每次都查库,慢 | 单实例:毫无影响。多实例:另一台机器上的客户端收不到 |

所以"先写 PG 再写 NATS"这个说法里,NATS 那一步不是在存第二份,
而是在**喊一嗓子让别的实例知道**。单实例的时候这一嗓子是喊给自己听的,
所以直接函数调用就行,压根不需要 NATS。

### 完整路径

```
客户端 HTTP 批量上传
   │
   ├─ 鉴权(CDK token)
   │
   ├─ 内存 LRU 去重 ──── 状态没变 ──→ 只攒一个 last_seen 待更新,到此为止
   │                                    (这里挡掉的是大头,见第六节)
   │  状态变了
   │  ↓
   ├──────────────┬──────────────────┐
   │              │                  │
   ↓              ↓                  ↓
PG 写入队列    conflator          (响应客户端 200,不等下面两步)
(批量 flush)   (200ms 合并)
   │              │
   ↓              ↓
market_order_live  Broadcaster.Publish
market_order_event      │
                        ├─ 单实例:hub.Fanout()  ← 内存里的函数调用
                        └─ 多实例:发 NATS → 各实例订阅到 → 各自 hub.Fanout()
```

### 写库和推送是并行的,不要串起来

**别写成"等 PG 提交成功再推送"。** 两者的失败模式不一样,串联只会让慢的那个拖死快的:

- PG 偶尔抖一下(RDS 主备切换、大事务、vacuum),推送不该跟着卡住
- 推送失败(某个客户端断线)也不该影响入库

代价是有个很窄的窗口:数据推出去了但 PG 写失败。对行情来说可以接受——
这个价格下次有人翻到同一个物品就会补上,而且 conflation 本来就在丢中间值。
但**PG 写失败必须进重试队列并告警**,不能静默吞掉。

### 那要不要缓存

**暂时不需要独立缓存层。**

- 当前价:`market_order_live` 走复合索引,本来就快
- 历史聚合:TimescaleDB 的连续聚合**已经是物化好的结果**,查的就是算好的表
- 排行榜这类重计算:Go 进程里一个带 TTL 的 map 就够(200 人共享同一份结果)

真到了需要独立缓存的规模,再加 Redis 不迟。**现在加等于多一个要运维的东西,
却没有对应的问题要解决**——这跟前面说 NATS 不要提前上是同一个道理。

## 十一、传输:上传走 HTTP 批量,推送才用 WS

"少握手"这个出发点对,但 **HTTP keep-alive 已经解决了握手问题**——
一条 TCP 连接上跑成千上万个请求,HTTP/2 还能多路复用。
WS 省下的那点握手在这个量级下量不出来。

先看实际负载:200 人、60 活跃,批量上传(攒 5 秒或 200 条发一次),
**峰值也就每秒十几个请求**。这个量级 HTTP 和 WS 没有性能差别。

真正的差别在别处:

| | HTTP POST | WebSocket |
|---|---|---|
| 重试/幂等 | 标准做法,失败重发就行 | 要自己设计 ack 和序号 |
| 负载均衡 | 无状态,随便加机器 | 连接有粘性,扩容要处理 |
| 限流/鉴权 | 网关层现成 | 得在应用层自己做 |
| 断线 | 无所谓,下次再发 | 要心跳 + 重连 + 补发 |
| 阿里云 SLB/日志 | 开箱即用 | 要额外配置 |
| **服务端主动推** | 做不到(要轮询) | **这才是它的主场** |

**建议**:

- **上传数据 → HTTP POST 批量**。带 `Idempotency-Key`,服务端去重。
  客户端本地攒一个队列,断网就堆着,连上再发——这个逻辑用 HTTP 写十行,
  用 WS 要处理一堆连接状态
- **查询 → HTTP**
- **实时推送 → WS**,但这是第二阶段的事:比如"某人刚发现 Martlock 钢锭
  有个 30% 的价差,推给所有在线成员"。这时候 WS 才有不可替代的价值

先把 HTTP 跑通,WS 用于推送——**这个需求已经明确了,见下一节。**

## 十一之二、实时行情推送

要的效果:价格自己跳,不刷页、不设定时器。**WS 在这里是必需的,不是可选。**

### 先说清楚能到什么程度

数据源是**人在游戏里翻市场**,不是交易所的撮合引擎。所以:

- 热门材料(钢锭、布、木板):几十个成员天天翻,**几秒到几十秒一跳**,体感接近行情
- 冷门物品:可能几小时才动一次

**它是"只要有人看到了,立刻同步给所有人",不是秒级 tick。** 这个预期要跟成员讲清楚,
不然会以为软件坏了。反过来说,公会人越多、翻得越勤,跳得越活——
这本身就是让大家多开客户端的理由。

### 链路

```
成员 A 客户端抓到新挂单
  → HTTP 批量上传
    → 服务端去重(内存 LRU)
      → 写 PG
      → 丢进 broadcast channel
        → conflator 合并 200ms 内的重复
          → 按订阅关系扇出给在线客户端
            → 前端只更新那一个单元格 + 闪一下
```

### 关键设计一:订阅,不要全推

客户端只订阅当前页面关心的东西,不是全服 6 万个组合:

```json
// 客户端 → 服务端
{"op":"sub","keys":["T5_METALBAR|Martlock|1","T5_METALBAR|Lymhurst|1"]}
{"op":"unsub","keys":[...]}
```

- 查价页:订阅这个物品 × 所有城 × 显示中的品质,几十个 key
- 扫描页:订阅榜单上那几十条
- 切页面就换订阅

服务端一个 map 就够:

```go
type Hub struct {
    mu   sync.RWMutex
    subs map[string]map[*Client]struct{}  // "item|city|quality" -> 订阅者
}
```

### 关键设计二:conflation —— 行情系统的核心

**同一个 key 在 200ms 内变了 10 次,只推最后一次。**
不做这件事,热门物品会把客户端淹掉,而且中间那 9 个值用户根本看不见。

```go
type Conflator struct {
    mu    sync.Mutex
    dirty map[string]Quote      // key -> 最新值,旧的直接覆盖
}

func (c *Conflator) Put(q Quote) {
    c.mu.Lock()
    c.dirty[q.Key] = q           // 覆盖,不排队
    c.mu.Unlock()
}

func (c *Conflator) Run(hub *Hub, interval time.Duration) {
    for range time.Tick(interval) {
        c.mu.Lock()
        batch := c.dirty
        c.dirty = make(map[string]Quote, len(batch))
        c.mu.Unlock()
        hub.Fanout(batch)        // 一个 tick 一批,合并发送
    }
}
```

200ms 的 tick 对眼睛来说已经是"实时",但推送量降了一到两个数量级。

### 关键设计三:背压 —— 慢客户端丢旧值,不要阻塞

```go
type Client struct {
    send chan []byte  // 有界,比如 256
}

select {
case c.send <- msg:
default:
    // 满了:丢掉这条,不阻塞扇出循环
    // 行情数据丢旧的没关系,反正下一个 tick 还会推最新值
    metrics.Dropped.Inc()
}
```

**绝对不能让一个网络差的成员卡住整个广播循环。** 行情和聊天不一样,
聊天丢了就没了,行情丢旧的无所谓——只要最新的那个是对的。

### 消息格式:紧凑

```json
{"k":"T5_METALBAR|Martlock|1","s":0,"p":1120,"d":340,"n":12,"t":1758547200}
```

`k` key、`s` side、`p` 价、`d` 深度、`n` 挂单数、`t` 时间戳。
字段名短一个字母,高频推送下省的带宽很可观。真嫌大再换 msgpack,但先别过早优化。

### 断线重连 + 补齐

```js
// 重连后必须重新订阅,并且拉一次快照补上断线期间的变化
ws.onclose = () => setTimeout(connect, backoff())   // 指数退避,封顶 30s
ws.onopen  = () => { subscribe(currentKeys); fetchSnapshot(currentKeys) }
```

**只重订阅不拉快照是最常见的 bug**:断线那 30 秒里变的价格永远补不回来,
界面显示的是旧值却看着"实时"。

### 前端:只改那个单元格

```js
ws.onmessage = e => {
  const q = JSON.parse(e.data)
  const cell = cells.get(q.k + "|" + q.s)
  if (!cell) return
  const dir = q.p > cell.last ? "up" : q.p < cell.last ? "down" : ""
  cell.last = q.p
  cell.el.textContent = fmt(q.p)
  if (dir) {
    cell.el.classList.remove("flash-up", "flash-down")
    void cell.el.offsetWidth          // 强制重排,让动画能重播
    cell.el.classList.add("flash-" + dir)
  }
}
```

```css
@keyframes flash-up {
  from { background: color-mix(in oklab, var(--gain), transparent 65%) }
  to   { background: transparent }
}
.flash-up   { animation: flash-up .6s ease-out }
.flash-down { animation: flash-down .6s ease-out }
@media (prefers-reduced-motion: reduce) { .flash-up,.flash-down { animation: none } }
```

涨绿跌红,跟现有设计语言一致(绿=收益、红=警示)。
`void offsetWidth` 那行是让同一个元素连续跳动时动画能重新播放的标准技巧。

### 服务端多实例:Fanout 抽成接口

单实例 200 人绰绰有余(Go 的 goroutine 撑几千连接不费劲),**现在别上多实例**。
但接口要先留对,不然以后改起来要动一堆地方。

#### 多实例坏在哪

WS 连接是有状态的,谁连在哪个实例是固定的:

```
成员 A ──── 实例 1        成员 B ──── 实例 2
                │                        │
        数据从 A 这边进来                 │
                │                        │
         实例 1 扇出 → A 收到      B 收不到 ✗
```

实例 1 的内存里只有自己那些连接,压根不知道 B 的存在。
**这不是加机器就能解决的,是 WS 天然的粘性。**

#### 接口:Hub 只管本地,Broadcaster 管跨实例

关键是把两件事分开:

- `Hub.Fanout` —— **永远只扇出本实例的连接**,这部分实现从头到尾不变
- `Broadcaster` —— 决定"一批行情怎么到达所有实例的 Hub"

```go
// 把一批行情送到所有该收到的客户端,不管它们连在哪台机器
type Broadcaster interface {
    Publish(ctx context.Context, batch map[string]Quote) error
}

// ── 单实例:直接给本地 Hub ──────────────────────────
type LocalBroadcaster struct{ hub *Hub }

func (b *LocalBroadcaster) Publish(_ context.Context, batch map[string]Quote) error {
    b.hub.Fanout(batch)
    return nil
}

// ── 多实例:发出去,各实例订阅回来再各自 Fanout ──────
type RedisBroadcaster struct {
    rdb *redis.Client
    hub *Hub
}

func (b *RedisBroadcaster) Publish(ctx context.Context, batch map[string]Quote) error {
    payload, err := encodeBatch(batch)
    if err != nil {
        return err
    }
    return b.rdb.Publish(ctx, "quotes", payload).Err()
}

// 后台常驻:收到广播就扇给本实例的连接
func (b *RedisBroadcaster) Run(ctx context.Context) {
    sub := b.rdb.Subscribe(ctx, "quotes")
    defer sub.Close()
    for msg := range sub.Channel() {
        batch, err := decodeBatch(msg.Payload)
        if err != nil {
            log.Warn("bad broadcast payload", "err", err)
            continue
        }
        b.hub.Fanout(batch)
    }
}
```

conflator 那边只认接口:

```go
func (c *Conflator) Run(b Broadcaster, interval time.Duration) {
    for range time.Tick(interval) {
        batch := c.swap()
        if len(batch) > 0 {
            _ = b.Publish(context.Background(), batch)
        }
    }
}
```

**切换只改 main 里一行**:

```go
var bc Broadcaster = &LocalBroadcaster{hub: hub}
// 要多实例时换成:
// rb := &RedisBroadcaster{rdb: rdb, hub: hub}; go rb.Run(ctx); bc = rb
```

#### 发布者自己也会收到 —— 别本地再 Fanout 一次

Redis 和 NATS 都会把消息回显给发布者。所以 `RedisBroadcaster.Publish`
里**只发布,不要顺手调 `hub.Fanout`**,否则本实例的客户端会收到两遍。

统一走"发出去 → 订阅回来 → Fanout"这一条路径,代价是本实例多一个 RTT
(同可用区内 <1ms,对 200ms 的 conflation tick 来说无所谓),
换来的是单实例和多实例**行为完全一致**,少一类只在多实例下才复现的 bug。

#### Redis Pub/Sub vs NATS

| | Redis Pub/Sub | NATS |
|---|---|---|
| 阿里云 | **有托管**(云数据库 Redis 版),开箱即用 | 没有托管,要 ECS/ACK 自建 |
| 投递保证 | fire-and-forget,订阅者断线期间的消息丢掉 | 同左(Core NATS);要可靠得上 JetStream |
| 吞吐 | 单线程,十万级 msg/s | 百万级 |
| 主题通配符 | `psubscribe` 有,但性能差 | **原生且高效**(`quote.T5_METALBAR.*`) |
| 动态订阅成本 | 较高 | 很低,设计上就鼓励细粒度主题 |
| 额外组件 | 多半你已经有 Redis(缓存/session) | 纯新增一个 |
| 集群扇出 | Redis 7 之前会广播到所有节点;7 有 Sharded Pub/Sub | 天然分布式 |

**对这个场景两者都远远够用**——200 人、200ms 合并一次,每秒也就几十条批量消息,
离任何一个的瓶颈都差几个数量级。

所以按"少一个要运维的东西"来选:

- **阿里云上已经有 Redis** → 用 Redis Pub/Sub。托管、监控告警现成,少一套部署
- **没有 Redis,且以后想按物品做细粒度主题** → NATS。主题模型确实更适合行情

顺带一提 ADC 客户端本身就依赖 `nats-io/go-nats`(它往 AODP 的 NATS 传数据),
所以真选 NATS 的话团队不算完全陌生。但那是客户端的事,服务端不必跟着走。

**丢消息在这里是可接受的**:conflation 本来就在丢中间值,
少推一个 tick 的后果是价格晚 200ms 更新,下一个 tick 就自愈了。
这也是为什么不需要 JetStream / Redis Streams 这类持久化方案——
上持久化只会换来延迟和运维成本,解决的却不是问题。

#### 选定 NATS:实现与部署

没有 Redis 的话直接用 NATS,对这个场景它本来就更顺手。

```go
type NatsBroadcaster struct {
    nc  *nats.Conn
    hub *Hub
}

func (b *NatsBroadcaster) Publish(_ context.Context, batch map[string]Quote) error {
    payload, err := encodeBatch(batch)
    if err != nil {
        return err
    }
    // Publish 是异步的(写进本地发送缓冲),对行情来说正合适,
    // 不要每批都 Flush,那会把吞吐打回同步 RTT
    return b.nc.Publish("quotes", payload)
}

func (b *NatsBroadcaster) Run(ctx context.Context) error {
    sub, err := b.nc.Subscribe("quotes", func(m *nats.Msg) {
        batch, err := decodeBatch(m.Data)
        if err != nil {
            log.Warn("bad broadcast payload", "err", err)
            return
        }
        b.hub.Fanout(batch)     // 和 Redis 版一模一样,只有传输层换了
    })
    if err != nil {
        return err
    }
    <-ctx.Done()
    return sub.Unsubscribe()
}
```

连接要配重连,断了不能就此不推:

```go
nc, err := nats.Connect(url,
    nats.MaxReconnects(-1),                  // 无限重连
    nats.ReconnectWait(2*time.Second),
    nats.ReconnectBufSize(8*1024*1024),      // 断线期间的发送缓冲
    nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
        log.Warn("nats disconnected", "err", err)
    }),
)
```

**部署**:单节点就够,一个二进制或一行 docker,几十 MB 内存。

```
docker run -d --name nats -p 4222:4222 -p 8222:8222 nats:latest
```

默认配置直接能用,不需要调优。8222 是监控端点(`/varz`、`/connz`、`/subsz`),
接 Prometheus 正好——**那才是 Prometheus 该干的活**(见第五节)。

要 HA 再上 3 节点集群,但 200 人用不着;NATS 挂了的后果是"价格不自动跳了",
刷新页面还能看到最新数据,不影响上传和入库。

#### 只在服务端内部用,别暴露给客户端

NATS 支持客户端直连(甚至支持 WebSocket 协议),理论上可以让桌面客户端
直接连 NATS,上传和推送一条通道解决,省掉 HTTP 和 WS 两套东西。

**但不建议**,原因不是技术:

| | 客户端 ←HTTP/WS→ 服务端 ←NATS→ 服务端 | 客户端直连 NATS |
|---|---|---|
| CDK 过期控制 | 一个 HTTP 中间件搞定 | 要配 NATS accounts + JWT,过期还要主动踢连接 |
| 限流 / WAF / 访问日志 | 网关现成 | 自己在 NATS 层做 |
| 暴露面 | 只开 443 | 还要开 4222 并做 TLS + 认证 |
| 排查问题 | 看 HTTP 访问日志 | 看 NATS 连接表 |

**NATS 放在服务端内网,不认证、不加密、不暴露端口**——这是它最省心的用法。
客户端那一侧继续走 HTTP(上传)+ WS(推送),两者各自有成熟的运维设施。

#### NATS 解决了哪半,没解决哪半

WS "有状态"其实是两个不同的问题,NATS 只管得了一个:

| 问题 | NATS 管吗 | 靠什么 |
|---|---|---|
| 实例 A 上的客户端收不到实例 B 收到的数据 | ✅ **正是它的活** | NATS |
| **不需要会话粘性**,重连落到哪个实例都行 | ✅ 顺带解决 | NATS |
| 重连后**订阅关系没了** | ❌ | 客户端重连时重新 `sub` |
| 断线那几秒**错过的更新** | ❌ | 重连后拉一次快照 |

前两条是你说的那半,确实成立:**两个容器前面挂个 LB,不用配 sticky session**,
客户端断了随便重连到哪台都能正常工作。这是 WS 在扩容上最大的痛点,NATS 消掉了。

后两条 NATS 帮不上,因为:

- **订阅关系存在实例的内存里**,连接一销毁就跟着没了。实例 2 能收到
  T5_METALBAR 的广播,但它不知道这个刚连上来的客户端要看 T5_METALBAR
- **NATS Core 是 fire-and-forget**,不在线就是错过了,不会补发
  (JetStream 能补,但为这个上 JetStream 不划算——快照接口更简单也更准)

#### 所以客户端重连必须做两件事

```js
let ws, backoff = 1000;
const wanted = new Set();          // 当前页面要看的 key,切页面时维护

function connect() {
  ws = new WebSocket(WS_URL);

  ws.onopen = async () => {
    backoff = 1000;
    // ① 重新订阅 —— 新实例完全不知道你之前看的是什么
    ws.send(JSON.stringify({ op: "sub", keys: [...wanted] }));
    // ② 拉快照 —— 补上断线期间错过的变化
    //    少了这步,界面会停在断线前的旧价上,而且看起来"一切正常"
    const snap = await fetch("/api/quotes?keys=" + [...wanted].join(","));
    applySnapshot(await snap.json());
  };

  ws.onclose = () => {
    setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, 30000);   // 指数退避,封顶 30s
  };
}
```

顺序上**先订阅再拉快照**:反过来的话,两步之间发生的更新会被漏掉
(快照拉完了、订阅还没建立)。先订阅的话最多是同一个值收两次,
而 `Fanout` 里那个时间戳去重(见下面"乱序"一节)本来就会挡掉。

#### LB 上的两个坑

**心跳**。阿里云 SLB/ALB 的空闲超时默认 60 秒,而冷门物品可能几分钟都没有更新推送。
不发心跳的话连接会被 LB 静默掐掉,客户端还以为自己连着:

```go
// 服务端每 30 秒发一个 ping
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if err := conn.WriteControl(websocket.PingMessage, nil,
                                time.Now().Add(10*time.Second)); err != nil {
        return   // 写不进去说明已经断了,走清理流程
    }
}
```

同时把 LB 的空闲超时调到大于心跳间隔(比如 120 秒)。

**Upgrade 支持**。用 ALB(应用型)或者七层 SLB,确认转发规则允许
`Connection: Upgrade`。四层转发也行,但就拿不到 HTTP 层的访问日志了。

#### 附带好处:滚动更新不用停机

有了 NATS + 无粘性之后,发新版本就是普通的滚动更新:

```
实例 1 下线 → 上面的客户端 WS 断开
           → 客户端指数退避重连(1 秒后)
           → LB 分到实例 2
           → 重新订阅 + 拉快照
           → 用户侧看到的就是价格停了一秒,然后继续跳
```

**客户端那段重连逻辑写对了,部署就不用挑时间。** 这比"多实例"本身更实用——
200 人的并发压力根本用不着两个容器,但发版不用等所有人下线这件事天天都有价值。

#### 单主题还是按物品分主题

先用**单主题 `quotes`**,所有实例收全量、各自按本地订阅过滤。
200 人量级下每秒几十条批量消息,过滤成本可以忽略。

什么时候该换成 `quote.{item_id}` 分主题:

- 实例数 ≥ 4,**且**跨实例带宽开始显眼
- 或者单实例订阅的物品集合只占全量的一小部分(说明收了大量用不上的)

真到那天,NATS 的通配符订阅比 Redis 的 `psubscribe` 舒服得多,
这也是上面那张表里唯一实质性的差别。

#### 一个必须处理的坑:乱序

多个实例同时往外发,**跨发布者的消息顺序没有保证**。
T5_METALBAR 的两次更新,晚发生的那条可能先到,界面就会停在旧值上。

解法很简单,在 `Hub.Fanout` 里按时间戳丢弃过期值:

```go
func (h *Hub) Fanout(batch map[string]Quote) {
    for key, q := range batch {
        if last, ok := h.lastSeen[key]; ok && q.At.Before(last) {
            continue      // 这条比手上的还旧,扔掉
        }
        h.lastSeen[key] = q.At
        h.send(key, q)
    }
}
```

这层在单实例下也不亏(能挡住重试导致的重复),所以**从一开始就写上**,
别等上了多实例才发现界面偶尔卡在旧价上——这种 bug 极难复现。

## 十二、前端选型

Wails 不限制框架(它只要 `frontend/dist` 里的静态文件)。
按"生态大、用的人多、扛得住数据量"排,当前最主流的组合:

| 层 | 选择 | 理由 |
|---|---|---|
| 框架 | **React 19** | 生态和人才储备最大,遇到问题搜得到 |
| 组件 | **shadcn/ui** | 现在事实标准。代码直接进仓库,不是黑盒依赖,想改就改 |
| 样式 | **Tailwind CSS 4** | 和 shadcn 配套 |
| 表格 | **TanStack Table + Virtual** | 几千行挂单必须虚拟滚动,不然 webview 里必卡 |
| 图表 | **ECharts** | K 线、深度图、趋势线都有现成的,中文文档全 |

**但别指望组件库让界面变好看。** shadcn 给的是按钮、下拉、对话框这些基础件,
而这个工具的主体是**表格、订单簿、折线**——那部分还是得自己设计。
现在这套 web 界面的设计语言(银灰冷底、青铜强调、城市六色、数据越旧越褪色、
品质色跟游戏一致)可以直接搬,省掉从零定调的功夫。

性能上有个坑要先知道:React 的虚拟 DOM 在几千行表格上会掉帧,
**虚拟滚动不是可选项是必需项**。Svelte 5 在这方面天生好一些
(编译时框架,无虚拟 DOM),如果团队只有你自己写,Svelte 的性价比更高。
要生态就 React,要性能就 Svelte——两条路都能做出好看的东西。


---

# 十三、客户端自动更新

## 主流做法长什么样

不管是 VS Code、Chrome 还是各种桌面工具,骨架都是这四步:

```
启动(或定时)→ 问服务端"最新是哪个版本" → 比较 → 需要就下载、校验、替换、重启
```

差别只在细节:文件放哪、怎么校验、强制还是提示、灰度怎么做。

## 推荐架构:manifest 接口 + OSS/CDN,不要让文件走你的应用服务器

你说的"提供一个接口下载 exe"——**接口要提供,但别提供文件本身**。

```
        ┌─ GET /api/client/version ──→ 你的 Go 服务端(带 CDK 鉴权)
客户端 ─┤                               返回 JSON manifest
        └─ GET https://cdn.../x.exe ──→ 阿里云 OSS + CDN
```

为什么分开:一次发版 200 人各下 30 MB = 6 GB。走应用服务器的话带宽直接打满,
连正常的行情推送都会受影响。**OSS + CDN 的流量费在这个量级几乎可以忽略**,
而且天然支持断点续传和就近节点。

### manifest 长这样

```json
{
  "latest":        "1.4.2",
  "min_supported": "1.3.0",
  "url":           "https://cdn.example.com/client/1.4.2/flipper-windows-amd64.exe",
  "sha256":        "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "size":          24117248,
  "mandatory":     false,
  "notes":         "修复过期邮件解析;新增仓库总览"
}
```

`min_supported` 是关键:游戏更新导致 opcode 漂移时,老客户端解出来的数据是错的,
这时候把它一提,所有低于这个版本的客户端**必须更新才能继续用**。
这和第八节 `/session` 里的 `server_min_version` 是同一个东西,
放一个接口里返回也行。

## Windows 的核心难点:不能覆盖正在运行的 exe

这是所有桌面自更新绕不开的一关。两种主流解法:

**改名法**(推荐,`minio/selfupdate` 就是这么干的):

```
1. 把 flipper.exe 改名成 flipper.exe.old   ← Windows 允许重命名运行中的文件
2. 把新版本写到 flipper.exe
3. 提示用户重启(或者自己 exec 新进程后退出)
4. 下次启动时删掉 .old
```

**辅助进程法**:主程序退出前拉起一个 `updater.exe`,由它替换再重启主程序。
更可控但要多维护一个二进制,而且杀毒软件对"一个程序启动另一个程序去改写 exe"
这种行为更敏感。

**fork ADC 的话这块是白捡的**——它已经在用 `minio/selfupdate`,
你只要把"去 GitHub 查 release"换成"查你的 manifest 接口":

```go
// 原来:go-githubupdate 拼 GitHub API + 匹配 update-windows-amd64.exe.gz
// 改成:
resp, _ := httpClient.Get(apiBase + "/api/client/version")
var m Manifest
json.NewDecoder(resp.Body).Decode(&m)

if semver.Compare(m.Latest, currentVersion) > 0 {
    bin, _ := download(m.URL)                    // 支持 Range 断点续传
    if sha256Hex(bin) != m.SHA256 {              // 必须校验
        return errChecksum
    }
    selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{})
}
```

`selfupdate.Apply` 内部就做了改名那一套,失败还会自动回滚。

## 代码签名:多数情况下不需要,先搞清楚两种弹窗

**先分清 UAC 和 SmartScreen,这是两回事:**

| | UAC(用户账户控制) | SmartScreen(应用信誉) |
|---|---|---|
| 长什么样 | "是否允许此应用对你的设备进行更改?" | "Windows 已保护你的电脑 / 未知发布者" |
| 为什么弹 | 程序要管理员权限 | 文件带下载标记且信誉不足 |
| 抓包工具会不会弹 | **一定会**,Npcap 需要管理员权限 | **不一定** |
| 签名能消除吗 | **不能**,只是把"未知发布者"换成你的名字 | 能(EV 证书) |

你双击 ADC 只弹 UAC 就正常运行了——**这是对的,而且我们的软件完全可以一样**。
实证:ADC 的构建脚本里**没有任何签名步骤**(`buildall_main.sh`、`scripts/`、
`docs/build-and-release.md` 里搜不到 signtool / osslsigncode),
`winres.json` 里也是 `execution-level: as invoker`。它就是个没签名的裸二进制。

### SmartScreen 到底什么时候才弹

条件要同时满足:

1. 文件带 **Mark of the Web**(MOTW)——只有**浏览器等来源下载**的文件才会被打这个标记
2. 且该文件或签名者信誉不足

所以:

- 用 7-Zip 之类解压出来的 exe → 多数情况**不传播 MOTW** → 不弹
- Windows 资源管理器解压 zip → 会传播 MOTW → 可能弹
- 直接下载单个 exe 运行 → 最容易弹

### 自动更新不会触发 SmartScreen

**这条我上一版写错了,更正:** `minio/selfupdate` 是**程序自己写文件**,
不经过浏览器,写出来的 exe **不带 MOTW**,所以自更新完重启**不会**弹 SmartScreen。

也就是说整个生命周期里,最多只有**第一次手动安装**那一下可能遇到,
之后几十次自动更新全都静默完成。

### 首次安装怎么免费规避

不用买证书,三选一:

- 发 **zip 包**并让成员用 7-Zip 解压(而不是双击 zip 用资源管理器解)
- 弹了就右键 exe → 属性 → 勾**解除锁定**
- 或者群里发一行:`Unblock-File .\flipper.exe`

对 200 人的内部工具,这个成本远低于一年三四千的 EV 证书。

### 真正值得担心的不是 SmartScreen,是杀软误报

**"抓网络包 + 自己改写自己的 exe"这个组合,行为特征上跟木马高度相似。**
国内的 360、火绒、电脑管家有概率直接拦截或删除,这跟有没有 MOTW 无关。

签名能降低误报率,但不保证。更实际的做法:

1. 发版后把二进制提交到各家的**误报申诉/白名单通道**(都是免费的,微软、
   火绒、360 都有提交入口),一般一两天生效
2. 安装说明里写清楚"如果被拦截,把安装目录加进杀软白名单"
3. **别把 exe 放在 `%TEMP%` 或下载目录运行**,固定装到
   `%LOCALAPPDATA%\AlbionFlipper\` 这种正经位置,可疑度低很多

真被大面积误报了再考虑买证书,别一开始就花这个钱。

## 为什么 Albion 的更新看起来那么简单

因为**它更新的不是启动器自己,是游戏本体**。

```
Albion Online Launcher.exe   ← 常驻,负责检查/下载/替换,自己极少更新
        │ 下载并替换
        ↓
Albion-Online.exe + 几 GB 资源文件   ← 被更新的对象,更新时它没在运行
```

一个程序去改另一个没在运行的程序的文件——**"不能覆盖运行中的 exe"这个难点
根本不存在**。它进度条走完、"关闭又打开",实际上是启动器换完文件后拉起了游戏进程。

我们想做的是"**单个 exe 自己更新自己**",那才是难点所在。
所以不是我们做得难,是两件事本来就不一样。

Albion 还得解决另外两个我们没有的问题:几 GB 资源要**增量下载**(只传变化的块)、
文件损坏要**校验修复**。这些才是它需要一个独立启动器的真正原因。
我们的二进制就 30 MB,全量替换反而更简单。

### 两条路都能给到一样的体验

| | 单 exe 自更新 | 启动器 + 主程序 |
|---|---|---|
| 文件数 | 1 个 | 2 个 |
| 覆盖运行中的 exe | 靠改名法绕开 | **不存在这个问题** |
| 进度条 / 速率 | 能做 | 能做 |
| 自动重启 | 能做 | 天然 |
| 杀软观感 | 自己改写自己,稍敏感 | 启动器改另一个文件,常见得多 |
| 实现量 | 约 50 行 | 多一个二进制要维护和发布 |

**建议先做单 exe 自更新**——ADC 的基础设施现成,50 行就能做出你说的那个体验。
真被杀软误报困扰了,再拆成启动器架构不迟。

### 「两个 exe 互相更新」行不行

时序上**是成立的**,因为两者从不同时运行:

```
双击 launcher.exe
  → launcher 查更新 → 替换 main.exe(此时 main 没在跑,安全)
  → 启动 main.exe → launcher 退出
      → main 运行中 → 替换 launcher.exe(此时 launcher 已退出,安全)
```

但它换来一个新问题:**launcher 是单点,坏了就全卡死。**

| | 单 exe 自更新 | 两个 exe 互相更新 |
|---|---|---|
| 更新失败怎么办 | `selfupdate` 自动回滚,`.old` 还在,**下次启动照常跑** | launcher 被写坏 → **双击没反应,只能让成员重新下载** |
| 要维护几个二进制 | 1 | 2,还要管版本兼容矩阵 |
| 用户绕过 | 不可能 | 会有人给 main.exe 建快捷方式,从此再不走 launcher,主程序永远不更新 |
| 解决的问题 | —— | 覆盖运行中的 exe(而这个 `selfupdate` 已经解决了) |

最后一行是关键:**它解决的是一个已经被解决的问题。**
改名法在 NTFS 上是可靠的(Windows 允许重命名运行中的文件),ADC 用了很多年。

### 如果还是想要两个 exe,把 launcher 做成"不需要更新"

真正稳的形态不是互相更新,而是**让 launcher 简单到永远不用改**:

```
launcher.exe   ~500 KB,一辈子只干四件事:
               查 manifest → 下载 → 替换 main.exe → 启动它
               不带 UI 框架、不连 WS、不碰数据库

main.exe       所有功能和迭代都在这里
```

launcher 的逻辑几个月都不会变,也就没有"谁来更新 launcher"这个问题。
真要改(比如 manifest 地址变了),就发一次全量安装包让大家重装一次。

这样既避开了覆盖运行中的 exe,又没有单点故障——**代价只是多一个几百 KB 的二进制**。

### 已定:先做单 exe(2026-09-22)

`selfupdate` 的改名法够用,失败能回滚,实现量 50 行。
**不拆 launcher,也不做互相更新。** 等真遇到下面任一情况再重新考虑:

- 杀软对"自己改写自己"误报严重,加白名单也压不住
- 主程序膨胀到需要增量更新(几百 MB 以上)
- 更新失败率在实际使用中显著偏高

拆的时候也不亏:manifest 接口、下载逻辑、校验、进度条全都能原样搬进 launcher。

### 进度条 + 自动重启的完整实现

体验上的三件事(进度、速率、自动重启)都不难,难的是我上一版没讲实现让它显得难:

```go
type progressReader struct {
    r          io.Reader
    done, total int64
    start      time.Time
    emit       func(done, total int64, bytesPerSec float64)
    lastEmit   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
    n, err := p.r.Read(b)
    p.done += int64(n)
    // 每 100ms 往前端推一次,别每个 chunk 都推
    if time.Since(p.lastEmit) > 100*time.Millisecond {
        p.lastEmit = time.Now()
        elapsed := time.Since(p.start).Seconds()
        p.emit(p.done, p.total, float64(p.done)/elapsed)
    }
    return n, err
}

func Update(m Manifest, emit func(int64, int64, float64)) error {
    resp, err := http.Get(m.URL)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    pr := &progressReader{
        r: resp.Body, total: resp.ContentLength,
        start: time.Now(), emit: emit,
    }

    // 边下边算 sha256,不用先落盘再读一遍
    h := sha256.New()
    buf := &bytes.Buffer{}
    if _, err := io.Copy(io.MultiWriter(buf, h), pr); err != nil {
        return err
    }
    if hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
        return errors.New("校验失败")
    }

    // selfupdate 内部就是改名法:当前 exe 改成 .old,新内容写到原路径
    if err := selfupdate.Apply(buf, selfupdate.Options{}); err != nil {
        return err
    }
    return restart()
}

func restart() error {
    exe, err := os.Executable()
    if err != nil {
        return err
    }
    cmd := exec.Command(exe, os.Args[1:]...)
    cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
    if err := cmd.Start(); err != nil {
        return err
    }
    os.Exit(0)   // 旧进程退出,.old 文件下次启动时清理
    return nil
}
```

Wails 侧把 `emit` 接到事件上,前端就有进度条和速率了:

```go
app.EmitEvent("update:progress", map[string]any{
    "done": done, "total": total, "speed": bytesPerSec,
})
```

```svelte
<!-- 前端 -->
{#if updating}
  <progress value={done} max={total}></progress>
  <span>{fmtSize(done)} / {fmtSize(total)} · {fmtSpeed(speed)}</span>
{/if}
```

**"界面关了又开"那个效果,就是 `restart()` 里 `cmd.Start()` + `os.Exit(0)`。**
用户看到的就是窗口消失、新窗口出现,和 Albion 启动器一模一样。

### 一个 Windows 上的细节

新进程起来的瞬间,旧进程可能还没完全退出。**改名法天然避开了这个冲突**——
新 exe 写在原路径,旧的已经改名成 `.old`,两个文件不是同一个,互不锁定。

启动时顺手清理一下上次的残留:

```go
func init() {
    if exe, err := os.Executable(); err == nil {
        _ = os.Remove(exe + ".old")   // 失败也无所谓,下次再试
    }
}
```

## 版本策略

```
mandatory=true 或 currentVersion < min_supported
    → 不给跳过,更新完才能用
否则
    → 提示"有新版本",用户可以本次跳过
```

**灰度**:manifest 里加个 `rollout`,服务端按 `device_id` 的 hash 决定返不返新版本:

```go
if hashPercent(deviceID) >= m.Rollout {   // rollout=20 就是只给 20% 的人
    m.Latest = m.PreviousStable
    m.URL    = m.PreviousURL
}
```

先放 10% 的人,盯一小时错误率没问题再放全量。200 人规模下这个动作很轻,
但能挡住"一次发版把所有人搞挂"。

**回滚**就是把 manifest 里的 `latest` 改回旧版本号和旧 URL。
所以**旧版本的文件不要从 OSS 删掉**,至少留最近三个版本。

## 别忘了服务端也要版本协商

客户端更新是异步的(有人没开软件就不会更新),所以服务端不能假设大家版本一致:

- API 带 `X-Client-Version` 头
- 服务端对低于 `min_supported` 的请求返回 426 Upgrade Required
- 客户端收到 426 就强制走更新流程

这样即使有人绕过了启动检查,也没法用老版本传脏数据进来。


---

# 十四、安装:能傻瓜到什么程度

先澄清上一节一个容易误读的地方:**"提交白名单"和"装到 %LOCALAPPDATA%"
这两件事都是我们做的,成员全程不知道。**

| 事 | 谁做 | 成员感知 |
|---|---|---|
| 提交杀软误报白名单 | **我们**,发版后提交给各厂商 | 无 |
| 装到 `%LOCALAPPDATA%` | **安装包**自动决定 | 无 |
| 装 Npcap | **成员**,一次性 | 有,下一步下一步 |
| 万一还是被杀软拦了 | 成员点放行 | 有,但是兜底不是常规 |

**真正需要成员动手的只有装 Npcap 那一次。**

## Npcap 不能捆绑,这是许可限制

免费版 Npcap 明确**不允许外部分发**(只能在 5 台系统内自用,
或者仅配合 Nmap/Wireshark 使用时不限台数)。要捆进安装包必须买
OEM Redistribution License。

Nmap 官方给开源软件的建议就是:**别捆绑,检测缺失并给链接**。
ADC 就是这么做的,而且做得挺完整——fork 之后这块白捡:

1. **安装时**:NSIS 脚本 `.onInit` 读注册表 `HKLM\SOFTWARE\Npcap`
   (带 `WOW6432Node` 回退),没有就弹窗问"是否打开下载页",
   不拦安装,只警告
2. **每次启动**:`internal/pcapdriver` 再查一遍。这个才是关键——
   安装时的检查只能覆盖新装用户,老用户换了系统、卸了 Npcap 都靠它

`decide()` 分四种状态给不同文案:Npcap 正常 / Npcap 装成了 admin-only 但进程没提权 /
只有过时的 WinPcap / 什么都没有。

## 能不能替成员装 Npcap:两个限制

| 限制 | 免费版 | OEM 版 |
|---|---|---|
| 打进我们的安装包分发 | ❌ 禁止(5 台内自用) | ✅ 无限制 |
| 静默安装 `/S` | ❌ **没有这个功能** | ✅ 有 |

官方原话:"only Npcap OEM provides a silent installer for hands-off installation",
免费版 "only allows five installs"。价格没公开,要询价。

所以**做不到完全无感**:文件不能由我们分发,向导也没法跳过。
但中间那些琐碎步骤全都能替成员省掉。

### 能做到的最好形态

```
客户端检测到没装 Npcap
  → 在我们自己的界面里下载官方安装器(带进度条,校验 SHA256)
  → 自动启动它
  → 界面上同步显示图文引导:"在弹出的窗口点下一步;
     第 3 步那个 Restrict to Administrators 不要勾"(配截图箭头)
  → 后台每 2 秒查一次注册表,装好了自动进入下一步
```

成员要做的只剩"在 Npcap 向导里点四五下",不用找下载页、不用选版本、
不用翻下载目录、不用自己判断装完没有。

### 三个实现要点

**下载地址从我们的 manifest 里给,文件从官方域名下载。**

```json
{
  "npcap": {
    "version": "1.83",
    "url":     "https://npcap.com/dist/npcap-1.83.exe",
    "sha256":  "...",
    "guide":   "https://your-server/help/npcap"
  }
}
```

版本由我们控制(只推验证过的),但 URL 必须指向 `npcap.com`——
**放到自己的 CDN 上就构成分发了**,那是要买 license 的行为。
顺带也要校验 SHA256,防的是 DNS 劫持。

**检测完成直接复用 ADC 现成的函数。**

```go
for {
    if present, _ := npcapState(); present {   // internal/pcapdriver 已有
        break
    }
    select {
    case <-time.After(2 * time.Second):
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

**引导里最重要的一句是"别勾 admin-only"。**
勾了的话客户端从此每次启动都要 UAC。这一步值得在界面上配张标了箭头的截图,
比纯文字有效得多。

### 要不要买 OEM license

买了能做到:安装包里捆绑 + `/S` 静默装完,成员**完全无感**。

判断标准很简单:**先按上面的做,看实际有多少人卡在这一步。**
200 人里如果只有零星几个装不上,群里手把手一下就过了;
如果成规模地卡住,再去询价不迟——那时候你手里有具体数据,
谈价和说服自己都更容易。

## UAC 弹窗其实可以消除

你说"双击只弹管理员权限",那是因为 Npcap 装成了 **admin-only 模式**。
ADC 的引导文案里写了这件事:

> 安装 Npcap 时,建议**不要勾选** "Restrict Npcap driver's access to
> Administrators only" —— 这样客户端不用以管理员身份运行就能抓包。
> 代价是系统上其他程序也能在无管理员权限下读网络流量。

所以只要在成员安装 Npcap 时引导他们别勾那个框,**之后双击就跑,一次 UAC 都不弹**。

这个权衡要讲明白再让人选,但对个人游戏机来说,不勾是常规选择。

## 改一处:装到用户目录,不要 Program Files

ADC 的 NSIS 脚本装在 `$PROGRAMFILES64`,**这一点我们要改**。

| | Program Files | `%LOCALAPPDATA%` |
|---|---|---|
| 安装 | 需要管理员权限,弹 UAC | **不需要**,静默装完 |
| 自更新写文件 | **需要提权**,可能失败 | 直接写,无障碍 |
| 杀软观感 | 正常 | 正常(比 `%TEMP%` 好得多) |
| 多用户共享 | 可以 | 每个用户各一份 |

自更新是天天要跑的,装在 Program Files 意味着每次更新都可能因为权限失败。
改成 per-user 安装:

```nsis
!define MULTIUSER_INSTALLMODE_DEFAULT_CURRENTUSER
InstallDir "$LOCALAPPDATA\AlbionFlipper"
RequestExecutionLevel user      ; 安装时不要 UAC
```

## 最终的成员视角

```
1. 群里发链接 → 下载 setup.exe
2. 双击 → 不弹 UAC → 自动装到用户目录 → 建桌面快捷方式
3. 安装程序发现没装 Npcap → 弹窗"点是打开下载页"
   (文案里写清楚:安装时别勾 admin-only 那个框)
4. 装完 Npcap,双击桌面图标 → 直接跑起来,不弹 UAC
5. 输入 CDK → 激活 → 用
6. 之后每次更新全程无感:启动时检查 → 下载(有进度条)→ 自动重启
```

**整个流程里成员要做的判断只有两个:装 Npcap 时别勾那个框、输一次 CDK。**
这两步在群里发张带箭头的截图就能说清楚。

真被杀软拦了才需要加白名单,那是兜底——而且我们提前提交过误报申诉的话,
多数成员根本碰不到。
