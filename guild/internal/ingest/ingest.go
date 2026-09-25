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

// sink 是 flush 落库那一步,线上就是 *store.Store。抽成接口是为了测试能断言
// "一轮 flush 只提交一次",以及写失败之后 LRU 的善后
type sink interface {
	Flush(ctx context.Context, byReporter map[string][]model.MarketOrder, touches map[int64]time.Time) error
}

// orderState 是一张挂单的最后已知状态。
//
// 游戏里一张单的物品和城市不会变(撤单再挂是新的 order_id),但**上报方
// 可能报错**:多开时客户端只有一个"当前位置",谁最后切区,两边的挂单就都
// 记成谁的城市。所以除了价格和数量,还要存一份身份指纹 ident
// (物品|城市|品质|方向的哈希)。以前只比价格和数量,错归城市的单一旦进了
// LRU,之后正确城市的观测只会被当成"没变"去续命,错的那条永远改不回来。
//
// seen 是这个状态最近一次被看到的观测时间(unix 纳秒,50 万条多 4MB)。
// 比它还旧的观测是乱序晚到的(断网重传、两个成员先后上传同一眼),
// 状态再不一样也不能拿来改写:它说的是过去,不是现在
type orderState struct {
	price  int64
	amount int32
	ident  uint64
	seen   int64
}

// conflictKeep 是盘口冲突记录留多久。扫描窗口默认 6h,留 24h 足够
const conflictKeep = 24 * time.Hour

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
	ClampedFuture int64 `json:"clamped_future"`
	// StaleObservations 是比已知状态更旧、直接丢掉的观测条数。断网重传、
	// 两个成员先后上传同一眼都会有;涨得飞快说明有客户端在反复重传
	StaleObservations int64 `json:"stale_observations"`
	// LegacyBatches 是不带 sent_at 的批次数,即还在跑老客户端的上传。
	// 老客户端的观测时间纠不了偏(只能钳未来):钟慢的成员的单会提前过期,
	// 慢得多了连实时变化都会被当成旧观测挡掉。这个数不再涨之前,
	// 新鲜度口径对这些成员不准
	LegacyBatches int64     `json:"legacy_batches"`
	LastConflict  *Conflict `json:"last_conflict,omitempty"`
}

type Ingestor struct {
	store sink
	dirty DirtyMarker
	seen  *lru.Cache[int64, orderState]

	// now 为 nil 时用 time.Now。留这个口子是为了测试能钉住时间;
	// 现有测试直接写 &Ingestor{seen, dirty},不受影响
	now func() time.Time

	conflicts, clamped, stale, legacy atomic.Int64

	mu sync.Mutex
	// pending 按上报人分组。**不能只记一个 reporter**:两次 flush 之间
	// 会有好几个成员提交,后来者会把整批待写订单都算到自己头上,
	// 库里 reporter 那列就全是最后一个上传的人
	pending map[string][]model.MarketOrder // 状态变了的,要写两张表
	// touches 是状态没变的,只刷 last_seen。存每张单这段时间里最新的观测时间
	// 而不是 flush 时刻:last_seen 要和 WriteOrders 用同一套(已纠偏的)客户端
	// 观测时钟,"同一眼"的判断才有意义
	touches map[int64]time.Time
	// dirtyKeys 是这一轮要标脏的盘口:状态变了的单和只刷 last_seen 的单所在的盘口。
	// **flush 提交之后才标**(见 flush)。以前在 Submit 里当场标,conflator 每 200ms
	// 从库里读最优价,而单子要等 2s 一次的 flush 才落库:读到的是旧状态,脏标却已经
	// 清掉了,flush 之后也没人再标一次。结果是每条推送都晚一拍,一张单只被看到
	// 一次的话,它的状态永远推不出去。
	//
	// 只 touch 的盘口也要标:这一眼里没再出现的旧最优单会被读簿当成幽灵剔掉,
	// 最优价可能已经退到次优档;"最近一眼"的时间也往前走了
	dirtyKeys    map[model.QuoteKey]struct{}
	lastConflict *Conflict
	lastWarn     time.Time // 串城告警限频:每分钟最多一条,免得把日志刷满
	// conflictAt 是每个盘口最近一次卷进身份冲突的观测时间,按盘口指纹
	// (identOf)索引。串城的单新旧两个盘口都记:服务端分不清哪边是对的,
	// 两边的"最近一眼"都可能被错归单搅过。用指纹而不是 QuoteKey 做键,
	// 是因为 LRU 里只存了旧盘口的指纹,拿不到它的字符串
	conflictAt map[uint64]time.Time
	lastPrune  time.Time

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
	if batch.SentAt.IsZero() && len(batch.Orders) > 0 {
		i.legacy.Add(1)
	}
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
	if i.dirtyKeys == nil {
		i.dirtyKeys = map[model.QuoteKey]struct{}{}
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
		at := o.ObservedAt.UnixNano()

		prev, ok := i.seen.Get(o.OrderID)
		if ok && at < prev.seen {
			// 乱序晚到的旧观测:不改 LRU、不进待写、不标脏。以前它状态不同就算
			// "变了",会把新状态盖回旧状态(库里 last_seen 还是新的),
			// LRU 也跟着退回去,下一次真实的新观测又得重写一遍。
			// 也不计冲突:它不落库,污染不了任何盘口。
			// 算进 touched 只是为了回给客户端的两个数加起来等于条数
			i.stale.Add(1)
			touched++
			continue
		}
		if ok && prev.price == o.UnitPrice && prev.amount == o.Amount && prev.ident == ident {
			prev.seen = at
			i.seen.Add(o.OrderID, prev)
			if o.ObservedAt.After(i.touches[o.OrderID]) {
				i.touches[o.OrderID] = o.ObservedAt
			}
			i.dirtyKeys[o.Key()] = struct{}{}
			touched++
			continue
		}
		if ok && prev.ident != ident {
			// 按"变了"处理:upsert 会连 location_id、reporter 一起刷新,
			// 错归的单才能被后来的观测改写。至于哪次才是对的,服务端判断不了,
			// 只能计数暴露出来,根治要靠客户端按连接分流
			i.noteConflict(o, prev.ident, ident, batch.Reporter, now)
		}
		i.seen.Add(o.OrderID, orderState{price: o.UnitPrice, amount: o.Amount, ident: ident, seen: at})
		i.pending[batch.Reporter] = append(i.pending[batch.Reporter], o)
		i.dirtyKeys[o.Key()] = struct{}{}
		changed++
	}
	return changed, touched
}

