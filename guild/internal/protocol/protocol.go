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
type OpCodes struct {
	AuctionGetOffers       byte // 卖单列表
	AuctionGetRequests     byte // 买单列表
	Join                   byte // 登录/切区,带角色和位置
	GetGameServerByCluster byte // 切区
}

// DefaultOpCodes 对应 albiondata-client 12ff34e(2026-09-16)那一版的枚举。
func DefaultOpCodes() OpCodes {
	return OpCodes{
		AuctionGetOffers:       81,
		AuctionGetRequests:     82,
		Join:                   2,
		GetGameServerByCluster: 17,
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
	Codes OpCodes

	// 客户端给每张网卡起一个抓包 goroutine,但只有一个 Parser——
	// 这几个字段是被并发读写的。加锁不是为了性能,是为了正确:
	// 没有它,城市归属可能读到一半更新过的值
	mu            sync.Mutex
	location      string
	characterID   string
	characterName string

	// OnOrders 收到一批挂单时回调。
	OnOrders func(orders []model.MarketOrder)
	// OnIdentity 角色或位置变化时回调。
	OnIdentity func(characterID, characterName, location string)
}

func New() *Parser {
	return &Parser{Codes: DefaultOpCodes()}
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
// HandleResponse 处理 operation 的响应。
//
// photonOp 是 Photon 那一层的操作码,params[253] 是 Albion 真正的操作码。
// **两个都要认**,因为市场挂单走的是一条没有参数表的路:
//
// 挂单数据以字符串数组的形式占在 debug message 的位置上(见
// photon.dispatchResponse 的 []string 分支),photon 层把它包成
// params[0] 就直接回调了,里面不可能有 253。只认 253 的话这条路是死的
// —— 而 handleOrders 读的恰恰就是 params[0],也就是说唯一能拿到挂单的
// 那条路被堵住了。症状是"日志一切正常、库里一条挂单都没有"。
func (p *Parser) HandleResponse(photonOp byte, params map[byte]any) {
	code, hasCode := byteParam(params, paramOperationCode)
	if !hasCode {
		// 没有参数表。params[0] 是字符串数组就一定是挂单,没有别的响应长这样。
		// 方向靠 JSON 里的 AuctionType 判,本来就不需要操作码区分
		// offers / requests
		if _, isOrders := stringSlice(params[0]); isOrders {
			p.handleOrders(params)
			return
		}
		code = photonOp // 退回 Photon 层那个操作码
	}
	switch code {
	case p.Codes.AuctionGetOffers, p.Codes.AuctionGetRequests:
		p.handleOrders(params)
	case p.Codes.Join:
		p.handleJoin(params)
	}
}

// HandleRequest 处理 operation 的请求(我们主动发出去的)。
// 切区是从请求里看出来的,响应里没有。
func (p *Parser) HandleRequest(_ byte, params map[byte]any) {
	code, ok := byteParam(params, paramOperationCode)
	if !ok || code != p.Codes.GetGameServerByCluster {
		return
	}
	if zone, ok := params[0].(string); ok && zone != "" {
		p.setLocation(zone)
	}
}

func (p *Parser) handleOrders(params map[byte]any) {
	raws, ok := stringSlice(params[0])
	if !ok || len(raws) == 0 {
		return
	}
	now := time.Now()
	orders := make([]model.MarketOrder, 0, len(raws))
	for _, s := range raws {
		var r rawOrder
		if err := json.Unmarshal([]byte(s), &r); err != nil {
			slog.Debug("挂单 JSON 解析失败", "err", err)
			continue
		}
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
	if len(orders) > 0 && p.OnOrders != nil {
		p.OnOrders(orders)
	}
}

func (p *Parser) handleJoin(params map[byte]any) {
	p.mu.Lock()
	if v, ok := params[1].(string); ok {
		p.characterID = v
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

// byteParam 取一个数值参数。Photon 反序列化出来的整数类型不固定
// (byte/int16/int32 都可能),挨个试。
func byteParam(params map[byte]any, key byte) (byte, bool) {
	switch v := params[key].(type) {
	case byte:
		return v, true
	case int16:
		return byte(v), true
	case int32:
		return byte(v), true
	case int64:
		return byte(v), true
	case int:
		return byte(v), true
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
		return out, len(out) > 0
	}
	return nil, false
}
