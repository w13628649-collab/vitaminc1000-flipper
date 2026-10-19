package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"albion-guild/internal/clientui"
	"albion-guild/internal/model"
)

// maxPending 是断网时内存里最多堆多少条挂单。
// 一条大约 100 字节,10 万条约 10 MB——成员的机器扛得住,
// 再多就没意义了:市场数据本来就是越新越值钱。
const maxPending = 100_000

// maxPendingStations 是断网时最多堆多少条建筑观测。一个城一次地图几十到几百个建筑,
// 5000 条够攒几十次开图;费率变得慢,丢最旧的没损失
const maxPendingStations = 5_000

// maxPendingSamples 是最多堆多少条原始样本。明文全抓之后一次运行会冒出一两百个码,
// protocol 那边每个码最多 20 条;进区、登录那一刻是一大波,正好是最想要的那批
// (阿瓦隆之路的门就是进区时推的)。队列满了从最老的丢,所以要够装下开局那一波
const maxPendingSamples = 5000

// maxPendingLedger 是个人账本(成交、银币、专注)最多堆多少条。一个人一天几百条,
// 这个数只是断网很久时的兜底
const maxPendingLedger = 20_000

// uploader 攒一批再发,不是每解析出一条就发一次。
type uploader struct {
	server string
	token  string
	client *http.Client

	mu       sync.Mutex
	pending  []model.MarketOrder
	stations []model.StationObs
	samples  []model.CaptureSample
	// 个人账本。量很小,每批全部带上,不参与限额和拆分
	trades    []model.MyTrade
	wallet    []model.WalletObs
	focus     []model.FocusObs
	spec      []model.SpecObs
	character string
	location  string
	seen      int64
	uploaded  int64
	// stationsUploaded 是传上去的建筑观测条数
	stationsUploaded int64
	lastAt           time.Time
	lastErr          string
	// limits 是下一批最多拿几条,零值 = fullLimits
	limits batchLimits
}

// snapshot 取当前状态给界面看。只填客户端自己知道的那几项,
// 版本、网卡数之类由调用方补。
func (u *uploader) snapshot() clientui.Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := clientui.Status{
		Character:        u.character,
		Location:         u.location,
		OrdersSeen:       u.seen,
		OrdersUploaded:   u.uploaded,
		StationsUploaded: u.stationsUploaded,
		Pending:          len(u.pending),
		LastError:        u.lastErr,
		ServerOK:         u.lastErr == "",
	}
	if !u.lastAt.IsZero() {
		st.LastUploadAt = u.lastAt.Format(time.RFC3339)
	}
	return st
}

