// Package protocol 把 Photon 消息翻译成本项目的数据。
//
// opcode 值取自 ao-data/albiondata-client 的枚举(MIT,见 NOTICE),
// 与 Nouuu/Albion-Online-OpenRadar 的实测抓包交叉核对过
// (evNewCharacter=29、evMove=3 两边一致)。
//
// **这些数值会随游戏版本漂移。** 2026-06-29 那次 patch 在上游枚举里插了两条,
// 248 及以上的 code 全部 +2。所以它们做成变量而不是常量,
// 将来可以由服务端下发,不用重新发版。
package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"albion-guild/internal/model"
)

// Protocol18 下真实的 opcode 不在 Photon 的 Code 字节里:
// event 的真码在 params[252],operation 的在 params[253]。
const (
	paramEventCode     = 252
	paramOperationCode = 253
)

// OpCodes 是当前版本的操作码。发现对不上时改这里,不用动解析逻辑。
//
// **是 int 不是 byte。** 客户端码表里操作码排到 555、事件码排到 711,
// 线上 params[253] 是 int16。以前按 byte 存、取参数时也截成 byte,
// 258(LaborerTakeJobLoot)截完是 2,被当成 Join,拿它的垃圾参数改写了
// 角色名和所在地——之后翻到的挂单全记到错的城市上
type OpCodes struct {
	AuctionGetOffers       int // 卖单列表
	AuctionGetRequests     int // 买单列表
	Join                   int // 登录/切区,带角色和位置
	GetGameServerByCluster int // 换游戏服务器(老的切区信号)
	ChangeCluster          int // 切区:响应 key0 是目的地
	GetClusterMapInfo      int // 区域地图(N 键):全区建筑和制造站费率

	// 个人账本(ledger.go)
	AuctionBuyOffer                int // 即时买:吃别人的卖单
	AuctionSellSpecificItemRequest int // 即时卖:卖给别人的买单
	GetMailInfos                   int // 邮件列表
	ReadMail                       int // 读一封邮件
}

// DefaultOpCodes 对应 albiondata-client 12ff34e(2026-09-16)那一版的枚举,
// 197 取自客户端 1.32.030.345319 的码表(docs/client-data/photon-codes.json)。
func DefaultOpCodes() OpCodes {
	return OpCodes{
		AuctionGetOffers:       81,
		AuctionGetRequests:     82,
		Join:                   2,
		GetGameServerByCluster: 17,
		ChangeCluster:          41,
		GetClusterMapInfo:      197,

		AuctionBuyOffer:                83,
		AuctionSellSpecificItemRequest: 315,
		GetMailInfos:                   174,
		ReadMail:                       176,
	}
}

// PriceDivisor 是挂单价的换算除数:包里的 UnitPriceSilver 是 ×10000 的定点数。
//
// 已实测(2026-09-21 抓包):T6_METALBAR_LEVEL4@4 @ Thetford 原文
// 3,299,970,000,游戏里显示 329,997。albiondata-client 不除,是因为它把原文
// 原样传给 AODP(我们截到的上传里就是 3,299,970,000),除法在 AODP 服务端做。
// 我们的服务端只认银币(见 market_order_live.unit_price 的注释),所以在这里除。
// 不除的话,抓包价比 AODP 高一万倍,两路一合并就是满屏假机会
var PriceDivisor int64 = model.PriceScale

// rawOrder 是包里那段 JSON 的原样结构。
//
// 市场订单在 Photon 参数里本身就是 JSON 字符串,不需要解二进制——
// 这是这套协议里最省事的一块。
type rawOrder struct {
	ID               int64  `json:"Id"`
	ItemTypeID       string `json:"ItemTypeId"`
	LocationID       string `json:"LocationId"`
	QualityLevel     int16  `json:"QualityLevel"`
	EnchantmentLevel int16  `json:"EnchantmentLevel"`
	UnitPriceSilver  int64  `json:"UnitPriceSilver"`
	Amount           int32  `json:"Amount"`
	AuctionType      string `json:"AuctionType"`
	Expires          string `json:"Expires"`
}

