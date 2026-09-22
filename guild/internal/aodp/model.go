// Package aodp 是 AODP REST 接口的模型和客户端。
//
// AODP 的时间戳不带时区,实际是 UTC。空数据用 price=0 / date=0001-01-01 表示,
// 这两个哨兵必须显式处理,否则 0 价会被当成"史上最低价"。
package aodp

import (
	"encoding/json"
	"strings"
	"time"
)

// C# DateTime.Ticks 纪元偏移。REST v2 返回 ISO 字符串,NATS 流返回 ticks。
const (
	ticksEpochOffset = 621_355_968_000_000_000
	ticksPerSecond   = 10_000_000
)

// emptySentinel 是 AODP 表示"没有数据"用的 0001-01-01。
var emptySentinel = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// Stamp 是 AODP 的时间戳。零值表示"没有数据",而不是"很久以前"——
// 这两者的区别决定了一条记录是被过滤还是被当成新鲜价。
type Stamp struct{ T time.Time }

func (s Stamp) Valid() bool { return !s.T.IsZero() }

// AgeHours 返回距今多少小时。没有数据时第二个返回值是 false。
func (s Stamp) AgeHours(now time.Time) (float64, bool) {
	if !s.Valid() {
		return 0, false
	}
	return now.Sub(s.T).Seconds() / 3600, true
}

func (s Stamp) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return []byte("null"), nil
	}
	return json.Marshal(s.T)
}

// UnmarshalJSON 同时认 ISO 字符串和 C# ticks 数字。
func (s *Stamp) UnmarshalJSON(b []byte) error {
	text := strings.TrimSpace(string(b))
	if text == "null" || text == `""` {
		return nil
	}
	if text[0] != '"' {
		var ticks float64
		if err := json.Unmarshal(b, &ticks); err != nil {
			return err
		}
		s.T = keepIfReal(time.Unix(0, int64((ticks-ticksEpochOffset)/ticksPerSecond*1e9)).UTC())
		return nil
	}
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.T = keepIfReal(ParseTime(raw))
	return nil
}

func keepIfReal(t time.Time) time.Time {
	if t.IsZero() || !t.After(emptySentinel) {
		return time.Time{}
	}
	return t.UTC()
}

// isoLayouts 是 AODP 见过的几种写法。不带时区的一律当 UTC。
var isoLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseTime 解析 AODP 的时间字符串。解析不出来返回零值,
// 调用方据此判定"没有数据"。
func ParseTime(raw string) time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}
	}
	text = strings.Replace(text, "Z", "+00:00", 1)
	for _, layout := range isoLayouts {
		if t, err := time.Parse(layout, text); err == nil {
			return t.UTC()
		}
	}
	// 上面那几个 layout 都要求带偏移量或者完全不带,
	// "+00:00" 这种补出来的尾巴单独再试一次
	if t, err := time.Parse("2006-01-02T15:04:05.999999999-07:00", text); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// PriceRecord 是 /api/v2/stats/prices 的一行:某物品在某城市的当前挂单价。
//
// 注意:这个端点**不返回挂单数量**。"最低卖价 1000"可能只对应 1 件货,
// 这是 troll 过滤必须存在的根本原因。
type PriceRecord struct {
	ItemID  string `json:"item_id"`
	City    string `json:"city"`
	Quality int    `json:"quality"`

	SellPriceMin     int64 `json:"sell_price_min"`
	SellPriceMinDate Stamp `json:"sell_price_min_date"`
	SellPriceMax     int64 `json:"sell_price_max"`
	SellPriceMaxDate Stamp `json:"sell_price_max_date"`
	BuyPriceMin      int64 `json:"buy_price_min"`
	BuyPriceMinDate  Stamp `json:"buy_price_min_date"`
	BuyPriceMax      int64 `json:"buy_price_max"`
	BuyPriceMaxDate  Stamp `json:"buy_price_max_date"`
}

type HistoryPoint struct {
	ItemCount int64 `json:"item_count"`
	AvgPrice  int64 `json:"avg_price"`
	Timestamp Stamp `json:"timestamp"`
}

// HistorySeries 是 /api/v2/stats/history 的一组:某物品在某城市的逐日成交。
//
// ItemCount **只统计卖单成交**,拿不到买单成交量——实际流动性比这个数字高,
// 所以用它算可吃量是偏保守的一侧。
type HistorySeries struct {
	Location string         `json:"location"`
	ItemID   string         `json:"item_id"`
	Quality  int            `json:"quality"`
	Data     []HistoryPoint `json:"data"`
}
