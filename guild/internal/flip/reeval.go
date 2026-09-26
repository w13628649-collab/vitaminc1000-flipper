package flip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"albion-guild/internal/scan"
)

// errNotReady:还没有 AODP 快照可重算(服务刚起、第一次全量还没跑完,或目录没载入)。
var errNotReady = errors.New("还没有 AODP 快照,等第一次全量扫描")

// backfillEvery 是两次补拉之间至少隔多久。成员翻市场时每分钟都可能冒出新物品,
// 不节流的话每次重算都要打一两次 AODP;攒 5 分钟一批,配额几乎不受影响
const backfillEvery = 5 * time.Minute

// Reevaluate 用缓存的 AODP 快照配上当下的抓包盘口重算一遍机会板,原子替换对外结果。
//
// 两次全量之间新抓到、快照里还没有的物品,到点了就批量补拉它们的 AODP
// 价格和历史并进快照(至多每 5 分钟一次);没到点的在 capture.extra_pending 里报数。
// 除了补拉,重算不打 AODP;查价页现取过的 AODP 当前价会并进来(evalSnapshot)。
func (s *Service) Reevaluate(ctx context.Context) (*scan.Result, error) {
	cat := s.cat.Load()
	if cat == nil || s.snap.Load() == nil {
		return nil, errNotReady
	}
	books := s.books()

	// 补拉要打 AODP,放在锁外:限流等待可能好几秒,读结果和全量扫描不该跟着等
	captured, listErr := scan.ListCaptured(ctx, s.Cfg, books, time.Now().UTC())
	var ext *scan.Extension
	var fillErr error
	if pending := s.snap.Load().PendingExtras(captured, s.Cfg, cat); len(pending) > 0 {
		ext, fillErr = s.backfill(ctx, pending)
	}

	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	// 锁里重读快照:补拉期间可能刚跑完一次全量,With 会跳过它已经有的物品。
	// 评估时并上查价页现取过的 AODP,和面板用同一份(见 aodp_fresh.go)
	snap := s.snap.Load().With(ext)
	s.snap.Store(snap)
	res := scan.EvaluateSnapshot(ctx, s.evalSnapshot(snap), s.Cfg, cat, books, time.Now().UTC())
	if err := ctx.Err(); err != nil {
		// 自己的 ctx 被取消了(服务端在关):读簿多半是因此失败、退回了纯 AODP,
		// 这份降级结果不该盖掉手上那份好的。重算便宜,不像全量那样值得收尾
		return nil, err
	}
	res.Capture.ExtraPending = len(snap.PendingExtras(captured, s.Cfg, cat))
	res.Capture.AddError(listErr)
	res.Capture.AddError(fillErr)
	s.publish(res, false)
	return res, nil
}

// backfill 给快照里还没有的物品补拉 AODP。没到点、全量正在跑、上一轮补拉
// 还没完时什么都不做(返回 nil, nil)。
func (s *Service) backfill(ctx context.Context, pending []string) (*scan.Extension, error) {
	now := time.Now().UTC()
	if last := s.lastBackfill.Load(); last != 0 && now.Sub(time.Unix(0, last)) < backfillEvery {
		return nil, nil
	}
	if s.scanning.Load() {
		return nil, nil // 全量正在跑,它列物品时会把这些一起带上
	}
	if !s.backfilling.CompareAndSwap(false, true) {
		return nil, nil
	}
	defer s.backfilling.Store(false)
	// 失败也算一次:AODP 挂着的时候不能每分钟去撞一回
	s.lastBackfill.Store(now.UnixNano())

	ext, history, err := scan.FetchExtension(ctx, s.aodp, s.Cfg, pending, now)
	if err != nil {
		slog.Warn("给新抓到的物品补拉 AODP 失败", "items", len(pending), "err", err)
		return nil, fmt.Errorf("补拉 AODP: %w", err)
	}
	slog.Info("给新抓到的物品补拉了 AODP", "items", len(pending), "requests", ext.RequestCount)
	s.writeHistory(ctx, history)
	return ext, nil
}

// RunReeval 按 capture.reeval_seconds 定时重算。开关关着或间隔为 0 时直接返回。
//
// 和全量扫描(Run)各跑各的 goroutine:全量拉 AODP 可能要好几分钟,
// 这期间重算照常用旧快照出结果;发布顺序由 evalMu 保证。
func (s *Service) RunReeval(ctx context.Context) {
	every := s.Cfg.ReevalInterval()
	if !s.Cfg.Capture.Enabled || every <= 0 {
		return
	}
	slog.Info("抓包定时重算已开启", "every", every)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			start := time.Now()
			res, err := s.Reevaluate(ctx)
			switch {
			case errors.Is(err, errNotReady):
			case err != nil && ctx.Err() != nil: // 服务端在关,不算失败
			case err != nil:
				slog.Warn("抓包重算失败", "err", err)
			default:
				slog.Debug("抓包重算完成", "机会", len(res.Opportunities),
					"抓包卖方", res.Capture.AsksUsed, "抓包买方", res.Capture.BidsUsed,
					"待补拉", res.Capture.ExtraPending, "耗时", time.Since(start).Round(time.Millisecond))
			}
		}
	}
}
