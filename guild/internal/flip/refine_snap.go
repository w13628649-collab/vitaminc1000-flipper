package flip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/model"
	"albion-guild/internal/refine"
)

// 精炼页的数据快照(诉求 #1/#8):把「读数据」(I/O)和「算」(个人参数)分开。
//
// 一次点击原来要:读簿(BookOrders + book.Build,72h 窗口)、等 AODP(冷了最多 12 秒)、
// 读成交量,再跑引擎。引擎实测 T4 不到 1ms、T8.0~8.3 最慢 53ms,矩阵 25 格并行约 60ms ——
// 卡的不是引擎。可计算缓存的键带全部个人参数、TTL 只有 2 秒,几乎每次点击都未命中,
// 整条 I/O 跟着重走一遍。
//
// 现在按 (线, 城市) 存一份原料快照:这条线全部物品在这座城两边的盘口(book.Build 之后)、
// 串城标记、AODP 当前价、25 个目标的日成交量。refine.Key 是「物品 × 城市」、合并逐边独立,
// 任意 买料城市 / 出售城市 组合都能拿几个单城快照拼出来,所以不按请求的城市组合建。
// 点击时只读快照、按请求的 max_age_hours 合并(mergeBooks)、跑引擎;引擎结果依赖个人参数,不预算。
//
// 什么时候重建(staleLocked):
//   - 盘口 refineSnapTTL(60s)到期:AODP 更新、串城记录、72h 窗口滑动都靠它兜
//   - 入库标脏(MarkRefineDirty)或客户端带来的 WS 报价时刻(RefineOpts.After)比读簿时刻新
//   - AODP 单独 refineSnapAODPTTL(120s);成交量 refineSnapVolTTL(10 分钟,日线一天才变一根)
//   - 同一个快照两次重建至少隔 refineSnapMinGap(2s),抗翻页风暴
//
// 重建时盘口每次都重读;AODP 和成交量没到期就沿用上一份。几座城一起缺的合成一批:
// 一条 BookOrders、一次 AODP(这条线 × 这些城)、每城一次成交量。
const (
	refineSnapTTL       = 60 * time.Second
	refineSnapAODPTTL   = 120 * time.Second
	refineSnapAODPRetry = 20 * time.Second // AODP 没取到时,至少隔这么久才为它再重建
	refineSnapVolTTL    = 10 * time.Minute
	refineSnapMinGap    = 2 * time.Second
	// refineSnapColdAODPWait:冷启动(还没有快照)时最多等 AODP 这么久(原来是 lookupAODPWait 12 秒)。
	// 没等到的请求留在后台跑完,回来以后标脏重建一次
	refineSnapColdAODPWait = 3 * time.Second
	refineWarmIdle         = 15 * time.Minute
	refineWarmTick         = 20 * time.Second
	// refineWarmDebounce 比界面的推送防抖(1.5 秒)短:推送触发的重算一般直接命中刚建好的快照
	refineWarmDebounce = time.Second
)

// refineSnapWait:要重建的快照最多等这么久,等不到先用旧的(stale)。变量是为了测试能调短
var refineSnapWait = 3 * time.Second

type snapKey struct{ line, city string }

type refineCell struct{ item, city string }

// refineSnap 是一条线在一座城的原料快照。发布之后不再改(标脏记在 refineState.dirty 里),
// 几个请求、矩阵的 25 个格子并发读不用加锁。
type refineSnap struct {
	key snapKey
	// ver 全局单调递增,进计算缓存的键:数据一变,同样的参数也重算
	ver uint64
	// at 是读簿时刻(→ books.read_at)
	at      time.Time
	sides   map[model.QuoteKey]book.Side
	flagged map[model.QuoteKey]bool
	// fetched 是最近一次成功取回的 AODP 原始记录(这座城);prices 是它并上扫描快照和查价页取过的
	fetched   []aodp.PriceRecord
	prices    map[refineCell]aodp.PriceRecord
	pricesAt  time.Time // 最近一次成功取 AODP 的时刻;零 = 还没成功过
	aodpTried time.Time
	aodpErr   error
	vols      map[string]refine.Volume
	volsAt    time.Time

	readDur, aodpDur, volDur time.Duration
	aodpWaited               bool
}

// volume 是 target 在这座城的日成交量;没有数据时 Known=false(不当成 0)。
func (sn *refineSnap) volume(target string, days int) refine.Volume {
	if sn != nil {
		if v, ok := sn.vols[target]; ok {
			return v
		}
	}
	return refine.Volume{Days: days}
}

