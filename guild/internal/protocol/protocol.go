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
	"time"

	"github.com/ludy/albion-guild/internal/model"
)

// Protocol18 下真实的 opcode 不在 Photon 的 Code 字节里:
// event 的真码在 params[252],operation 的在 params[253]。
const (
	paramEventCode     = 252
	paramOperationCode = 253
)

// OpCodes 是当前版本的操作码。发现对不上时改这里,不用动解析逻辑。
type OpCodes struct {
	AuctionGetOffers   byte // 卖单列表
	AuctionGetRequests byte // 买单列表
	Join               byte // 登录/切区,带角色和位置
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

// PriceDivisor 是挂单价的换算除数。
//
// **待实测确认。** 游戏协议里货币是 ×10000 的定点整数
// (SA 的 FixPoint.InternalFactor),邮件解析里确实除了 10000;
// 但 albiondata-client 对挂单 JSON 是直接反序列化、没有除。
// 两条路径是否同单位代码里看不出来,先按 ADC 的行为(不除),
// 抓一条真实数据对照游戏内显示后再定。
var PriceDivisor int64 = 1

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

func (p *Parser) Location() string { return p.location }
func (p *Parser) Character() (id, name string) {
	return p.characterID, p.characterName
}

// HandleResponse 处理 operation 的响应。
func (p *Parser) HandleResponse(_ byte, params map[byte]any) {
	code, ok := byteParam(params, paramOperationCode)
	if !ok {
		return
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
			loc = p.location
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
			UnitPrice:  r.UnitPriceSilver / PriceDivisor,
			Amount:     r.Amount,
			ObservedAt: now,
		})
	}
	if len(orders) > 0 && p.OnOrders != nil {
		p.OnOrders(orders)
	}
}

func (p *Parser) handleJoin(params map[byte]any) {
	if v, ok := params[1].(string); ok {
		p.characterID = v
	}
	if v, ok := params[2].(string); ok {
		p.characterName = v
	}
	if v, ok := params[8].(string); ok && v != "" {
		p.location = v
	}
	if p.OnIdentity != nil {
		p.OnIdentity(p.characterID, p.characterName, p.location)
	}
}

func (p *Parser) setLocation(loc string) {
	if p.location == loc {
		return
	}
	p.location = loc
	if p.OnIdentity != nil {
		p.OnIdentity(p.characterID, p.characterName, p.location)
	}
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
