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

// 抓包报上来的是市场 id(0007),AODP 和界面用城市名。盘口键必须在
// 标脏之前就收敛,否则推送的 key 和界面订阅的 key 对不上,
// 实时页上抓到的数据一条都不会出现
func TestSubmit_市场id收敛成城市名并保留原值(t *testing.T) {
	ing, dirty := newTestIngestor(t)
	o := order(1, 1000, 50)
	o.LocationID = "3008"
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{o}})

	if len(dirty.keys) != 1 || dirty.keys[0].LocationID != "Martlock" {
		t.Fatalf("脏盘口该按城市名标,得到 %+v", dirty.keys)
	}
	got := ing.pending["甲"][0]
	if got.LocationID != "Martlock" || got.RawLocationID != "3008" {
		t.Fatalf("落库应为 Martlock 且保留原值 3008,得到 %q / %q", got.LocationID, got.RawLocationID)
	}
}

// 多开时客户端只有一个"当前位置",Martlock 的单会被记成 Thetford(或反过来)。
// 以前 LRU 只比价格和数量,错归的单进了缓存后,正确城市的观测只会续命、
// 永远改不回来;现在身份指纹变了就按"变了"处理并计数
func TestSubmit_同一张单换了城市按变化处理并计数(t *testing.T) {
	ing, dirty := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }

	first := order(1, 1000, 50)
	first.ObservedAt = t0.Add(-time.Minute)
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{first}})

	moved := order(1, 1000, 50) // 同价同量,只有城市不同
	moved.LocationID = "Thetford"
	moved.ObservedAt = t0
	changed, touched := ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{moved}})
	if changed != 1 || touched != 0 {
		t.Fatalf("换城市应该入库,得到 changed=%d touched=%d", changed, touched)
	}

	st := ing.Stats()
	if st.LocationConflicts != 1 {
		t.Fatalf("冲突计数应为 1,得到 %d", st.LocationConflicts)
	}
	if st.LastConflict == nil || st.LastConflict.OrderID != 1 || st.LastConflict.LocationID != "Thetford" {
		t.Fatalf("最近一次冲突现场不对: %+v", st.LastConflict)
	}
	p := ing.pending["甲"]
	if last := p[len(p)-1]; last.OrderID != 1 || last.LocationID != "Thetford" {
		t.Fatalf("待写队列里这张单最后应是 Thetford,得到 %+v", last)
	}
	if n := len(dirty.keys); n != 2 || dirty.keys[1].LocationID != "Thetford" {
		t.Fatalf("新盘口要标脏,得到 %+v", dirty.keys)
	}

	// 再报一次 Thetford:指纹已更新,这回才是真的"没变"
	again := moved
	again.ObservedAt = t0
	if changed, touched := ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{again}}); changed != 0 || touched != 1 {
		t.Fatalf("指纹更新后的重复观测应只 touch,得到 changed=%d touched=%d", changed, touched)
	}
	if ing.Stats().LocationConflicts != 1 {
		t.Fatal("重复观测不该再计冲突")
	}
}

// touch 记的是每张单最新的观测时间。乱序到达的旧观测不能把它往回拨,
// 否则 last_seen 会忽早忽晚,"同一眼"的判断跟着抖
func TestSubmit_同一张单多次touch取最晚观测(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})

	at := func(d time.Duration) model.MarketOrder {
		o := order(1, 1000, 50)
		o.ObservedAt = t0.Add(d)
		return o
	}
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{at(-time.Minute)}})
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{at(0)}})
	if got := ing.touches[1]; !got.Equal(t0) {
		t.Fatalf("touches[1] 应为 T,得到 %v", got)
	}
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{at(-2 * time.Minute)}}) // 晚到的旧观测
	if got := ing.touches[1]; !got.Equal(t0) {
		t.Fatalf("旧观测不该把 touches[1] 往回拨,得到 %v", got)
	}
}

// 钟快的老客户端:观测时间被钳到服务端 now,计数要能在 /api/coverage 上看到
func TestSubmit_未来时间被钳位并计数(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }
	o := order(1, 1000, 50)
	o.ObservedAt = t0.Add(time.Hour)
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{o}})

	if n := ing.Stats().ClampedFuture; n != 1 {
		t.Fatalf("钳位计数应为 1,得到 %d", n)
	}
	if got := ing.pending["甲"][0].ObservedAt; !got.Equal(t0) {
		t.Fatalf("落库的观测时间应被钳到 T,得到 %v", got)
	}
}

func TestSubmit_附魔后缀在入库前补齐(t *testing.T) {
	ing, dirty := newTestIngestor(t)
	o := order(1, 1000, 50)
	o.ItemID, o.Enchant = "T5_2H_FIRESTAFF", 2
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{o}})
	if got := ing.pending["甲"][0].ItemID; got != "T5_2H_FIRESTAFF@2" {
		t.Fatalf("落库物品应为 T5_2H_FIRESTAFF@2,得到 %q", got)
	}
	if dirty.keys[0].ItemID != "T5_2H_FIRESTAFF@2" {
		t.Fatalf("脏盘口也要用补齐后的 id,得到 %q", dirty.keys[0].ItemID)
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