// snapUse 是这次请求怎么拿到快照的(进 RefineTiming)。
type snapUse struct {
	cache string // hit | miss | stale
	wait  time.Duration
}

// snapVers 是计算缓存键里的快照版本串。
func snapVers(snaps map[string]*refineSnap, cities []string) string {
	var b strings.Builder
	for _, c := range cities {
		if sn := snaps[c]; sn != nil {
			b.WriteString(strconv.FormatUint(sn.ver, 10))
		}
		b.WriteByte(',')
	}
	return b.String()
}

// timingOf 汇总耗时:用到的几份快照里各段取最长的那份(它们是一批建的时候是同一个数)。
func timingOf(snaps map[string]*refineSnap, use snapUse, now time.Time, engine time.Duration) RefineTiming {
	ms := func(d time.Duration) float64 { return round3(float64(d) / float64(time.Millisecond)) }
	t := RefineTiming{EngineMS: ms(engine), WaitMS: ms(use.wait), Cache: use.cache}
	var read, aodpD, vol time.Duration
	oldest := now
	for _, sn := range snaps {
		read, aodpD, vol = max(read, sn.readDur), max(aodpD, sn.aodpDur), max(vol, sn.volDur)
		t.AODPWaited = t.AODPWaited || sn.aodpWaited
		if sn.at.Before(oldest) {
			oldest = sn.at
		}
	}
	t.ReadMS, t.AODPMS, t.VolumeMS = ms(read), ms(aodpD), ms(vol)
	t.SnapshotAgeS = round3(now.Sub(oldest).Seconds())
	return t
}

// staleLocked:这份快照该不该重建。调用方持有 snapMu。
func (r *refineState) staleLocked(sn *refineSnap, now, after time.Time) bool {
	age := now.Sub(sn.at)
	switch {
	case age < refineSnapMinGap:
		return false
	case age >= refineSnapTTL:
		return true
	case r.dirty[sn.key].After(sn.at):
		return true
	case after.After(sn.at):
		return true
	case now.Sub(sn.volsAt) >= refineSnapVolTTL:
		return true
	}
	// AODP 到期(或从没取到过):失败过的至少隔 refineSnapAODPRetry 再为它重建
	if sn.pricesAt.IsZero() || now.Sub(sn.pricesAt) >= refineSnapAODPTTL {
		return sn.aodpTried.IsZero() || now.Sub(sn.aodpTried) >= refineSnapAODPRetry
	}
	return false
}

// ensureSnaps 拿到 line 在 cities 的快照:
//   - 有、干净、没到期:直接用(hit)
//   - 要重建:后台重建,最多等 refineSnapWait;等不到先用旧的(stale)
//   - 没有(冷启动):必须等,直到 ctx 到期(miss)
//
// after 非零时,读簿时刻比它旧的快照算要重建(客户端带来的 WS 报价时刻,不晚于现在)。
func (s *Service) ensureSnaps(ctx context.Context, line refine.Line, cities []string,
	after time.Time) (map[string]*refineSnap, snapUse, error) {

	r := &s.refine
	now := r.now()
	if after.After(now) {
		after = now
	}
	s.lazyWarm()
	out := make(map[string]*refineSnap, len(cities))
	var need []string
	cold := false
	r.snapMu.Lock()
	for _, c := range cities {
		k := snapKey{line.Key, c}
		r.active[k] = now
		sn := r.snaps[k]
		switch {
		case sn == nil:
			cold = true
			need = append(need, c)
		case r.staleLocked(sn, now, after):
			need = append(need, c)
			out[c] = sn
		default:
			out[c] = sn
		}
	}
	r.snapMu.Unlock()
	use := snapUse{cache: "hit"}
	if len(need) == 0 {
		return out, use, nil
	}

	t0 := time.Now()
	chans := s.startBuild(line, need)
	wctx := ctx
	if !cold {
		var cancel context.CancelFunc
		wctx, cancel = context.WithTimeout(ctx, refineSnapWait)
		defer cancel()
	}
	timedOut := false
	for _, c := range need {
		select {
		case <-chans[c]:
		case <-wctx.Done():
			timedOut = true
		}
		if timedOut {
			break
		}
	}
	use.wait = time.Since(t0)
	use.cache = "miss"

	r.snapMu.Lock()
	defer r.snapMu.Unlock()
	for _, c := range need {
		k := snapKey{line.Key, c}
		sn := r.snaps[k]
		switch {
		case sn == nil && timedOut:
			return nil, use, ctx.Err() // 冷启动等到了请求的期限:计算留在后台,过一会儿再要
		case sn == nil:
			if err := r.buildErr[k]; err != nil {
				return nil, use, err
			}
			return nil, use, fmt.Errorf("%s @ %s 的快照没建出来", line.Key, c)
		case sn == out[c]:
			use.cache = "stale" // 没等到,或者重建失败了:先用旧的
		default:
			out[c] = sn
		}
	}
	return out, use, nil
}

