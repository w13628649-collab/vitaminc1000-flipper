package ingest

import (
	"context"
	"errors"
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

	at := func(d time.Duration) model.MarketOrder {
		o := order(1, 1000, 50)
		o.ObservedAt = t0.Add(d)
		return o
	}
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{at(-3 * time.Minute)}})
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

// 断网重传、两个成员先后上传同一眼:旧观测晚到,状态又和当前不同。
// 以前它算"变了",会把新状态盖回旧状态,LRU 也退回去,
// 下一次真实的新观测又被当成变化重写一遍
func TestSubmit_乱序晚到的旧观测不回写(t *testing.T) {
	ing, dirty := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }
	obs := func(amount int32, d time.Duration) model.MarketOrder {
		o := order(42, 1000, amount)
		o.ObservedAt = t0.Add(d)
		return o
	}
	// SentAt 填成 now:钟是准的,这里只测乱序
	ing.Submit(model.UploadBatch{Reporter: "甲", SentAt: t0, Orders: []model.MarketOrder{obs(5, -10*time.Minute)}})
	ing.Submit(model.UploadBatch{Reporter: "乙", SentAt: t0, Orders: []model.MarketOrder{obs(3, -time.Minute)}})

	changed, touched := ing.Submit(model.UploadBatch{Reporter: "甲", SentAt: t0,
		Orders: []model.MarketOrder{obs(5, -5*time.Minute)}}) // 甲重传 T−5m 那一眼
	if changed != 0 || touched != 1 {
		t.Fatalf("旧观测不该算变化,得到 changed=%d touched=%d", changed, touched)
	}
	if n := ing.Stats().StaleObservations; n != 1 {
		t.Fatalf("旧观测计数应为 1,得到 %d", n)
	}
	if n := len(ing.pending["甲"]); n != 1 {
		t.Fatalf("甲的待写应只有最初那条,得到 %d", n)
	}
	if _, ok := ing.touches[42]; ok {
		t.Fatal("旧观测也不该进 touch,反正刷不动 last_seen")
	}
	if n := len(dirty.keys); n != 2 {
		t.Fatalf("旧观测不该标脏,应共 2 次,得到 %d", n)
	}

	// LRU 仍是乙的 ×3:乙的下一次同样观测只是 touch
	if changed, touched := ing.Submit(model.UploadBatch{Reporter: "乙", SentAt: t0,
		Orders: []model.MarketOrder{obs(3, 0)}}); changed != 0 || touched != 1 {
		t.Fatalf("LRU 不该被旧观测改回去,得到 changed=%d touched=%d", changed, touched)
	}
	if got := ing.touches[42]; !got.Equal(t0) {
		t.Fatalf("touch 应记 T,得到 %v", got)
	}

	// touch 也推进了已知的观测时间:比它旧的不同状态照样挡掉
	if changed, _ := ing.Submit(model.UploadBatch{Reporter: "丙", SentAt: t0,
		Orders: []model.MarketOrder{obs(4, -30*time.Second)}}); changed != 0 {
		t.Fatal("比最近一次 touch 还旧的观测不该算变化")
	}
}

// 串城的单新旧两个盘口都要记下冲突时刻:服务端分不清哪边是对的,
// 两边的"最近一眼"都可能被错归单搅过,读簿时要对这些 key 暂停幽灵剔除
func TestConflictedSince_新旧两个盘口都记(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }
	first := order(1, 1000, 50)
	first.ObservedAt = t0.Add(-time.Minute)
	ing.Submit(model.UploadBatch{SentAt: t0, Orders: []model.MarketOrder{first}})
	moved := first
	moved.LocationID, moved.ObservedAt = "Thetford", t0
	ing.Submit(model.UploadBatch{SentAt: t0, Orders: []model.MarketOrder{moved}})

	kM, kT, kOther := first.Key(), moved.Key(), first.Key()
	kOther.LocationID = "Lymhurst"
	got := ing.ConflictedSince([]model.QuoteKey{kM, kT, kOther}, t0.Add(-5*time.Minute))
	if len(got) != 2 || !got[kM].Equal(t0) || !got[kT].Equal(t0) {
		t.Fatalf("Martlock 和 Thetford 都应记 T,得到 %v", got)
	}
	if got := ing.ConflictedSince([]model.QuoteKey{kM, kT}, t0); len(got) != 0 {
		t.Fatalf("since 之后没有冲突,应为空,得到 %v", got)
	}
}