func (u *uploader) add(orders []model.MarketOrder) {
	u.mu.Lock()
	u.pending = append(u.pending, orders...)
	u.seen += int64(len(orders))
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addTrade(t model.MyTrade) {
	u.mu.Lock()
	u.trades = append(u.trades, t)
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addWallet(w model.WalletObs) {
	u.mu.Lock()
	u.wallet = append(u.wallet, w)
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addFocus(f model.FocusObs) {
	u.mu.Lock()
	u.focus = append(u.focus, f)
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addSpec(s model.SpecObs) {
	u.mu.Lock()
	u.spec = append(u.spec, s)
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addStations(obs []model.StationObs) {
	u.mu.Lock()
	u.stations = append(u.stations, obs...)
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addSample(s model.CaptureSample) {
	u.mu.Lock()
	u.samples = append(u.samples, s)
	u.trimLocked()
	u.mu.Unlock()
}

// requeue 把发失败的一批退回队列头部,并记下错误给界面看。
func (u *uploader) requeue(b model.UploadBatch, err error) {
	u.mu.Lock()
	u.pending = append(b.Orders, u.pending...)
	u.stations = append(b.Stations, u.stations...)
	u.samples = append(b.Samples, u.samples...)
	u.trades = append(b.Trades, u.trades...)
	u.wallet = append(b.Wallet, u.wallet...)
	u.focus = append(b.Focus, u.focus...)
	u.spec = append(b.Spec, u.spec...)
	u.trimLocked()
	u.lastErr = err.Error()
	u.mu.Unlock()
}

// trimLocked 超上限就丢最旧的。调用方必须持锁。
func (u *uploader) trimLocked() {
	u.pending = trimOldest(u.pending, maxPending)
	u.stations = trimStations(u.stations, maxPendingStations)
	u.samples = trimOldest(u.samples, maxPendingSamples)
	u.trades = trimOldest(u.trades, maxPendingLedger)
	u.wallet = trimOldest(u.wallet, maxPendingLedger)
	u.focus = trimOldest(u.focus, maxPendingLedger)
	u.spec = trimOldest(u.spec, maxPendingLedger)
}

func trimOldest[T any](s []T, limit int) []T {
	if n := len(s) - limit; n > 0 {
		return append(s[:0], s[n:]...)
	}
	return s
}

// 建筑观测要整次扫描一起走:一次 op 197 响应是整区的全部建筑,服务端拿「最新那次里没有」
// 判一台站拆了(flip.pickStationFee)。从一次扫描中间截开,存下的半次会把另一半当成没了。
// 同一次响应的建筑区和观测时刻都一样(protocol.decodeStations)
func sameScan(a, b model.StationObs) bool {
	return a.ZoneID == b.ZoneID && a.ObservedAt.Equal(b.ObservedAt)
}

// scanCut 把切点 n 挪到扫描边界上:往前退到这次扫描的开头;一次扫描本身就比 n 长时往后挪到它的结尾
func scanCut(st []model.StationObs, n int) int {
	if n <= 0 || n >= len(st) || !sameScan(st[n-1], st[n]) {
		return n
	}
	k := n
	for k > 0 && sameScan(st[k-1], st[k]) {
		k--
	}
	if k > 0 {
		return k
	}
	for k = n; k < len(st) && sameScan(st[k-1], st[k]); k++ {
	}
	return k
}

// trimStations 超上限时从最旧的整次扫描丢起
func trimStations(st []model.StationObs, limit int) []model.StationObs {
	n := len(st) - limit
	if n <= 0 {
		return st
	}
	for n < len(st) && sameScan(st[n-1], st[n]) {
		n++
	}
	return append(st[:0], st[n:]...)
}

func (u *uploader) setIdentity(name, location string) {
	u.mu.Lock()
	u.character, u.location = name, location
	u.mu.Unlock()
}

func (u *uploader) run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.flush(ctx)
		}
	}
}

// 一批上传的上限。服务端 handleUpload 的 MaxBytesReader 是 8 MiB,超了回 400、
// 整批作废。以前 flush 一次把整个积压塞进一个请求:服务端断几分钟、恢复后那一批
// 就可能超限,而且不看状态码,挂单、建筑、样本一起静默丢掉,顶栏还显示已上传
const (
	maxBatchBytes    = 4 << 20 // 留一半余量
	maxBatchOrders   = 20_000  // 一条约 100 字节,约 2 MB
	maxBatchStations = 2_000
	maxBatchSamples  = 100 // 单条几百字节到几十 KB,100 条离 maxBatchBytes 还远;超了 encode 会拆
	// maxBatchesPerFlush 是一次 flush 最多连发几批:积压能尽快补完,又不至于一直占着
	maxBatchesPerFlush = 20
)

// batchLimits 是这一批最多拿几条。服务端回 400/413 时减半,发成功一次翻倍回去
type batchLimits struct{ orders, stations, samples int }

var fullLimits = batchLimits{maxBatchOrders, maxBatchStations, maxBatchSamples}

// halve 按被拒的那批减半;这批里没有的那种不动
func (l batchLimits) halve(b model.UploadBatch) batchLimits {
	if l.orders <= 0 {
		l = fullLimits
	}
	half := func(cur, n int) int {
		if n == 0 {
			return cur
		}
		return max(1, n/2)
	}
	return batchLimits{half(l.orders, len(b.Orders)), half(l.stations, len(b.Stations)),
		half(l.samples, len(b.Samples))}
}

func (l batchLimits) grow() batchLimits {
	if l.orders <= 0 {
		return fullLimits
	}
	return batchLimits{min(fullLimits.orders, l.orders*2), min(fullLimits.stations, l.stations*2),
		min(fullLimits.samples, l.samples*2)}
}

func batchItems(b model.UploadBatch) int {
	return len(b.Orders) + len(b.Stations) + len(b.Samples) + len(b.Trades) + len(b.Wallet) + len(b.Focus) + len(b.Spec)
}

// takeLocked 从队列头拿一批,不超过 limits。调用方必须持锁
func (u *uploader) takeLocked() model.UploadBatch {
	l := u.limits
	if l.orders <= 0 {
		l = fullLimits
	}
	n, s, m := min(len(u.pending), l.orders), min(len(u.stations), l.stations), min(len(u.samples), l.samples)
	s = scanCut(u.stations, s) // 建筑整次扫描一起发
	// SentAt 让服务端算出这台机器的钟差并整批平移 ObservedAt。服务端的 touch
	// 已改为记观测时间而不是服务端时刻,不带 SentAt 的话钟慢的成员的单会提前过期。
	// 每次发送现填:退回队列重发的那批也要用这一次发出的时刻,里面的旧单才保持旧
	b := model.UploadBatch{Reporter: u.character,
		Orders:   append([]model.MarketOrder(nil), u.pending[:n]...),
		Stations: append([]model.StationObs(nil), u.stations[:s]...),
		Samples:  append([]model.CaptureSample(nil), u.samples[:m]...),
		// 个人账本整批带走:量小,不值得参与限额
		Trades: u.trades, Wallet: u.wallet, Focus: u.focus, Spec: u.spec,
		SentAt: time.Now(), ClientVersion: version}
	u.pending, u.stations, u.samples = u.pending[n:], u.stations[s:], u.samples[m:]
	u.trades, u.wallet, u.focus, u.spec = nil, nil, nil, nil
	return b
}

// flush 把队列按批发完(最多 maxBatchesPerFlush 批),遇到失败就停,剩下的等下次。
func (u *uploader) flush(ctx context.Context) {
	for range maxBatchesPerFlush {
		u.mu.Lock()
		batch := u.takeLocked()
		u.mu.Unlock()
		// 只有建筑或样本、没有挂单也要发:开一次地图不翻市场是常态。
		// 以前这里挂单为空就 return
		if batchItems(batch) == 0 {
			return
		}
		if !u.send(ctx, batch) {
			return
		}
	}
}

// encode 序列化一批,并保证体积不超过 maxBatchBytes。
//
// 超了就把后一半退回队列重来;序列化失败(protocol 已经挡住 NaN 坐标,这里是兜底)
// 时先摘掉建筑和样本只发挂单——以前这里直接 return,队列在前面已经清空,
// 同一批的挂单、建筑、样本全部丢掉,界面上也看不出出了问题。
// 返回 nil 表示这批没东西可发了
func (u *uploader) encode(batch *model.UploadBatch) []byte {
	for {
		body, err := json.Marshal(batch)
		if err != nil {
			if len(batch.Stations) > 0 || len(batch.Samples) > 0 {
				slog.Error("序列化失败,丢掉这批的建筑和样本、只发挂单", "err", err,
					"stations", len(batch.Stations), "samples", len(batch.Samples))
				batch.Stations, batch.Samples = nil, nil
				if len(batch.Orders) == 0 {
					return nil
				}
				continue
			}
			slog.Error("挂单序列化失败,丢掉这批", "err", err, "count", len(batch.Orders))
			u.mu.Lock()
			u.lastErr = "序列化失败:" + err.Error()
			u.mu.Unlock()
			return nil
		}
		if len(body) <= maxBatchBytes || batchItems(*batch) <= 1 {
			return body
		}
		u.splitBack(batch)
	}
}

// splitBack 把这批每种各一半退回队列头,自己留前一半。
func (u *uploader) splitBack(b *model.UploadBatch) {
	no, ns, nm := (len(b.Orders)+1)/2, scanCut(b.Stations, (len(b.Stations)+1)/2), (len(b.Samples)+1)/2
	back := model.UploadBatch{Orders: b.Orders[no:], Stations: b.Stations[ns:], Samples: b.Samples[nm:]}
	b.Orders, b.Stations, b.Samples = b.Orders[:no], b.Stations[:ns], b.Samples[:nm]
	// 一种都拆不动(每种最多一条)时只能整条推回一种,不然死循环
	if batchItems(back) == 0 {
		switch {
		case len(b.Samples) > 0:
			back.Samples, b.Samples = b.Samples, nil
		case len(b.Stations) > 0:
			back.Stations, b.Stations = b.Stations, nil
		}
	}
	u.mu.Lock()
	u.pending = append(back.Orders, u.pending...)
	u.stations = append(back.Stations, u.stations...)
	u.samples = append(back.Samples, u.samples...)
	u.trimLocked()
	u.mu.Unlock()
}

// send 发一批。返回 false 表示这次 flush 该停了(失败、退回了队列)。
func (u *uploader) send(ctx context.Context, batch model.UploadBatch) bool {
	body := u.encode(&batch)
	if body == nil {
		return true
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		u.server+"/api/upload", bytes.NewReader(body))
	if err != nil {
		// 服务端地址配错会走到这。以前这里直接 return,整批挂单
		// 连同后面每一批都被静默吞掉,界面上还一直显示"抓包中"
		u.requeue(batch, err)
		slog.Error("构造上传请求失败,检查 -server 地址", "server", u.server, "err", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		// 网络不好就把这批退回队列,下次一起发。
		// 断网期间数据堆在内存里,连上自动补——这是 HTTP 比 WS 省事的地方
		u.requeue(batch, err)
		slog.Warn("上传失败,已退回队列", "count", len(batch.Orders),
			"stations", len(batch.Stations), "samples", len(batch.Samples), "err", err)
		return false
	}
	defer resp.Body.Close()

	// 以前不看状态码:400/413/5xx 都算成功,整批丢掉、lastErr 清空、顶栏显示已上传
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		herr := fmt.Errorf("服务端回 %d:%s", resp.StatusCode, strings.TrimSpace(string(msg)))
		switch {
		case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge:
			// 多半是太大(MaxBytesReader 超限回 400)。拆小重发;只剩一条还被拒,
			// 那条就是坏的,丢掉,不然它会永远堵在队列头
			if batchItems(batch) <= 1 {
				u.mu.Lock()
				u.lastErr = herr.Error() + "(单条被拒,已丢弃)"
				u.mu.Unlock()
				slog.Error("上传被拒,单条丢弃", "status", resp.StatusCode, "err", herr)
				return true
			}
			u.mu.Lock()
			u.limits = u.limits.halve(batch)
			u.mu.Unlock()
		}
		u.requeue(batch, herr)
		slog.Warn("上传被服务端拒绝,已退回队列", "status", resp.StatusCode, "count", len(batch.Orders),
			"stations", len(batch.Stations), "samples", len(batch.Samples), "err", herr)
		return false
	}

	// stations 用服务端确认存下的条数:老服务端忽略 stations、新服务端存失败也回 200,
	// 按发出去的条数计,顶栏那句"另传工作站观测 N 条"就是虚的
	var out struct {
		Changed, Touched  int
		Stations, Samples *int
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	saved := 0
	if out.Stations != nil {
		saved = *out.Stations
	} else if len(batch.Stations) > 0 {
		slog.Warn("服务端没确认建筑观测(老版本服务端?),不计入已上传", "stations", len(batch.Stations))
	}

	u.mu.Lock()
	u.uploaded += int64(len(batch.Orders))
	u.stationsUploaded += int64(saved)
	u.lastAt = time.Now()
	u.lastErr = ""
	u.limits = u.limits.grow()
	u.mu.Unlock()
	slog.Info("已上传", "sent", len(batch.Orders), "changed", out.Changed, "touched", out.Touched,
		"stations", len(batch.Stations), "stations_saved", saved,
		"samples", len(batch.Samples), "samples_saved", deref(out.Samples),
		"trades", len(batch.Trades), "wallet", len(batch.Wallet), "focus", len(batch.Focus), "spec", len(batch.Spec))
	return true
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
