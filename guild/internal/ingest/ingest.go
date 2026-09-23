// Package ingest 接收客户端上传,去重后落库并标记脏盘口。
package ingest

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"albion-guild/internal/model"
	"albion-guild/internal/store"
	"albion-guild/internal/world"
)

// DirtyMarker 告诉行情层"这个盘口变了,该重算最优价了"。
// 用接口是为了让 ingest 不依赖 hub,两边可以独立测试。
type DirtyMarker interface {
	MarkDirty(model.QuoteKey)
}

// orderState 是一张挂单的最后已知状态。
//
// 游戏里一张单的物品和城市不会变(撤单再挂是新的 order_id),但**上报方
// 可能报错**:多开时客户端只有一个"当前位置",谁最后切区,两边的挂单就都
// 记成谁的城市。所以除了价格和数量,还要存一份身份指纹 ident
// (物品|城市|品质|方向的哈希)。以前只比价格和数量,错归城市的单一旦进了
// LRU,之后正确城市的观测只会被当成"没变"去续命,错的那条永远改不回来。
type orderState struct {
	price  int64
	amount int32
	ident  uint64
}

// Conflict 是最近一次"同一个 order_id 被报到了别的盘口"的现场。
// 只记新的那次观测;旧的那次在 market_order_event 里按 order_id 能查到
type Conflict struct {
	OrderID       int64      `json:"order_id"`
	ItemID        string     `json:"item_id"`
	LocationID    string     `json:"location_id"`
	RawLocationID string     `json:"raw_location_id"`
	Quality       int16      `json:"quality"`
	Side          model.Side `json:"side"`
	Reporter      string     `json:"reporter"`
	At            time.Time  `json:"at"`
}

// Stats 是入库口径的几项计数,/api/coverage 原样输出。
type Stats struct {
	// LocationConflicts 是同一个 order_id 指纹变了的次数。指纹里还有物品、
	// 品质、方向,但这几项游戏里不会变,实际几乎全是多开串城。
	// 这是服务端唯一看得见多开污染的信号
	LocationConflicts int64 `json:"location_conflicts"`
	// ClampedFuture 是纠偏之后仍在未来、被钳到服务端 now 的观测条数。
	// 持续增长说明有老客户端(不带 sent_at)的钟是快的
	ClampedFuture int64     `json:"clamped_future"`
	LastConflict  *Conflict `json:"last_conflict,omitempty"`
}

type Ingestor struct {
	store *store.Store
	dirty DirtyMarker
	seen  *lru.Cache[int64, orderState]

	// now 为 nil 时用 time.Now。留这个口子是为了测试能钉住时间;
	// 现有测试直接写 &Ingestor{seen, dirty},不受影响
	now func() time.Time

	conflicts, clamped atomic.Int64

	mu sync.Mutex
	// pending 按上报人分组。**不能只记一个 reporter**:两次 flush 之间
	// 会有好几个成员提交,后来者会把整批待写订单都算到自己头上,
	// 库里 reporter 那列就全是最后一个上传的人
	pending map[string][]model.MarketOrder // 状态变了的,要写两张表
	// touches 是状态没变的,只刷 last_seen。存每张单这段时间里最新的观测时间
	// 而不是 flush 时刻:last_seen 要和 WriteOrders 用同一套(已纠偏的)客户端
	// 观测时钟,"同一眼"的判断才有意义
	touches      map[int64]time.Time
	lastConflict *Conflict
	lastWarn     time.Time // 串城告警限频:每分钟最多一条,免得把日志刷满

	flushSize int
	flushWait time.Duration
}

func New(st *store.Store, dirty DirtyMarker, cacheSize int) (*Ingestor, error) {
	c, err := lru.New[int64, orderState](cacheSize)
	if err != nil {
		return nil, err
	}
	return &Ingestor{
		store:     st,
		dirty:     dirty,
		seen:      c,
		pending:   map[string][]model.MarketOrder{},
		flushSize: 500,
		flushWait: 2 * time.Second,
	}, nil
}