// Parser 有状态:城市挂单的 LocationId 在包里是空的,得靠当前所在地补。
type Parser struct {
	Codes  OpCodes
	Events EventCodes
	// Version 是客户端版本号,写进原始样本:字段布局随版本变,样本得知道自己是哪一版
	Version string

	// allow 是业务层白名单索引(allowlist.go)。不在里面的消息记完样本就停,不进处理器
	allow allowIndex

	// 客户端给每张网卡起一个抓包 goroutine,但只有一个 Parser——
	// 这几个字段是被并发读写的。加锁不是为了性能,是为了正确:
	// 没有它,城市归属可能读到一半更新过的值
	mu            sync.Mutex
	location      string
	characterID   string
	characterName string
	// zoneReqs 是还在等响应的 op 197 请求(station.go)
	zoneReqs []zoneRequest
	// zoneDropped 是因为配不出区丢掉的 197 响应条数
	zoneDropped int64
	// sampled 是这次运行里每个 (方向, 码) 已经记了几条样本(sample.go)
	sampled map[ruleKey]int
	// ledger 是个人账本要的状态:挂单缓存、待确认的即时买卖、邮件类型(ledger.go)
	ledger ledgerState

	// clock 为 nil 时用 time.Now。留给测试钉时间
	clock func() time.Time

	// OnOrders 收到一批挂单时回调。
	OnOrders func(orders []model.MarketOrder)
	// OnIdentity 角色或位置变化时回调。
	OnIdentity func(characterID, characterName, location string)
	// OnMarketRequest 看到 81/82 请求时回调,给加密判定做门控(CryptoTracker.MarketRequest)。
	OnMarketRequest func()
	// OnMarketResponded 认出一条明文的 81/82 响应时回调,不管里面有没有挂单
	// (CryptoTracker.MarketResponded)。空结果也要报:它说明这条响应不是密文
	OnMarketResponded func()
	// OnMarketDecoded 一条 81/82 响应处理完时回调,n 是 JSON 解开的挂单条数
	// (CryptoTracker.MarketDecoded)。位置不明被丢掉的也算在 n 里
	OnMarketDecoded func(n int)
	// OnStations 解出一次区域地图(op 197)里的建筑时回调。
	OnStations func(obs []model.StationObs)
	// OnSample 记下一条原始样本时回调。所有认得出码的消息都记,
	// 每个 (方向, 码) 每次运行最多 MaxSamplesPerCode 条(sample.go)
	OnSample func(s model.CaptureSample)

	// 个人账本(ledger.go):自己的一笔成交、银币余额变化、专注余额变化
	OnTrade  func(t model.MyTrade)
	OnWallet func(w model.WalletObs)
	OnFocus  func(f model.FocusObs)
}

func (p *Parser) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// SetClock 让解析器用给定的时钟取"现在"。离线回放用:挂单、建筑、样本的观测时间
// 应该是包上的抓包时间,不是回放那一刻的墙钟。nil 恢复用墙钟
func (p *Parser) SetClock(f func() time.Time) {
	p.mu.Lock()
	p.clock = f
	p.mu.Unlock()
}

func New() *Parser {
	return &Parser{Codes: DefaultOpCodes(), Events: DefaultEventCodes()}
}

func (p *Parser) Location() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.location
}

func (p *Parser) Character() (id, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.characterID, p.characterName
}

// HandleResponse 处理 operation 的响应。
//
// photonOp 是 Photon 那一层的操作码,不用:params[253] 才是 Albion 真正的操作码。
// 但**没有 253 的那条路也要认**,因为市场挂单走的是一条没有参数表的路:
//
// 挂单数据以字符串数组的形式占在 debug message 的位置上(见
// photon.dispatchResponse 的 []string 分支),photon 层把它包成
// params[0] 就直接回调了,里面不可能有 253。只认 253 的话这条路是死的
// —— 而 handleOrders 读的恰恰就是 params[0],也就是说唯一能拿到挂单的
// 那条路被堵住了。症状是"日志一切正常、库里一条挂单都没有"。
//
// 认出码之后先记全键样本(sample.go),再过业务层白名单(allowlist.go):
// 不在表里的到此为止,在表里的只把表内的键交给下面的 handle*。
func (p *Parser) HandleResponse(photonCode byte, params map[byte]any) {
	p.HandleResponseCode(photonCode, 0, params)
}

