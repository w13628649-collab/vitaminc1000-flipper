package flip

import (
	"context"
	"time"

	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/store"
)

// ConflictSource 报出最近卷进过多开串城的盘口。线上就是 *ingest.Ingestor;
// 用接口是为了 flip 不依赖 ingest,测试也能直接喂。
type ConflictSource interface {
	ConflictedSince(keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time
}

// ladderReader 是读簿那一步,线上是 *store.Store。
type ladderReader interface {
	BookSides(ctx context.Context, keys []model.QuoteKey, since time.Time,
		slack time.Duration, maxLevels int) (map[model.QuoteKey]store.BookSide, error)
}

// storeBooks 把 store.BookSides 包成 scan.BookSource。
type storeBooks struct {
	st        ladderReader
	conflicts ConflictSource // 可以为 nil:没接 ingest 时就不做串城处理
	slack     time.Duration  // "同一眼"的宽容度
	window    time.Duration  // 抓包窗口;串城的 key 把 slack 放到这么大
	levels    int
}

// CaptureBooks 读一批盘口边。
//
// 最近卷进过多开串城的 key 单独读一遍,slack 放到窗口大小——等于对它们暂停
// 幽灵剔除。幽灵规则把"最近一眼"当权威,而错归的单恰恰带着最新的时间戳,
// 会成为被串入那座城的最近一眼,把那座城上一眼的真实挂单当幽灵剔掉。
// 暂停剔除的代价是可能留几张已成交的旧单,比整段真实盘口凭空消失要轻。
func (b *storeBooks) CaptureBooks(ctx context.Context, keys []model.QuoteKey,
	since time.Time) (map[model.QuoteKey]scan.CapturedSide, error) {

	var conflicted map[model.QuoteKey]time.Time
	if b.conflicts != nil {
		conflicted = b.conflicts.ConflictedSince(keys, since)
	}
	normal := keys
	var risky []model.QuoteKey
	if len(conflicted) > 0 {
		normal = make([]model.QuoteKey, 0, len(keys))
		for _, k := range keys {
			if _, bad := conflicted[k]; bad {
				risky = append(risky, k)
			} else {
				normal = append(normal, k)
			}
		}
	}

	out := make(map[model.QuoteKey]scan.CapturedSide)
	read := func(keys []model.QuoteKey, slack time.Duration, flagged bool) error {
		if len(keys) == 0 {
			return nil
		}
		got, err := b.st.BookSides(ctx, keys, since, slack, b.levels)
		if err != nil {
			return err
		}
		for k, side := range got {
			cs := toCaptured(side)
			cs.Conflicted = flagged
			out[k] = cs
		}
		return nil
	}
	if err := read(normal, b.slack, false); err != nil {
		return nil, err
	}
	wide := b.window
	if wide < b.slack {
		wide = b.slack
	}
	if err := read(risky, wide, true); err != nil {
		return nil, err
	}
	return out, nil
}

func toCaptured(b store.BookSide) scan.CapturedSide {
	cs := scan.CapturedSide{
		Levels:     make([]depth.Level, len(b.Levels)),
		LevelSeen:  make([]time.Time, len(b.Levels)),
		Newest:     b.Newest,
		QtyTotal:   b.QtyTotal,
		LevelCount: b.LevelCount,
		Ghosts:     b.Ghosts,
	}
	for i, l := range b.Levels {
		cs.Levels[i] = depth.Level{Price: l.Price, Qty: l.Depth}
		cs.LevelSeen[i] = l.Seen
	}
	return cs
}

// books 是扫描用的读簿来源。没有库(测试里)时是 nil,扫描退回纯 AODP;
// 开关关着时照样返回——要不要融合由 scan 按 capture.enabled 决定,口径只在一处
func (s *Service) books() scan.BookSource {
	if s.Store == nil {
		return nil
	}
	return &storeBooks{
		st:        s.Store,
		conflicts: s.Conflicts,
		slack:     s.Cfg.SnapshotSlack(),
		window:    s.Cfg.CaptureWindow(),
		levels:    s.Cfg.Capture.BookLevels,
	}
}
