// Package model 是客户端和服务端共用的数据定义。
//
// 放在一起是有意的:两边字段漂移是这类系统最常见的线上事故来源,
// 同一份 struct 直接消灭这个问题。客户端解析完 Photon 包后发的就是这些。
package model

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

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

	// SentAt 是客户端发出这一批时自己的钟。服务端用 now−SentAt 得出这台机器
	// 的时钟偏差,整批平移 ObservedAt:成员的 Windows 钟快几分钟很常见,
	// 不纠正的话 last_seen 会跑到未来,"同一眼"的判断和新鲜度全都失真。
	// 老客户端不带这个字段,解码后是零值,服务端就不纠偏。
	// 用 omitzero 而不是 omitempty:后者对 time.Time 这种 struct 不起作用
	SentAt time.Time `json:"sent_at,omitzero"`
	// ClientVersion 是上传方的版本号,老客户端为空
	ClientVersion string `json:"client_version,omitempty"`

	// Stations 是 op 197(区域地图)里的建筑和制造站费率。
	// omitempty:老服务端的 handleUpload 不开 DisallowUnknownFields,多一个字段它
	// 照样解;老客户端不带这个字段,新服务端解出来是 nil。两个方向都不坏
	Stations []StationObs `json:"stations,omitempty"`
	// Samples 是原始样本(还没解过布局的消息原样),兼容性同 Stations
	Samples []CaptureSample `json:"samples,omitempty"`
}

// 原始样本的消息方向,和 protocol.Kind.String() 一致
const (
	SampleOpResponse = "op_response"
	SampleEvent      = "event"
)

// CaptureSample 是一条原样记下来的消息,用来解还没解过的字段布局。
//
// Params 是整张参数表、每项带 Go 类型标签:[{"k":17,"t":"[]int32","v":[…]}, …]。
// 类型标签是要点——同一个字段线上可能是几种 Go 类型,解码器要照着写。
type CaptureSample struct {
	Code          int             `json:"code"`
	Kind          string          `json:"kind"` // op_response | event
	Params        json.RawMessage `json:"params"`
	ObservedAt    time.Time       `json:"observed_at"`
	ClientVersion string          `json:"client_version,omitempty"`
}

// StationObs 是一次区域地图响应(op 197)里一个建筑的观测。
//
// 数组按下标对齐成一个建筑一行;Index 就是那个下标,只在本次响应里有意义。
// 跨次观测认"是不是同一台站"用 BuildingKey(类型 + 坐标)。
//
// 字段类型按客户端声明(docs/client-data.md 第二点六节),**不照抄 AODP**:
// 它把 long[] 写成 []int,银币类会溢出;坐标写成 [][]int,丢精度。
// 没收到的数组对应字段留 nil,不补 0——"没观测到"和"观测到 0"意思不同
// (营养 0 的站不能用)。
type StationObs struct {
	// ZoneID 是这次 197 请求点名的区 id(key0),只收纯数字的
	ZoneID string `json:"zone_id"`
	Index  int    `json:"index"`
	// BuildingType 来自 key17(int[])
	BuildingType int32 `json:"building_type"`
	// Pos 来自 key18(float[][]),**只当"哪个建筑"的连接键**,不建地图层
	Pos []float64 `json:"pos,omitempty"`
	// Durability / Nutrition / Silver 来自 key20 / 22 / 24(long[]),只存原值。
	// 客户端里它们也是 a7m 定点类型,但比例没定,不归一化
	Durability *int64 `json:"durability,omitempty"`
	Nutrition  *int64 `json:"nutrition,omitempty"`
	Silver     *int64 `json:"silver,omitempty"`
	// Owner 来自 key25(string[])
	Owner string `json:"owner,omitempty"`

	// 两档使用费(每 100 营养)。key33 = Associate、key34 = Public **只有社区一个来源**,
	// 客户端侧区分不了,待一张游戏内截图钉死——所以两个都存。
	// 标度线上没定(a7m 定点比例 10000,还是整银):Raw 是原值,不带 Raw 的是归一化后的
	// 银币(Raw > 1000 按 /10000,否则原值;1000 是 buildings.xml 的 maxuseagefee)
	AssociateFeeRaw *int64   `json:"associate_fee_raw,omitempty"`
	AssociateFee    *float64 `json:"associate_fee,omitempty"`
	PublicFeeRaw    *int64   `json:"public_fee_raw,omitempty"`
	PublicFee       *float64 `json:"public_fee,omitempty"`

	// Key23 / Key31 含义未知,带类型标签原样存({"t":"[]int64","v":…}):
	// 数组和建筑数对得上就存这个建筑那一格,对不上存整个值
	Key23 json.RawMessage `json:"key23,omitempty"`
	Key31 json.RawMessage `json:"key31,omitempty"`

	ObservedAt time.Time `json:"observed_at"`
}

// BuildingKey 是"哪台站"的连接键:建筑类型 + 坐标(保留两位小数)。
// 没有坐标时退回响应里的下标——那只在同一次响应里可靠,有总比没有强。
func (s StationObs) BuildingKey() string {
	t := strconv.Itoa(int(s.BuildingType))
	if len(s.Pos) == 0 {
		return t + "#" + strconv.Itoa(s.Index)
	}
	parts := make([]string, len(s.Pos))
	for i, p := range s.Pos {
		parts[i] = strconv.FormatFloat(math.Round(p*100)/100, 'f', -1, 64)
	}
	return t + "@" + strings.Join(parts, ",")
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
	At     time.Time `json:"t"` // 这个价位最近一次被观测到的时刻(挂单的观测时刻,已平移到服务端钟)
	// Pushed 是服务端推出这条的时刻(conflator 的 tick),只有推送带,/api/quotes 快照不带。
	// 精炼页拿它当 after:它一定晚于这批单入库提交(提交之后才标脏、下一个 tick 才推),
	// 而 At 是观测时刻,比入库早几秒,拿 At 比快照读簿时刻会漏掉"观测早于快照、入库晚于快照"的单
	Pushed time.Time `json:"s,omitzero"`
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