// HandleResponseCode 同 HandleResponse,多带 Photon 响应的 returnCode:
// 即时买卖要靠它判服务端是不是真的成交了(0 = 成功)
func (p *Parser) HandleResponseCode(_ byte, returnCode int16, params map[byte]any) {
	code, hasCode := intParam(params, paramOperationCode)
	if !hasCode {
		// 没有参数表。params[0] 是字符串数组就一定是挂单,没有别的响应长这样。
		// 方向靠 JSON 里的 AuctionType 判,本来就不需要操作码区分
		// offers / requests,所以按卖单列表那一行过白名单。
		//
		// 别的形状一律丢。以前退回用 Photon 层那个 byte 码查白名单,
		// 等于开了一道侧门:Photon 层码恰好是 2 / 197 的包能进 Join / 区域地图的处理
		if _, isOrders := stringSlice(params[0]); !isOrders {
			return
		}
		code = p.Codes.AuctionGetOffers
	}
	p.recordSample(KindResponse, code, params)
	rule, ok := p.allow.lookup(p.Codes, p.Events, KindResponse, code)
	if !ok {
		return
	}
	params = rule.filter(params)
	switch code {
	case p.Codes.AuctionGetOffers, p.Codes.AuctionGetRequests:
		// 先报"收到了一条能解析的明文市场响应",再看解出几条。空结果(筛选没货、
		// 翻过最后一页)也是明文响应,要关掉加密判定的窗口,不然窗口里混进一条
		// 无关密文,到期就把明文号判成加密
		if p.OnMarketResponded != nil {
			p.OnMarketResponded()
		}
		p.handleOrders(params)
	case p.Codes.Join:
		p.handleJoin(params)
	case p.Codes.ChangeCluster:
		if zone, ok := params[0].(string); ok && zone != "" {
			p.setLocation(zone)
		}
	case p.Codes.GetClusterMapInfo:
		p.handleClusterMap(params)
	case p.Codes.AuctionBuyOffer, p.Codes.AuctionSellSpecificItemRequest:
		p.handleInstantResponse(code, returnCode)
	case p.Codes.GetMailInfos:
		p.handleMailInfos(params)
	case p.Codes.ReadMail:
		p.handleReadMail(params)
	}
}

// HandleRequest 处理 operation 的请求(我们主动发出去的)。
// 切区是从请求里看出来的,响应里没有。
func (p *Parser) HandleRequest(_ byte, params map[byte]any) {
	code, ok := intParam(params, paramOperationCode)
	if !ok {
		return
	}
	p.recordSample(KindRequest, code, params)
	rule, ok := p.allow.lookup(p.Codes, p.Events, KindRequest, code)
	if !ok {
		return
	}
	params = rule.filter(params)
	switch code {
	case p.Codes.GetGameServerByCluster:
		if zone, ok := params[0].(string); ok && zone != "" {
			p.setLocation(zone)
		}
	case p.Codes.AuctionGetOffers, p.Codes.AuctionGetRequests:
		if p.OnMarketRequest != nil {
			p.OnMarketRequest()
		}
	case p.Codes.GetClusterMapInfo:
		zone, _ := params[0].(string)
		p.noteZoneRequest(zone, p.now())
	case p.Codes.AuctionBuyOffer:
		p.handleInstantRequest(code, model.TradeInstantBuy, 1, 2, params)
	case p.Codes.AuctionSellSpecificItemRequest:
		p.handleInstantRequest(code, model.TradeInstantSell, 4, 1, params)
	}
}

// HandleEvent 处理服务端推来的事件:记全键样本。
//
// 真码在 params[252];**没有 252 时真码就是 Photon 层的派发字节**。Move(事件 3)走的就是
// 这条路:它占全部事件的大头,服务端为了省字节不发 252。只认 252 的话,最多的这类事件
// 既认不出也记不了样本(拿真实录包跑出来的,见 cmd/pcapdump)。
//
// 还没有事件的类型化处理器——要用哪条事件,先看它的样本,再在 allowlist.go 加一行、
// 在这里加一个分支。
func (p *Parser) HandleEvent(photonCode byte, params map[byte]any) {
	code, ok := intParam(params, paramEventCode)
	if !ok {
		code = int(photonCode)
	}
	p.recordSample(KindEvent, code, params)
	rule, ok := p.allow.lookup(p.Codes, p.Events, KindEvent, code)
	if !ok {
		return
	}
	params = rule.filter(params)
	switch code {
	case p.Events.UpdateMoney:
		p.handleMoney(params)
	case p.Events.CraftingFocusUpdate:
		p.handleFocus(params)
	}
}