// 老客户端不带 sent_at,钟纠不了偏。数出来,才知道还有多少人没升级
func TestSubmit_不带SentAt的批次计数(t *testing.T) {
	ing, _ := newTestIngestor(t)
	ing.now = func() time.Time { return t0 }
	ing.Submit(model.UploadBatch{Orders: []model.MarketOrder{order(1, 1000, 50)}})
	ing.Submit(model.UploadBatch{SentAt: t0, Orders: []model.MarketOrder{order(2, 1000, 50)}})
	ing.Submit(model.UploadBatch{}) // 空批不算
	if n := ing.Stats().LegacyBatches; n != 1 {
		t.Fatalf("应只数到 1 个老客户端批次,得到 %d", n)
	}
}

// fakeSink 记下每次 Flush 收到了什么;during 在 Flush 里执行,
// 用来模拟"写库期间又有新上传进来"
type fakeSink struct {
	calls  []flushCall
	err    error
	during func()
}

type flushCall struct {
	byReporter map[string][]model.MarketOrder
	touches    map[int64]time.Time
}

func (f *fakeSink) Flush(_ context.Context, byReporter map[string][]model.MarketOrder, touches map[int64]time.Time) error {
	f.calls = append(f.calls, flushCall{byReporter, touches})
	if f.during != nil {
		f.during()
	}
	return f.err
}

// 同一眼里变了的走 upsert、没变的走 touch。两路分开提交的话,
// 读簿夹在中间读到半眼,会把没变的真实挂单当幽灵剔掉。
// 一轮 flush 必须只调一次 Flush,两样一起交
func TestFlush_变了的和没变的一次提交(t *testing.T) {
	ing, _ := newTestIngestor(t)
	sk := &fakeSink{}
	ing.store = sk
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{order(1, 1000, 50)}})
	ing.flush(context.Background())
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{
		order(1, 1000, 50), // 没变 → touch
		order(2, 900, 10),  // 新单
	}})
	ing.Submit(model.UploadBatch{Reporter: "乙", Orders: []model.MarketOrder{order(3, 800, 5)}})
	sk.calls = nil
	ing.flush(context.Background())

	if len(sk.calls) != 1 {
		t.Fatalf("一轮 flush 应只提交一次,得到 %d 次", len(sk.calls))
	}
	c := sk.calls[0]
	if len(c.byReporter["甲"]) != 1 || len(c.byReporter["乙"]) != 1 || len(c.touches) != 1 {
		t.Fatalf("应同时带上两人的变化和一条 touch,得到 %+v", c)
	}
	if ing.PendingCount() != 0 || len(ing.touches) != 0 {
		t.Fatal("flush 之后待写和待刷新都应清空")
	}

	sk.calls = nil
	ing.flush(context.Background())
	if len(sk.calls) != 0 {
		t.Fatal("没东西可写时不该碰库")
	}
}

// 写库失败后,LRU 里那几张单的新状态库里其实没有。不拿掉的话,
// 下一次同样的观测只会 touch,库里的旧价格顶着新 last_seen 一直续命
func TestFlush_写失败后这批单下次按变化重写(t *testing.T) {
	ing, _ := newTestIngestor(t)
	sk := &fakeSink{}
	ing.store = sk
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{
		order(1, 1000, 50), order(2, 900, 10)}})
	ing.flush(context.Background()) // 两张单都写成功

	// 1 号单改价后写失败;写库期间又来了一次同价观测(会进 touch)
	sk.err = errors.New("库挂了")
	ing.Submit(model.UploadBatch{Reporter: "甲", Orders: []model.MarketOrder{order(1, 990, 50)}})
	sk.during = func() {
		if _, touched := ing.Submit(model.UploadBatch{Reporter: "甲",
			Orders: []model.MarketOrder{order(1, 990, 50)}}); touched != 1 {
			t.Error("写库期间 LRU 还是新状态,同价观测应先算 touch")
		}
	}
	ing.flush(context.Background())
	sk.during, sk.err = nil, nil

	if _, ok := ing.touches[1]; ok {
		t.Fatal("写失败那张单攒下的 touch 应一并丢掉")
	}
	if changed, _ := ing.Submit(model.UploadBatch{Reporter: "甲",
		Orders: []model.MarketOrder{order(1, 990, 50)}}); changed != 1 {
		t.Fatal("写失败的单下一次观测应按变化重写")
	}
	if _, touched := ing.Submit(model.UploadBatch{Reporter: "甲",
		Orders: []model.MarketOrder{order(2, 900, 10)}}); touched != 1 {
		t.Fatal("写成功的单不受影响,仍应只 touch")
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