// startBuild 在后台重建 line 在 cities 的快照,返回每座城建完会关上的 channel。
// 已经在建的城不重复建,直接给那个 channel(singleflight);剩下的合成一批。
func (s *Service) startBuild(line refine.Line, cities []string) map[string]chan struct{} {
	r := &s.refine
	out := make(map[string]chan struct{}, len(cities))
	prev := map[string]*refineSnap{}
	var todo []string
	r.snapMu.Lock()
	for _, c := range cities {
		k := snapKey{line.Key, c}
		if ch, ok := r.building[k]; ok {
			out[c] = ch
			continue
		}
		ch := make(chan struct{})
		r.building[k] = ch
		out[c] = ch
		todo = append(todo, c)
		if sn := r.snaps[k]; sn != nil {
			prev[c] = sn
		}
	}
	r.snapMu.Unlock()
	if len(todo) == 0 {
		return out
	}
	go func() {
		// 和发起它的请求脱钩:请求断了,快照照样建完给下一次用
		bctx, cancel := context.WithTimeout(context.Background(), refineComputeTimeout)
		defer cancel()
		built, err := s.buildSnaps(bctx, line, todo, prev)
		if err != nil {
			slog.Warn("精炼快照没建出来", "line", line.Key, "cities", todo, "err", err)
		}
		r.snapMu.Lock()
		for _, c := range todo {
			k := snapKey{line.Key, c}
			if err != nil {
				r.buildErr[k] = err
			} else {
				r.snaps[k] = built[c]
				delete(r.buildErr, k)
			}
			close(r.building[k])
			delete(r.building, k)
		}
		r.snapMu.Unlock()
	}()
	return out
}

// buildSnaps 是真正的 I/O:一条 BookOrders(这些城 × 这条线全部物品 × 两边)、
// 一次 AODP(到期的城合在一个请求里)、每城一次成交量(到期的)。
func (s *Service) buildSnaps(ctx context.Context, line refine.Line, cities []string,
	prev map[string]*refineSnap) (map[string]*refineSnap, error) {

	r := &s.refine
	now := r.now().UTC()
	src := s.refineSource()
	items := s.snapItems(line)
	window := s.Cfg.Refining.Window()
	since := now.Add(-window)

	cold := false
	var needA []string
	for _, c := range cities {
		p := prev[c]
		if p == nil {
			cold = true
		}
		if p == nil || p.pricesAt.IsZero() || now.Sub(p.pricesAt) >= refineSnapAODPTTL {
			needA = append(needA, c)
		}
	}
	// AODP 和读簿互不相干,并行:AODP 慢的时候不拖着盘口一起等
	var fetched []aodp.PriceRecord
	var aerr error
	var aodpDur time.Duration
	waited := false
	aodpDone := make(chan struct{})
	go func() {
		defer close(aodpDone)
		if len(needA) > 0 {
			t1 := time.Now()
			fetched, waited, aerr = s.snapPrices(ctx, src, line, items, needA, now, cold)
			aodpDur = time.Since(t1)
		}
	}()

	// 串城的盘口边把 slack 放到整个读单窗口(见 rounds):窗口是 72h,不是扫描的 6h。
	// 续页只认隔得够近的(refinePageGap)
	t0 := time.Now()
	sb := &storeBooks{st: ordersOnly{src}, conflicts: s.Conflicts, slack: s.Cfg.SnapshotSlack(), window: window,
		pageGap: refinePageGap}
	sides, flagged, err := sb.readSides(ctx, refineKeys(items, cities, nil, ""), since, since)
	readDur := time.Since(t0)
	<-aodpDone
	if err != nil {
		return nil, fmt.Errorf("读抓包挂单: %w", err)
	}

	out := make(map[string]*refineSnap, len(cities))
	for _, c := range cities {
		p := prev[c]
		sn := &refineSnap{key: snapKey{line.Key, c}, at: now, readDur: readDur,
			sides: map[model.QuoteKey]book.Side{}, flagged: map[model.QuoteKey]bool{}}
		for k, v := range sides {
			if k.LocationID == c {
				sn.sides[k] = v
			}
		}
		for k, v := range flagged {
			if v && k.LocationID == c {
				sn.flagged[k] = true
			}
		}
		if slices.Contains(needA, c) {
			sn.aodpTried, sn.aodpDur, sn.aodpWaited = now, aodpDur, waited
			if aerr == nil {
				sn.pricesAt = now
				for _, rec := range fetched {
					if rec.City == c {
						sn.fetched = append(sn.fetched, rec)
					}
				}
			} else {
				sn.aodpErr = aerr
				if p != nil { // 沿用上一份取到的
					sn.fetched, sn.pricesAt = p.fetched, p.pricesAt
				}
			}
		} else {
			sn.fetched, sn.pricesAt, sn.aodpTried, sn.aodpErr = p.fetched, p.pricesAt, p.aodpTried, p.aodpErr
		}
		sn.prices = map[refineCell]aodp.PriceRecord{}
		for _, rec := range s.mergePrices(items, sn.fetched, now) {
			if rec.Quality == refineQuality && rec.City == c {
				sn.prices[refineCell{rec.ItemID, c}] = rec
			}
		}
		if p != nil && now.Sub(p.volsAt) < refineSnapVolTTL {
			sn.vols, sn.volsAt = p.vols, p.volsAt
		} else {
			t2 := time.Now()
			vols, err := s.refineVolumes(ctx, line.Targets(), c, now)
			if err != nil {
				return nil, err
			}
			sn.vols, sn.volsAt, sn.volDur = vols, now, time.Since(t2)
		}
		sn.ver = r.ver.Add(1)
		out[c] = sn
	}
	return out, nil
}

