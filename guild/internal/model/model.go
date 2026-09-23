// Package model 是客户端和服务端共用的数据定义。
//
// 放在一起是有意的:两边字段漂移是这类系统最常见的线上事故来源,
// 同一份 struct 直接消灭这个问题。客户端解析完 Photon 包后发的就是这些。
package model

import "time"

// Side 挂单方向。用 int8 而不是字符串,库里省空间、比较也快。
type Side int8

const (
	SideOffer   Side = 0 // 卖单:别人挂着卖,你想马上买到货就付这个价
	SideRequest Side = 1 // 买单:别人挂着收,你想马上出货就拿这个价
)

// PriceScale 是游戏协议里的定点数倍率。
//
// 银币、金币、学习点在 Photon 包里都是 ×10000 的整数
// (对应 StatisticsAnalysis 的 FixPoint.InternalFactor)。
// 客户端负责还原成银币再上传,服务端只认还原后的值。
const PriceScale = 10000

// MarketOrder 是一张挂单的一次观测。
//
// 关键字段是 Amount —— AODP 的 REST 接口给不了数量,
// 这是本地抓包相对它最大的优势。
type MarketOrder struct {
	OrderID    int64      `json:"order_id"`
	ItemID     string     `json:"item_id"`
	LocationID string     `json:"location_id"`
	Quality    int16      `json:"quality"`
	Enchant    int16      `json:"enchant"`
	Side       Side       `json:"side"`
	UnitPrice  int64      `json:"unit_price"`
	Amount     int32      `json:"amount"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ObservedAt time.Time  `json:"observed_at"`

	// RawLocationID 是收敛成城市名之前包里的原始地点 id(0007 这种)。
	// 服务端入库时填,客户端留空。收敛规则将来改了,有原始值才能重算
	RawLocationID string `json:"raw_location_id,omitempty"`
}

// Key 是这张挂单所属的行情键:同一个物品+城市+品质+方向算一个盘口。
// 订阅和 conflation 都按它来。
func (o MarketOrder) Key() QuoteKey {
	return QuoteKey{
		ItemID:     o.ItemID,
		LocationID: o.LocationID,
		Quality:    o.Quality,
		Side:       o.Side,
	}
}

// UploadBatch 是客户端一次上传的内容。
type UploadBatch struct {
	Reporter string        `json:"reporter"` // 哪个角色抓到的
	Orders   []MarketOrder `json:"orders"`
}

// QuoteKey 唯一标识一个盘口。
type QuoteKey struct {
	ItemID     string `json:"item_id"`
	LocationID string `json:"location_id"`
	Quality    int16  `json:"quality"`
	Side       Side   `json:"side"`
}

// String 给出订阅用的紧凑表示,前端也用同一套拼 key。
func (k QuoteKey) String() string {
	return k.ItemID + "|" + k.LocationID + "|" + itoa(int(k.Quality)) + "|" + itoa(int(k.Side))
}

// Quote 是推给前端的一条行情。字段名压到最短,高频推送下省的带宽很可观。
type Quote struct {
	Key    string    `json:"k"`
	Price  int64     `json:"p"` // 该盘口的最优价:卖单取最低,买单取最高
	Depth  int64     `json:"d"` // 该价位上的挂单量合计
	Orders int32     `json:"n"` // 该价位上有几张单
	At     time.Time `json:"t"`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// DiagEntry 是客户端报上来的一条诊断记录。
//
// 200 人规模下没法一个个远程看屏幕,客户端把警告和错误传回来才有的查。
type DiagEntry struct {
	Level     string         `json:"level"` // info | warn | error
	Message   string         `json:"message"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Timestamp time.Time      `json:"ts"`
}

// DiagBatch 是一次诊断上报。
type DiagBatch struct {
	ClientID  string      `json:"client_id"`
	Character string      `json:"character"`
	Version   string      `json:"version"`
	OS        string      `json:"os"`
	Entries   []DiagEntry `json:"entries"`
}
