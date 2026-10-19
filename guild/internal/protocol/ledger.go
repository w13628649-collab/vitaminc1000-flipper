package protocol

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/model"
)

// 个人账本:自己的即时买卖、挂单成交(市场邮件)、银币和专注余额。
//
// 字段布局取自两份独立实现,并和我们按客户端元数据推出的键号逐个对过(docs/references.md):
// JPCodeCraft/AFMDataClientCore(AFM 数据客户端核心)、Triky313/AlbionOnline-StatisticsAnalysis。
// 真实样本到了再拿 cmd/pcapdump 核一次。
//
// 即时买卖的请求里只有订单号和件数,物品、品质、单价、地点要从最近看过的挂单里查——
// 你点"购买"之前一定先在列表里看到过那张单。请求先记成待确认,服务端回成功才算数。

// orderCacheSize 是按订单号缓存最近看过的挂单条数。一次翻页 50 张,够翻四十页
const orderCacheSize = 2000

// mailMetaSize 是缓存邮件列表元数据(类型、地点、收到时间)的条数。读邮件时靠它认类型
const mailMetaSize = 1000

// pendingTimeout:请求发出去这么久还没等到响应,就当它没成
const pendingTimeout = 30 * time.Second

type pendingTrade struct {
	code  int // 等哪个操作的响应
	trade model.MyTrade
	at    time.Time
}

type mailMeta struct {
	kind     string // 邮件类型,MARKETPLACE_SELLORDER_FINISHED_SUMMARY 这种
	location string
	received time.Time
}

// ledgerState 是个人账本要的解析状态。嵌在 Parser 里,由 Parser.mu 保护
type ledgerState struct {
	orders     map[int64]model.MarketOrder
	orderFIFO  []int64
	pending    *pendingTrade
	mails      map[int64]mailMeta
	mailFIFO   []int64
	selfObject int64 // 自己角色的对象 id(Join 响应 key0),认 UpdateMoney / 专注是不是自己的
	lastSilver int64
	lastFocus  float64
}

// cacheOrders 记下这批挂单,给即时买卖查订单号用。调用方不持锁
func (p *Parser) cacheOrders(orders []model.MarketOrder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ledger.orders == nil {
		p.ledger.orders = make(map[int64]model.MarketOrder, orderCacheSize)
	}
	for _, o := range orders {
		if _, seen := p.ledger.orders[o.OrderID]; !seen {
			p.ledger.orderFIFO = append(p.ledger.orderFIFO, o.OrderID)
		}
		p.ledger.orders[o.OrderID] = o
	}
	for len(p.ledger.orderFIFO) > orderCacheSize {
		delete(p.ledger.orders, p.ledger.orderFIFO[0])
		p.ledger.orderFIFO = p.ledger.orderFIFO[1:]
	}
}