// snapPrices 取这条线在 cities 的 AODP 当前价(和定时扫描、查价页共用一个客户端和限流器)。
// 按 物品 × 城市 缓存 60 秒,并发的同样请求合并。
//   - AODP 已知连不上(aodpWait 判定 down):不打,直接报连不上,沿用上一份
//   - 冷启动只等 min(aodpWait, 3s);后台重建等满 aodpWait。没等到的请求留在后台跑完,
//     回来以后把这几座城标脏,下一次(或后台预热)重建时直接命中缓存
func (s *Service) snapPrices(ctx context.Context, src RefineSource, line refine.Line, items, cities []string,
	now time.Time, cold bool) ([]aodp.PriceRecord, bool, error) {

	wait, down := s.aodpWait(now)
	if down != nil {
		return nil, false, down
	}
	if cold {
		wait = min(wait, refineSnapColdAODPWait)
	}
	key := strings.Join(items, ",") + "|" + strings.Join(cities, ",")
	fetch := func(c context.Context) ([]aodp.PriceRecord, error) { return src.AODPPrices(c, items, cities) }
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	recs, hit, err := s.refine.prices.get(wctx, key, fetch)
	if err != nil {
		slog.Warn("精炼快照 AODP 当前价没取到", "line", line.Key, "cities", cities, "err", err)
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			go s.aodpFollowUp(line, cities, key, fetch)
		}
	}
	return recs, !hit, err
}

// aodpFollowUp 等那次没等到的 AODP 请求跑完,成功了就把这几座城标脏:下一次请求或预热会重建,
// 重建时 AODP 直接命中 60 秒缓存。
func (s *Service) aodpFollowUp(line refine.Line, cities []string, key string,
	fetch func(context.Context) ([]aodp.PriceRecord, error)) {

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, _, err := s.refine.prices.get(ctx, key, fetch); err == nil {
		s.markSnapsDirty(line, cities)
	}
}