// noteConflict 记一次身份冲突,prevIdent/ident 是旧、新两个盘口的指纹。调用方持有 mu。
func (i *Ingestor) noteConflict(o model.MarketOrder, prevIdent, ident uint64, reporter string, now time.Time) {
	n := i.conflicts.Add(1)
	i.lastConflict = &Conflict{
		OrderID: o.OrderID, ItemID: o.ItemID, LocationID: o.LocationID,
		RawLocationID: o.RawLocationID, Quality: o.Quality, Side: o.Side,
		Reporter: reporter, At: o.ObservedAt,
	}
	if i.conflictAt == nil {
		i.conflictAt = map[uint64]time.Time{}
	}
	for _, h := range [2]uint64{prevIdent, ident} {
		if o.ObservedAt.After(i.conflictAt[h]) {
			i.conflictAt[h] = o.ObservedAt
		}
	}
	if now.Sub(i.lastPrune) >= 10*time.Minute {
		i.lastPrune = now
		for h, at := range i.conflictAt {
			if now.Sub(at) > conflictKeep {
				delete(i.conflictAt, h)
			}
		}
	}
	if now.Sub(i.lastWarn) >= time.Minute {
		i.lastWarn = now
		slog.Warn("同一张挂单被报到了别的盘口,多半是多开串城",
			"order_id", o.OrderID, "item", o.ItemID, "location", o.LocationID,
			"raw_location", o.RawLocationID, "reporter", reporter, "total", n)
	}
}

// ConflictedSince 返回 keys 里 since 之后卷进过身份冲突的盘口,值是最近一次的观测时间。
//
// 给读簿的调用方用(第 5 步接进扫描时):幽灵规则把"最近一眼"当权威,
// 多开串城的错归单带着新时间戳进来,会成为被串入那座城的 newest 和 view_edge,
// 把那座城上一眼的真实挂单当幽灵剔掉——以前错归只是混进盘口,现在会顶替真实盘口。
// 这些 key 应当把 slack 放到窗口大小(等于暂停幽灵剔除),并在扫描摘要里报出个数
func (i *Ingestor) ConflictedSince(keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time {
	out := map[model.QuoteKey]time.Time{}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.conflictAt) == 0 {
		return out
	}
	for _, k := range keys {
		if at, ok := i.conflictAt[identOfKey(k)]; ok && at.After(since) {
			out[k] = at
		}
	}
	return out
}

// Stats 返回入库口径的计数快照。
func (i *Ingestor) Stats() Stats {
	s := Stats{
		LocationConflicts: i.conflicts.Load(),
		ClampedFuture:     i.clamped.Load(),
		StaleObservations: i.stale.Load(),
		LegacyBatches:     i.legacy.Load(),
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
	byReporter, touches, keys := i.pending, i.touches, i.dirtyKeys
	i.pending, i.touches, i.dirtyKeys = map[string][]model.MarketOrder{}, nil, nil
	i.mu.Unlock()

	n := 0
	for _, orders := range byReporter {
		n += len(orders)
	}
	if n == 0 && len(touches) == 0 {
		return
	}
	// 变了的和没变的**一次提交**。同一眼里两种都有,分开提交的话,
	// 读簿夹在中间会读到半眼,把没变的真实挂单当幽灵剔掉(见 store.Flush)
	if err := i.store.Flush(ctx, byReporter, touches); err != nil {
		slog.Error("写入挂单失败,这一轮整批作废", "changed", n, "touched", len(touches), "err", err)
		i.forget(byReporter)
		return // 库里什么都没变,不标脏
	}
	// 提交之后才标脏:conflator 下一次 tick 读到的一定是这一轮的新状态
	if i.dirty != nil {
		for k := range keys {
			i.dirty.MarkDirty(k)
		}
	}
}

// forget 在一轮 flush 写失败后,把这一轮状态变了的单从 LRU 里拿掉。
//
// 不拿的话 LRU 以为库里已经是新状态,下一次同样的观测只会被当成"没变"去 touch,
// 库里的旧价格、旧件数就顶着新的 last_seen 一直续命。拿掉后下一次观测按"变了"重写。
// 这一轮之后新攒的 touch 里要是有这些单,也一并丢掉,理由相同。
// 代价是这一轮的 event 历史丢了。第一版不做重试队列:库挂着的时候队列只会越堆越大
func (i *Ingestor) forget(byReporter map[string][]model.MarketOrder) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, orders := range byReporter {
		for _, o := range orders {
			i.seen.Remove(o.OrderID)
			delete(i.touches, o.OrderID)
		}
	}
}
