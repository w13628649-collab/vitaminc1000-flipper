package refine

import (
	"math"
	"time"

	"albion-guild/internal/depth"
)

// 出售(设计 §2.3 ④)。
//
//   - 挂卖:(卖一 − undercut) × (1 − 税 − 创建费),即 ×0.935(F3)
//   - 吃买单:沿买单阶梯 × (1 − 税),即 ×0.96;遇到断崖就停。高 tier 半成品的买单常见这种形状:
//     前几档彼此差不到一成、一共只有几件,再往下一档就掉到买一的零头(占位单)

// sellNearPct 是挂卖时数「卖一附近有多少件在排队」的窗口,和 conf.Filters.NearPct 默认一致。
const sellNearPct = 0.05

// PostSell 是挂卖单。
type PostSell struct {
	BestAsk int64 `json:"best_ask"`
	// Price 是我挂的价:卖一 − undercut
	Price int64 `json:"price"`
	// Net 是每件到手
	Net float64 `json:"net"`
	// NearQty 是卖一 5% 以内已经在排队的件数:越多,挂上去越难轮到
	NearQty int64      `json:"near_qty"`
	SeenAt  *time.Time `json:"seen_at,omitempty"`
	AgeH    *float64   `json:"age_h,omitempty"`
	Source  string     `json:"source"`
}

// TakeFill 是吃买单卖 N 件(不过断崖)的结果。
type TakeFill struct {
	Want float64 `json:"want"`
	Got  float64 `json:"got"`
	VWAP float64 `json:"vwap"`
	// NetAvg 是每件到手均价 = VWAP × 0.96
	NetAvg float64 `json:"net_avg"`
}

// Cliff 是买单阶梯上的断崖:从这一档起价格掉到买一的 (1 − CliffPct) 以下。
type Cliff struct {
	Price int64 `json:"price"`
	// AfterQty 是断崖之前能卖的件数
	AfterQty float64 `json:"after_qty"`
	// Drop 是这一档相对买一的落差
	Drop float64 `json:"drop"`
}

// TakeSell 是吃买单。
type TakeSell struct {
	BestBid int64    `json:"best_bid"`
	Fill    TakeFill `json:"fill"`
	// MaxQty 是断崖之前最多能卖几件;深度未知时为 nil
	MaxQty    *float64   `json:"max_qty"`
	Cliff     *Cliff     `json:"cliff,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	SeenAt    *time.Time `json:"seen_at,omitempty"`
	AgeH      *float64   `json:"age_h,omitempty"`
	Source    string     `json:"source"`
}

// SellInfo 是目标产物在出售城市的两种卖法。缺价的那种为 nil。
type SellInfo struct {
	City  string    `json:"city"`
	Modes []string  `json:"modes"`
	Post  *PostSell `json:"post"`
	Take  *TakeSell `json:"take"`
}

func (e *engine) sellInfo() SellInfo {
	si := SellInfo{City: e.p.Sell.City, Modes: e.p.Sell.Modes}
	item, city := e.p.Target, e.p.Sell.City
	if l, ok := e.side(item, city, true); ok {
		best := l.levels[0].Price
		// 挂价不能压到 0:卖一本身就是 1 银时再压 1 银就没意义了,按 1 银算
		price := max(1, best-e.p.Fees.Undercut)
		ps := &PostSell{BestAsk: best, Price: price, Net: float64(price) * e.mkt.PostAsk, Source: l.source}
		if !l.depthUnknown {
			ps.NearQty = depth.Analyze(l.levels, sellNearPct).QtyNear
		}
		// 挂卖价跟的是卖一:新鲜度按卖一那一档的观测时刻
		ps.SeenAt, ps.AgeH = e.seenAge(l.seenAt(0))
		si.Post = ps
	}
	if l, ok := e.side(item, city, false); ok {
		best := l.levels[0].Price
		ts := &TakeSell{BestBid: best, Truncated: l.truncated, Source: l.source}
		ts.SeenAt, ts.AgeH = e.seenAge(l.seenAt(0))
		want := float64(e.p.Qty)
		if l.depthUnknown {
			// 只有价格没有件数:按买一全卖得掉算,并不知道哪里是断崖
			ts.Fill = TakeFill{Want: want, Got: want, VWAP: float64(best), NetAvg: float64(best) * e.mkt.TakeBid}
		} else {
			var usable []depth.Level
			acc := 0.0
			for _, lv := range l.levels {
				if float64(lv.Price) < float64(best)*(1-e.p.Sell.CliffPct) {
					ts.Cliff = &Cliff{Price: lv.Price, AfterQty: acc, Drop: 1 - float64(lv.Price)/float64(best)}
					break
				}
				usable = append(usable, lv)
				acc += float64(lv.Qty)
			}
			ts.MaxQty = &acc
			f := depth.WalkFrac(usable, want)
			ts.Fill = TakeFill{Want: want, Got: f.Got, VWAP: f.VWAP, NetAvg: f.VWAP * e.mkt.TakeBid}
			// 吃了几档,新鲜度按其中最旧的那一档(usable 是 levels 的前缀,下标对得上)
			if old := l.oldestSeen(len(f.Levels)); !old.IsZero() {
				ts.SeenAt, ts.AgeH = e.seenAge(old)
			}
		}
		si.Take = ts
	}
	return si
}

// seenAge 把观测时刻转成 JSON 里的 seen_at / age_h;零值时刻两个都是 nil。
func (e *engine) seenAge(t time.Time) (*time.Time, *float64) {
	age, ok := e.ageHours(t)
	if !ok {
		return nil, nil
	}
	tt := t.UTC()
	age = math.Round(age*1000) / 1000
	return &tt, &age
}