// snapItems 是快照覆盖的物品:这条线 T2~T8 的全部原料、半成品和心,再并上 25 个目标
// ItemsFor 收集到的(防配方表里出现线外的输入)。按 配方表 × 线 缓存。
func (s *Service) snapItems(line refine.Line) []string {
	r := &s.refine
	recipes := s.Recipes()
	key := fmt.Sprintf("%p|%s", recipes, line.Key)
	r.snapMu.Lock()
	if v, ok := r.items[key]; ok {
		r.snapMu.Unlock()
		return v
	}
	r.snapMu.Unlock()
	items := line.Items()
	for _, t := range line.Targets() {
		if its, err := refine.ItemsFor(recipes, t); err == nil {
			items = append(items, its...)
		}
	}
	items = uniqStrings(items)
	sort.Strings(items)
	r.snapMu.Lock()
	if len(r.items) > 32 { // 配方表换过很多次:旧的扔掉
		clear(r.items)
	}
	r.items[key] = items
	r.snapMu.Unlock()
	return items
}

// MarkRefineDirty 是 ingest 的 DirtyMarker 那一路:这个盘口有新单入库了(ingest 在 flush 提交之后
// 才标,所以重建时库里已经有)。只认品质 1、四条线里的物品;把 (线, 城市) 标脏、叫醒预热。
// 服务端 main 里要和 conflator 一起接上(ingest.New 的 DirtyMarker 换成两者的 fanout)。
func (s *Service) MarkRefineDirty(k model.QuoteKey) {
	if k.Quality != refineQuality {
		return
	}
	line, ok := refine.LineOfItem(k.ItemID)
	if !ok {
		return
	}
	s.markSnapsDirty(line, []string{k.LocationID})
}

func (s *Service) markSnapsDirty(line refine.Line, cities []string) {
	r := &s.refine
	r.init()
	now := r.now()
	r.snapMu.Lock()
	for _, c := range cities {
		r.dirty[snapKey{line.Key, c}] = now
	}
	r.snapMu.Unlock()
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// RunRefineWarm 是后台预热:先把四条线各自的专精城建好(打开精炼页第一下就是热的),
// 之后每 refineWarmTick、或者被标脏叫醒(防抖 1 秒),把 refineWarmIdle 以内有人用过、
// 又该重建的快照重建掉,同一条线的几座城合成一批。没人用就不碰库和 AODP。
// 同一个 Service 只跑一份;服务端 main 里 go flipper.RunRefineWarm(ctx)。
func (s *Service) RunRefineWarm(ctx context.Context) { s.runWarm(ctx, true) }

// lazyWarm:服务端 main 没接 RunRefineWarm 时,线上第一次用精炼页就在后台起一份(不预建)。
// 测试里(没有库、或者换了数据源)不起,免得后台 goroutine 搅乱读库计数。
func (s *Service) lazyWarm() {
	if s.Store == nil || s.RefineSrc != nil {
		return
	}
	s.refine.warmLazy.Do(func() { go s.runWarm(context.Background(), false) })
}

func (s *Service) runWarm(ctx context.Context, prebuild bool) {
	r := &s.refine
	r.init()
	if !r.warm.CompareAndSwap(false, true) {
		return
	}
	defer r.warm.Store(false)
	if prebuild {
		for _, l := range refine.Lines {
			waitBuilt(ctx, s.startBuild(l, []string{l.City}))
		}
	}
	t := time.NewTicker(refineWarmTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
			select {
			case <-ctx.Done():
				return
			case <-time.After(refineWarmDebounce):
			}
		case <-t.C:
		}
		s.warmOnce(ctx)
	}
}

// warmOnce 重建一轮:最近 refineWarmIdle 以内用过、又该重建的快照。
func (s *Service) warmOnce(ctx context.Context) {
	r := &s.refine
	r.init()
	now := r.now()
	byLine := map[string][]string{}
	r.snapMu.Lock()
	for k, at := range r.active {
		if now.Sub(at) > refineWarmIdle {
			// 没人用了:快照也扔掉,内存不随看过的城市组合一直涨;再用时冷启动重建
			delete(r.active, k)
			if r.building[k] == nil {
				delete(r.snaps, k)
				delete(r.dirty, k)
			}
			continue
		}
		if sn := r.snaps[k]; sn == nil || r.staleLocked(sn, now, time.Time{}) {
			byLine[k.line] = append(byLine[k.line], k.city)
		}
	}
	r.snapMu.Unlock()
	lines := make([]string, 0, len(byLine))
	for l := range byLine {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	for _, lk := range lines {
		line, ok := refine.LineByKey(lk)
		if !ok {
			continue
		}
		cities := byLine[lk]
		sort.Strings(cities)
		waitBuilt(ctx, s.startBuild(line, cities))
	}
}

func waitBuilt(ctx context.Context, chans map[string]chan struct{}) {
	for _, ch := range chans {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}
