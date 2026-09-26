package scan

import (
	"testing"
	"time"

	"albion-guild/internal/book"
	"albion-guild/internal/model"
)

func bookOrder(price, amt int64, seen time.Time, page int) book.Order {
	return book.Order{ItemID: "T5_CLOTH", City: "Lymhurst", Quality: 1, Side: model.SideOffer,
		Price: price, Amount: amt, LastSeen: seen, Page: page}
}

// 原 store.BookSides 的"截断只砍档不砍合计":按 capture.book_levels 截档之后,
// QtyTotal / LevelCount 还是全部档的,LevelCount > len(Levels) 就是截断了
// (MergeSide 据此判"近价件数只是下限")。截档现在发生在 CapturedFrom
func TestCapturedFrom_截断只砍档不砍合计(t *testing.T) {
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	s := book.Build([]book.Order{
		bookOrder(100, 5, T.Add(-10*time.Minute), 0), // 残单
		bookOrder(104, 4, T.Add(-time.Minute), 0),
		bookOrder(105, 7, T, 0), bookOrder(105, 6, T.Add(-30*time.Second), 0),
		bookOrder(110, 3, T, 0),
	}, model.SideOffer, 120*time.Second)

	cs := CapturedFrom(s, 2)
	if len(cs.Levels) != 2 || len(cs.LevelSeen) != 2 || cs.Levels[0].Price != 104 || cs.Levels[1].Qty != 13 {
		t.Fatalf("应只留 104×4 / 105×13 两档,得到 %+v", cs.Levels)
	}
	if cs.LevelCount != 3 || cs.QtyTotal != 20 || cs.Ghosts != 1 || !cs.Newest.Equal(T) {
		t.Fatalf("合计应是全部 3 档 20 件、剔 1 张,得到 %+v", cs)
	}
	if !cs.LevelSeen[0].Equal(T.Add(-time.Minute)) || !cs.LevelSeen[1].Equal(T) {
		t.Fatalf("LevelSeen 应是每档最近一次看到的时刻,得到 %v", cs.LevelSeen)
	}
	if all := CapturedFrom(s, 0); len(all.Levels) != 3 || all.LevelCount != 3 {
		t.Fatalf("maxLevels ≤ 0 不截,得到 %+v", all)
	}
}

// 满页和续页的信息原样带过来;满页不进"按档截断"那个标记(见 PageTruncated 的注释)
func TestCapturedFrom_满页和续页原样带过来(t *testing.T) {
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	var orders []book.Order
	for i := int64(0); i < book.PageSize; i++ {
		orders = append(orders, bookOrder(100+i, 1, T.Add(-10*time.Minute), 0), bookOrder(150+i, 1, T, 0))
	}
	orders = append(orders, bookOrder(300, 9, T.Add(-time.Hour), 0)) // 比满页的最远还深:stale
	cs := CapturedFrom(book.Build(orders, model.SideOffer, 120*time.Second), 128)
	if !cs.PageTruncated || cs.PrevPage != book.PageSize || cs.StaleOrders != 1 || cs.StaleQty != 9 {
		t.Fatalf("应带上满页 / 续页 50 张 / stale 1 张 9 件,得到 %+v", cs)
	}
	if cs.Levels[0].Price != 100 || cs.LevelCount != 2*book.PageSize || cs.QtyTotal != 2*book.PageSize {
		t.Fatalf("stale 不进档和合计,得到卖一 %d / %d 档 / %d 件", cs.Levels[0].Price, cs.LevelCount, cs.QtyTotal)
	}
}
