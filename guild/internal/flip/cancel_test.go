package flip

import (
	"context"
	"errors"
	"testing"
	"time"

	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/screen"
)

// cancelBooks 在读簿那一刻把调用方的 ctx 取消掉:模拟 POST /api/scan 的客户端
// 恰好在 AODP 拉完、读簿之前断开。读簿时 ctx 已取消就照 pgx 的样子报错
type cancelBooks struct {
	*rvBooks
	cancel context.CancelFunc
}

func (b *cancelBooks) CaptureBooks(ctx context.Context, keys []model.QuoteKey, since time.Time) (map[model.QuoteKey]scan.CapturedSide, error) {
	b.cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.rvBooks.CaptureBooks(ctx, keys, since)
}

// AODP 已经拉完、配额花出去了,调用方这时断开不该把全局结果换成一份"读簿失败、
// 退回纯 AODP、带着 capture.error"的降级结果,还推给所有界面
func TestScan_拉完AODP之后调用方断开照样用抓包发布(t *testing.T) {
	s, _, books := newRVService(t, rvConfig())
	books.setAsk("T5_CLOTH", 1400, 50, time.Now().UTC().Add(-time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.bookSrc = &cancelBooks{rvBooks: books, cancel: cancel}
	rec := &recPublisher{}
	s.Events = rec

	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("前提:读簿时调用方的 ctx 应已取消")
	}
	pub := s.LastScan()
	if pub.Capture.Error != "" || pub.Capture.AsksUsed != 1 {
		t.Fatalf("不该退回纯 AODP,得到 capture=%+v", pub.Capture)
	}
	if o := rvOpp(pub, "T5_CLOTH"); o == nil || o.SellPrice != 1400 || o.Ask.Source != screen.SourceCapture {
		t.Fatalf("应照样用上抓包卖价 1400,得到 %+v", o)
	}
	if evs := rec.events(t); len(evs) != 1 || !evs[0].Full || evs[0].Digest != pub.Digest {
		t.Fatalf("全量照常推一条,得到 %+v", evs)
	}
}

// 重算便宜,自己的 ctx 被取消(服务端在关)时不发布:手上那份好结果不能被一份
// 读簿失败的降级结果盖掉
func TestReevaluate_ctx取消时不发布(t *testing.T) {
	s, _, books := newRVService(t, rvConfig())
	rec := &recPublisher{}
	s.Events = rec
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := s.LastScan()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.bookSrc = &cancelBooks{rvBooks: books, cancel: cancel}
	if _, err := s.Reevaluate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled,得到 %v", err)
	}
	if s.LastScan() != before {
		t.Fatal("取消了还替换了对外结果")
	}
	if n := len(rec.events(t)); n != 1 {
		t.Fatalf("取消了不该再推,共推 %d 条", n)
	}
}