func (p *Parser) handleOrders(params map[byte]any) {
	raws, ok := stringSlice(params[0])
	if !ok || len(raws) == 0 {
		return
	}
	now := p.now() // 回放时是包上的时间
	orders := make([]model.MarketOrder, 0, len(raws))
	decoded := 0
	// 解开了几条就报几条,位置不明丢掉的也算:能解开 JSON 就说明是明文
	defer func() {
		if p.OnMarketDecoded != nil {
			p.OnMarketDecoded(decoded)
		}
	}()
	for _, s := range raws {
		var r rawOrder
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			slog.Debug("挂单 JSON 解析失败", "err", err)
			continue
		}
		decoded++
		side := model.SideOffer
		if r.AuctionType == "request" {
			side = model.SideRequest
		}
		loc := r.LocationID
		if loc == "" {
			// 城市挂单的 LocationId 是空的,用当前所在地补。
			// 走私贩巢穴/休息区自带(形如 xxxx@yyyy),不受影响。
			loc = p.Location()
		}
		if loc == "" {
			continue // 还不知道自己在哪,这批先丢掉,切次区就好了
		}
		orders = append(orders, model.MarketOrder{
			OrderID:    r.ID,
			ItemID:     r.ItemTypeID,
			LocationID: loc,
			Quality:    r.QualityLevel,
			Enchant:    r.EnchantmentLevel,
			Side:       side,
			UnitPrice:  toSilver(r.UnitPriceSilver),
			Amount:     r.Amount,
			ObservedAt: now,
		})
	}
	if len(orders) > 0 {
		p.cacheOrders(orders) // 即时买卖靠订单号查回物品和单价(ledger.go)
	}
	if len(orders) > 0 && p.OnOrders != nil {
		p.OnOrders(orders)
	}
}

func (p *Parser) handleJoin(params map[byte]any) {
	p.mu.Lock()
	// 真实的 Join 响应里 key1 是 16 字节的 Guid,不是字符串(OpenRadar 录包,
	// testdata/openradar)。以前只认字符串,角色 id 从来没读到过
	if v := guidParam(params[1]); v != "" {
		p.characterID = v
	}
	if v, ok := int64Param(params[0]); ok && v > 0 {
		p.ledger.selfObject = v
	}
	if v, ok := params[2].(string); ok {
		p.characterName = v
	}
	if v, ok := params[8].(string); ok && v != "" {
		p.location = v
	}
	id, name, loc := p.characterID, p.characterName, p.location
	p.mu.Unlock()

	// 回调在锁外调:调用方会去动它自己的状态,持锁调外部代码
	// 是死锁的经典配方
	if p.OnIdentity != nil {
		p.OnIdentity(id, name, loc)
	}
}

func (p *Parser) setLocation(loc string) {
	p.mu.Lock()
	if p.location == loc {
		p.mu.Unlock()
		return
	}
	p.location = loc
	id, name := p.characterID, p.characterName
	p.mu.Unlock()

	if p.OnIdentity != nil {
		p.OnIdentity(id, name, loc)
	}
}

// toSilver 定点数还原成银币,四舍五入。挂单原文都是整银 ×10000,
// 截断和四舍五入结果一样;留着是防将来出现非整银的价
func toSilver(raw int64) int64 {
	if PriceDivisor <= 1 {
		return raw
	}
	return (raw + PriceDivisor/2) / PriceDivisor
}

// intParam 取一个整数参数(操作码 / 事件码)。Photon 反序列化出来的整数类型
// 不固定(byte/int16/int32/int64 都可能,看数值大小和发送方声明),挨个试。
//
// **一律按 int 返回,不许截窄。** 码表里操作码到 555、事件码到 711,
// 截成 byte 的话 258 就成了 2(Join),整类误路由都从这里来
func intParam(params map[byte]any, key byte) (int, bool) {
	switch v := params[key].(type) {
	case byte:
		return int(v), true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case uint16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

// stringSlice 兼容 []string 和 []interface{} 两种形态。
func stringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		// 空数组也算(空结果页);非空但一个字符串都没有的不算
		return out, len(out) > 0 || len(s) == 0
	}
	return nil, false
}

// guidParam 把 Guid 参数转成 .NET 的标准字符串形式。线上是 16 字节,按 .NET
// new Guid(byte[]) 的布局:前三段小端、后 8 字节原序。转成这个形式才能和挂单 JSON 里
// SellerCharacterId 之类的字符串对得上。已经是字符串的原样返回,认不出返回空
func guidParam(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		if len(x) != 16 {
			return ""
		}
		return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
			x[3], x[2], x[1], x[0], x[5], x[4], x[7], x[6],
			x[8], x[9], x[10], x[11], x[12], x[13], x[14], x[15])
	}
	return ""
}
