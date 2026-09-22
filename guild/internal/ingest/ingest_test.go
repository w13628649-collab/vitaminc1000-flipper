package ingest

import (
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"albion-guild/internal/model"
)

type fakeDirty struct{ keys []model.QuoteKey }

func (f *fakeDirty) MarkDirty(k model.QuoteKey) { f.keys = append(f.keys, k) }

// 这些用例不碰数据库,只测去重那一层的判断
func newTestIngestor(t *testing.T) (*Ingestor, *fakeDirty) {
	t.Helper()
	c, err := lru.New[int64, orderState](1000)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDirty{}
	return &Ingestor{seen: c, dirty: d}, d
}

func order(id int64, price int64, amount int32) model.MarketOrder {
	return model.MarketOrder{
		OrderID: id, ItemID: "T5_METALBAR", LocationID: "Martlock",
		Quality: 1, Side: model.SideOffer, UnitPrice: price, Amount: amount,
		ObservedAt: time.Now(),
	}
}

func TestSubmit_状态没变的重复观测被挡掉(t *testing.T) {
	// 200 人同时翻市场,同一张挂单会被看到几十次。
	// 挡不住的话库里全是"什么都没发生"的重复行。
	ing, _ := newTestIngestor(t)

	changed, touched := ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{
		order(1, 1000, 50), order(2, 1100, 30),
	}})
	if changed != 2 || touched != 0 {
		t.Fatalf("首次上传应该全部入库,得到 changed=%d touched=%d", changed, touched)
	}

	changed, touched = ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{
		order(1, 1000, 50), order(2, 1100, 30),
	}})
	if changed != 0 || touched != 2 {
		t.Fatalf("完全相同的再次观测应该全被去重,得到 changed=%d touched=%d", changed, touched)
	}
}

func TestSubmit_价格或数量变了要入库(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})

	if changed, _ := ing.Submit(model.UploadBatch{
		Orders: []model.MarketOrder{order(1, 990, 50)}}); changed != 1 {
		t.Fatal("改价应该入库")
	}
	if changed, _ := ing.Submit(model.UploadBatch{
		Orders: []model.MarketOrder{order(1, 990, 30)}}); changed != 1 {
		t.Fatal("被买走一部分(数量变化)应该入库")
	}
}

func TestSubmit_只有真变化才标记脏盘口(t *testing.T) {
	// 脏盘口会触发一次查库算最优价再广播,重复标记等于白跑
	ing, dirty := newTestIngestor(t)
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})

	if len(dirty.keys) != 1 {
		t.Fatalf("应该只标记一次,得到 %d 次", len(dirty.keys))
	}
	want := model.QuoteKey{ItemID: "T5_METALBAR", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
	if dirty.keys[0] != want {
		t.Fatalf("脏盘口不对: %+v", dirty.keys[0])
	}
}

func TestSubmit_待写队列按变化与否分流(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{
		order(1, 1000, 50), // 没变 → 只刷 last_seen
		order(2, 900, 10),  // 新单 → 要写两张表
	}})

	if n := ing.PendingCount(); n != 2 {
		t.Fatalf("待写队列应有 2 条(首次 + 新单),得到 %d", n)
	}
	if len(ing.touches) != 1 {
		t.Fatalf("待刷新队列应有 1 条,得到 %d", len(ing.touches))
	}
}

// 两次 flush 之间会有好几个成员提交,归属不能串。
// 串了的话库里 reporter 那列全是最后一个上传的人,
// 想知道"谁在传数据"就永远查不准。
func TestSubmit_多人上传各归各的(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{order(1, 1000, 50)}})
	ing.Submit(model.UploadBatch{Reporter: "乙", Orders: []model.MarketOrder{order(2, 900, 10)}})
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{order(3, 800, 5)}})

	ing.mu.Lock()
	defer ing.mu.Unlock()
	if len(ing.pending) != 2 {
		t.Fatalf("应该分成 2 个上报人,得到 %d", len(ing.pending))
	}
	if n := len(ing.pending["甲"]); n != 2 {
		t.Fatalf("甲应该有 2 条,得到 %d", n)
	}
	if n := len(ing.pending["乙"]); n != 1 {
		t.Fatalf("乙应该有 1 条,得到 %d", n)
	}
}