// Submit 处理一批上传。
//
// 这里是整个系统写入放大的闸门:200 人同时翻市场,同一张挂单会被看到几十次,
// 不挡掉的话库里全是"什么都没发生"的重复行。
func (i *Ingestor) Submit(batch model.UploadBatch) (changed, touched int) {
	now := i.clock()
	// 纠偏只动这一批自己的数据,放在锁外
	if _, n := Normalize(&batch, now); n > 0 {
		i.clamped.Add(int64(n))
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pending == nil {
		i.pending = map[string][]model.MarketOrder{}
	}
	if i.touches == nil {
		i.touches = map[int64]time.Time{}
	}
	for _, o := range batch.Orders {
		// 先收敛地点再做别的:盘口键、落库、推送全都要用城市名,
		// 否则抓包数据和 AODP 永远是两个 key(0007 vs Thetford)
		if o.RawLocationID == "" {
			o.RawLocationID = o.LocationID
		}
		o.LocationID = world.City(o.LocationID)
		o.ItemID = canonicalItemID(o.ItemID, o.Enchant)
		ident := identOf(o)

		prev, ok := i.seen.Get(o.OrderID)
		if ok && prev.price == o.UnitPrice && prev.amount == o.Amount && prev.ident == ident {
			if o.ObservedAt.After(i.touches[o.OrderID]) {
				i.touches[o.OrderID] = o.ObservedAt
			}
			touched++
			continue
		}
		if ok && prev.ident != ident {
			// 按"变了"处理:upsert 会连 location_id、reporter 一起刷新,
			// 错归的单才能被后来的观测改写。至于哪次才是对的,服务端判断不了,
			// 只能计数暴露出来,根治要靠客户端按连接分流
			i.noteConflict(o, batch.Reporter, now)
		}
		i.seen.Add(o.OrderID, orderState{price: o.UnitPrice, amount: o.Amount, ident: ident})
		i.pending[batch.Reporter] = append(i.pending[batch.Reporter], o)
		i.dirty.MarkDirty(o.Key())
		changed++
	}
	return changed, touched
}

// noteConflict 记一次身份冲突。调用方持有 mu。
func (i *Ingestor) noteConflict(o model.MarketOrder, reporter string, now time.Time) {
	n := i.conflicts.Add(1)
	i.lastConflict = &Conflict{
		OrderID: o.OrderID, ItemID: o.ItemID, LocationID: o.LocationID,
		RawLocationID: o.RawLocationID, Quality: o.Quality, Side: o.Side,
		Reporter: reporter, At: o.ObservedAt,
	}
	if now.Sub(i.lastWarn) >= time.Minute {
		i.lastWarn = now
		slog.Warn("同一张挂单被报到了别的盘口,多半是多开串城",
			"order_id", o.OrderID, "item", o.ItemID, "location", o.LocationID,
			"raw_location", o.RawLocationID, "reporter", reporter, "total", n)
	}
}

// Stats 返回入库口径的计数快照。
func (i *Ingestor) Stats() Stats {
	s := Stats{
		LocationConflicts: i.conflicts.Load(),
		ClampedFuture:     i.clamped.Load(),
	}
	i.mu.Lock()
	if i.lastConflict != nil {
		c := *i.lastConflict
		s.LastConflict = &c
	}
	i.mu.Unlock()
	return s
}

func (i *Ingestor) clock() time.Time {
	if i.now != nil {
		return i.now()
	}
	return time.Now()
}

// PendingCount 是待写订单总数(各上报人加起来)。
func (i *Ingestor) PendingCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := 0
	for _, orders := range i.pending {
		n += len(orders)
	}
	return n
}

// Run 定时把攒下的批次刷进库。
func (i *Ingestor) Run(ctx context.Context) {
	t := time.NewTicker(i.flushWait)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			i.flush(context.WithoutCancel(ctx)) // 退出前把手上的写完
			return
		case <-t.C:
			i.flush(ctx)
		}
	}
}

func (i *Ingestor) flush(ctx context.Context) {
	i.mu.Lock()
	byReporter, touches := i.pending, i.touches
	i.pending, i.touches = map[string][]model.MarketOrder{}, nil
	i.mu.Unlock()

	// 一个上报人一次写入。两次 flush 之间通常只有几个人在传,
	// 分组的代价远小于把归属记错的代价
	for reporter, orders := range byReporter {
		if len(orders) == 0 {
			continue
		}
		if err := i.store.WriteOrders(ctx, reporter, orders); err != nil {
			slog.Error("写入挂单失败", "reporter", reporter, "count", len(orders), "err", err)
			// 这里应该进重试队列而不是丢掉,第一版先记日志
		}
	}
	// 顺序不能反:同一张单可能这一轮先新写入、又被 touch 过,
	// 先 WriteOrders 行才存在,TouchOrders 才有东西可刷
	if len(touches) > 0 {
		if err := i.store.TouchOrders(ctx, touches); err != nil {
			slog.Error("刷新 last_seen 失败", "count", len(touches), "err", err)
		}
	}
}
