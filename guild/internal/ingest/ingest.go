// Package ingest 接收客户端上传,去重后落库并标记脏盘口。
package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

// DirtyMarker 告诉行情层"这个盘口变了,该重算最优价了"。
// 用接口是为了让 ingest 不依赖 hub,两边可以独立测试。
type DirtyMarker interface {
	MarkDirty(model.QuoteKey)
}

// orderState 是一张挂单的最后已知状态。
//
// 只存价格和数量,不存物品/城市:order_id 是订单的身份,
// 游戏里一张单的物品和城市不会变(撤单再挂是新的 order_id)。
// 这个假设成立,去重键才能只看这两个字段。
type orderState struct {
	price  int64
	amount int32
}

type Ingestor struct {
	store *store.Store
	dirty DirtyMarker
	seen  *lru.Cache[int64, orderState]

	mu       sync.Mutex
	pending  []model.MarketOrder // 状态变了的,要写两张表
	touches  []int64             // 状态没变的,只刷 last_seen
	reporter string

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
		flushSize: 500,
		flushWait: 2 * time.Second,
	}, nil
}

// Submit 处理一批上传。
//
// 这里是整个系统写入放大的闸门:200 人同时翻市场,同一张挂单会被看到几十次,
// 不挡掉的话库里全是"什么都没发生"的重复行。
func (i *Ingestor) Submit(batch model.UploadBatch) (changed, touched int) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.reporter = batch.Reporter
	for _, o := range batch.Orders {
		prev, ok := i.seen.Get(o.OrderID)
		if ok && prev.price == o.UnitPrice && prev.amount == o.Amount {
			i.touches = append(i.touches, o.OrderID)
			touched++
			continue
		}
		i.seen.Add(o.OrderID, orderState{price: o.UnitPrice, amount: o.Amount})
		i.pending = append(i.pending, o)
		i.dirty.MarkDirty(o.Key())
		changed++
	}
	return changed, touched
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
	orders, touches, reporter := i.pending, i.touches, i.reporter
	i.pending, i.touches = nil, nil
	i.mu.Unlock()

	if len(orders) > 0 {
		if err := i.store.WriteOrders(ctx, reporter, orders); err != nil {
			slog.Error("写入挂单失败", "count", len(orders), "err", err)
			// 这里应该进重试队列而不是丢掉,第一版先记日志
		}
	}
	if len(touches) > 0 {
		if err := i.store.TouchOrders(ctx, touches, time.Now()); err != nil {
			slog.Error("刷新 last_seen 失败", "count", len(touches), "err", err)
		}
	}
}