// handleInstantRequest:即时买(op 83,key1 件数、key2 订单号)或即时卖
// (op 315,key1 订单号、key4 件数)发出去了,先记成待确认
func (p *Parser) handleInstantRequest(code int, kind string, amountKey, orderKey byte, params map[byte]any) {
	amount, okA := intParam(params, amountKey)
	orderID, okO := int64Param(params[orderKey])
	if !okA || !okO || amount <= 0 {
		slog.Debug("即时买卖请求缺件数或订单号", "code", code, "params", len(params))
		return
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	o, ok := p.ledger.orders[orderID]
	if !ok {
		// 没在列表里看到过这张单(比如客户端开在游戏之后、直接点了购买)。记不了价,跳过
		slog.Debug("即时买卖的订单不在缓存里", "order", orderID)
		p.ledger.pending = nil
		return
	}
	p.ledger.pending = &pendingTrade{code: code, at: now, trade: model.MyTrade{
		Kind: kind, Character: p.characterName, ItemID: o.ItemID, Quality: o.Quality,
		Amount: int32(amount), UnitPrice: o.UnitPrice, Total: o.UnitPrice * int64(amount),
		Location: o.LocationID, OrderID: orderID,
	}}
}

// handleInstantResponse:服务端回了。returnCode 0 才算成交;不是 0(单子被别人抢了之类)就作废
func (p *Parser) handleInstantResponse(code int, returnCode int16) {
	now := p.now()
	p.mu.Lock()
	pend := p.ledger.pending
	if pend == nil || pend.code != code {
		p.mu.Unlock()
		return
	}
	p.ledger.pending = nil
	p.mu.Unlock()
	if returnCode != 0 || now.Sub(pend.at) > pendingTimeout {
		return
	}
	t := pend.trade
	t.ObservedAt = now
	t.Key = fmt.Sprintf("instant:%d:%d", t.OrderID, now.UnixMilli())
	if p.OnTrade != nil {
		p.OnTrade(t)
	}
}

// handleMailInfos:邮件列表(op 174 响应)。key3 邮件 id、key7 地点、key11 类型、key12 收到时间(.NET ticks)。
// 只记市场相关的类型,读邮件时靠它认怎么拆正文
func (p *Parser) handleMailInfos(params map[byte]any) {
	ids, _ := int64Slice(params[3])
	locs, _ := stringSlice(params[7])
	kinds, _ := stringSlice(params[11])
	recv, _ := int64Slice(params[12])
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ledger.mails == nil {
		p.ledger.mails = make(map[int64]mailMeta, mailMetaSize)
	}
	for i, id := range ids {
		if i >= len(kinds) || mailTradeKind(kinds[i]) == "" {
			continue
		}
		m := mailMeta{kind: kinds[i]}
		if i < len(locs) {
			m.location = locs[i]
		}
		if i < len(recv) {
			m.received = fromTicks(recv[i])
		}
		if _, seen := p.ledger.mails[id]; !seen {
			p.ledger.mailFIFO = append(p.ledger.mailFIFO, id)
		}
		p.ledger.mails[id] = m
	}
	for len(p.ledger.mailFIFO) > mailMetaSize {
		delete(p.ledger.mails, p.ledger.mailFIFO[0])
		p.ledger.mailFIFO = p.ledger.mailFIFO[1:]
	}
}

// handleReadMail:读一封邮件(op 176 响应)。key0 邮件 id、key1 正文
func (p *Parser) handleReadMail(params map[byte]any) {
	id, okID := int64Param(params[0])
	body, okB := params[1].(string)
	if !okID || !okB {
		return
	}
	p.mu.Lock()
	m, ok := p.ledger.mails[id]
	who := p.characterName
	p.mu.Unlock()
	if !ok {
		return // 没见过它在列表里的类型,拆不了;不是市场邮件也走这里
	}
	t, err := parseMarketMail(m.kind, body)
	if err != nil {
		slog.Warn("市场邮件正文拆不开", "type", m.kind, "err", err)
		return
	}
	t.Key = "mail:" + strconv.FormatInt(id, 10)
	t.MailID, t.Character, t.Location, t.ObservedAt = id, who, m.location, m.received
	if t.ObservedAt.IsZero() {
		t.ObservedAt = p.now()
	}
	if p.OnTrade != nil {
		p.OnTrade(t)
	}
}

// mailTradeKind 把邮件类型映射成账本种类,不是市场邮件返回空
func mailTradeKind(mailType string) string {
	switch mailType {
	case "MARKETPLACE_SELLORDER_FINISHED_SUMMARY":
		return model.TradeOrderSold
	case "MARKETPLACE_BUYORDER_FINISHED_SUMMARY":
		return model.TradeOrderBought
	case "MARKETPLACE_SELLORDER_EXPIRED_SUMMARY":
		return model.TradeSellExpired
	case "MARKETPLACE_BUYORDER_EXPIRED_SUMMARY":
		return model.TradeBuyExpired
	case "BLACKMARKET_SELLORDER_EXPIRED_SUMMARY":
		return model.TradeBMSellExpired
	}
	return ""
}

// parseMarketMail 拆市场邮件正文。`|` 分隔,银币是 ×10000 的定点数,每种类型字段顺序不同:
//
//	卖单卖完   件数 | 物品 | 总银(税前) | 单价
//	买单买齐   件数 | 物品 | 实付总额 [| 挂单价]   单价用 总额÷件数,不用挂单价
//	卖单到期   卖出件数 | 挂单总件数 | 总银(税前) | 物品
//	买单到期   买到件数 | 挂单总件数 | 退款 | 物品   不给实付价,单价只能从退款反推
//
// 两份参考实现(AFM、StatisticsAnalysis)对位置的解法一致;卖单的总银是税前,
// StatisticsAnalysis 另外按税率扣,AFM 不扣——税我们在服务端入库时算
func parseMarketMail(mailType, body string) (model.MyTrade, error) {
	t := model.MyTrade{Kind: mailTradeKind(mailType)}
	if t.Kind == "" {
		return t, fmt.Errorf("不是市场邮件: %s", mailType)
	}
	f := strings.Split(body, "|")
	if len(f) < 3 {
		return t, fmt.Errorf("正文只有 %d 段: %q", len(f), body)
	}
	num := func(i int) (int64, error) {
		if i >= len(f) {
			return 0, fmt.Errorf("缺第 %d 段", i+1)
		}
		return strconv.ParseInt(strings.TrimSpace(f[i]), 10, 64)
	}
	switch t.Kind {
	case model.TradeOrderSold, model.TradeOrderBought:
		n, err := num(0)
		if err != nil {
			return t, err
		}
		total, err := num(2)
		if err != nil {
			return t, err
		}
		t.Amount, t.ItemID, t.Total = int32(n), strings.TrimSpace(f[1]), toSilver(total)
		t.UnitPrice = perUnit(total, n)
	case model.TradeSellExpired, model.TradeBMSellExpired, model.TradeBuyExpired:
		done, err := num(0)
		if err != nil {
			return t, err
		}
		all, err := num(1)
		if err != nil {
			return t, err
		}
		money, err := num(2)
		if err != nil {
			return t, err
		}
		if len(f) < 4 {
			return t, fmt.Errorf("到期邮件缺物品段: %q", body)
		}
		t.Amount, t.OrderAmount, t.ItemID = int32(done), int32(all), strings.TrimSpace(f[3])
		if t.Kind == model.TradeBuyExpired {
			// 退款是没买到的那部分按挂单价退回来的:单价 = 退款 ÷ 没买到的件数
			t.UnitPrice = perUnit(money, all-done)
			t.Total = t.UnitPrice * done
		} else {
			t.Total = toSilver(money)
			t.UnitPrice = perUnit(money, done)
		}
	}
	if t.ItemID == "" {
		return t, fmt.Errorf("物品为空: %q", body)
	}
	return t, nil
}

// perUnit 把 ×10000 的总额摊到每件,四舍五入到银币;件数为 0 返回 0
func perUnit(totalRaw, n int64) int64 {
	if n <= 0 {
		return 0
	}
	return int64(math.Round(float64(totalRaw) / float64(n) / float64(max(PriceDivisor, 1))))
}

// handleMoney:事件 UpdateMoney。key0 对象 id,key1 当前银币(×10000)。
// 键位:我们按元数据推的 k0/k1/k2 都是 long,StatisticsAnalysis 读 key1 当银币
func (p *Parser) handleMoney(params map[byte]any) {
	raw, ok := int64Param(params[1])
	if !ok {
		return
	}
	silver := toSilver(raw)
	p.mu.Lock()
	if !p.isSelfLocked(params[0]) || silver == p.ledger.lastSilver {
		p.mu.Unlock()
		return
	}
	p.ledger.lastSilver = silver
	obs := model.WalletObs{Character: p.characterName, Silver: silver}
	p.mu.Unlock()
	obs.ObservedAt = p.now()
	if p.OnWallet != nil {
		p.OnWallet(obs)
	}
}

// handleFocus:事件 CraftingFocusUpdate。key0 对象 id,key3 专注余额(float,不是定点数)。
// 专注会随时间回复,事件来得勤;变化不到 1 点不报
func (p *Parser) handleFocus(params map[byte]any) {
	bal, ok := floatParam(params[3])
	if !ok || bal < 0 || math.IsNaN(bal) || math.IsInf(bal, 0) {
		return
	}
	p.mu.Lock()
	if !p.isSelfLocked(params[0]) || math.Abs(bal-p.ledger.lastFocus) < 1 {
		p.mu.Unlock()
		return
	}
	p.ledger.lastFocus = bal
	obs := model.FocusObs{Character: p.characterName, Focus: bal}
	p.mu.Unlock()
	obs.ObservedAt = p.now()
	if p.OnFocus != nil {
		p.OnFocus(obs)
	}
}

// isSelfLocked 判断事件 key0 的对象 id 是不是自己。还不知道自己的 id(没抓到 Join)时
// 放行——这两条事件只会推给自己;知道了就严格比。调用方持锁
func (p *Parser) isSelfLocked(obj any) bool {
	if p.ledger.selfObject == 0 {
		return true
	}
	id, ok := int64Param(obj)
	return !ok || id == p.ledger.selfObject
}

// fromTicks 把 .NET ticks(100 纳秒,公元 1 年起)转成 time.Time
func fromTicks(ticks int64) time.Time {
	const unixEpochTicks = 621355968000000000
	if ticks <= unixEpochTicks {
		return time.Time{}
	}
	d := ticks - unixEpochTicks
	return time.Unix(d/10_000_000, (d%10_000_000)*100).UTC()
}

func int64Param(v any) (int64, bool) {
	switch x := v.(type) {
	case byte:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	}
	return 0, false
}

func floatParam(v any) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	if n, ok := int64Param(v); ok {
		return float64(n), true
	}
	return 0, false
}

// int64Slice 取整数数组。Photon 按数值大小可能发成 int[] 或 long[](AFM 两种都认)
func int64Slice(v any) ([]int64, bool) {
	switch x := v.(type) {
	case []int64:
		return x, true
	case []int32:
		out := make([]int64, len(x))
		for i, n := range x {
			out[i] = int64(n)
		}
		return out, true
	case []int16:
		out := make([]int64, len(x))
		for i, n := range x {
			out[i] = int64(n)
		}
		return out, true
	case []any:
		out := make([]int64, 0, len(x))
		for _, e := range x {
			n, ok := int64Param(e)
			if !ok {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	}
	return nil, false
}
