package refine

import (
	"math"
	"sort"
)

// Breakeven 是一种投入物的盈亏平衡价(诉求 #4):路线固定不变(买 / 做的拆分、专注开法、
// 心变体、转化路线、各档件数都不动),别的价格也不变,只让这种投入物的「到手单价」
// (买价 × 买方系数 + 运费 + 转化实付)整体上移,主出售方式下这一批利润正好归零时的到手均价。
//
// 推导:Total = Sold × (Net − TotalCost/N),归零 ⇔ TotalCost = N·Net,挂卖、吃买单一样。
// 余地 H = N·Net − TotalCost = N × primary.Unit;到手单价上移 δ,TotalCost 增加 δ·D_f,
// D_f = 这一批为它买进的件数 + 批末余料件数 × [IncludeTail 且余料不结转]。
// 结转(carry)时余料按取得成本估值,涨价不影响利润,所以不计;bid / zero 估值下余料的损失
// 跟着取得成本涨,要计。于是 x* = e_f + H ÷ D_f。站费、转化费、税、创建费都已经在
// Net 和 TotalCost 里,自动带上。
//
// 这是保守的下界:价格真涨了,引擎本可以换路线(换起点、改买现成、换心变体),
// 重新优化后的利润是若干条线性函数取最大、凸且递减,真实的盈亏平衡价只会 ≥ 这个值。
type Breakeven struct {
	// Item 是配方里的投入物;从别的起点转化来的按转化目标记(buyRec.forItem)
	Item string `json:"item"`
	// Qty 是 D_f:这一批为它买进的件数(含转化来的),加上要计价的批末余料
	Qty float64 `json:"qty"`
	// Now 是 e_f:现在的到手均价 =(买价 × 买方系数 + 运费 + 转化实付)÷ 件数
	Now float64 `json:"now"`
	// Unit 是 x*:到手均价涨到这里利润归零。≤ 0 时记 0 且 Never=true(白送也亏)
	Unit float64 `json:"unit"`
	// Headroom = (Unit − Now) ÷ Now:还能涨几成;负数 = 现在已经亏
	Headroom float64 `json:"headroom"`
	Never    bool    `json:"never,omitempty"`
}

// breakevens 按投入物算这条候选的盈亏平衡价,外加「所有买的料一起涨 s% 利润归零」的 s。
// 料不满、没有主出售方式的利润、或者主方式一件都卖不掉时没有意义,给空切片和 nil。
func (e *engine) breakevens(c *Candidate, ev *evalResult) ([]Breakeven, *float64) {
	out := []Breakeven{}
	if !ev.filled || c.primary == nil || c.primary.Sold <= 0 {
		return out, nil
	}
	N := float64(e.p.Qty)
	head := N * c.primary.Unit
	tailCounts := e.p.IncludeTail && e.p.Leftover != LeftoverCarry

	type acc struct{ qty, cost float64 }
	by := map[string]*acc{}
	var order []string
	buyAll := 0.0 // 所有买入的 买价 × 系数 + 运费(转化实付不跟着涨)
	for _, b := range ev.buys {
		a := by[b.forItem]
		if a == nil {
			a = &acc{}
			by[b.forItem] = a
			order = append(order, b.forItem)
		}
		a.qty += b.qty
		a.cost += b.cost + b.transport + b.route
		buyAll += b.cost + b.transport
	}
	tail := map[string]float64{}
	if tailCounts {
		for _, lo := range ev.leftovers {
			tail[lo.Item] += lo.Qty
			buyAll += lo.Cost
		}
	}
	for _, item := range order {
		a := by[item]
		if a.qty <= qtyEps {
			continue
		}
		now := a.cost / a.qty
		d := a.qty + tail[item]
		be := Breakeven{Item: item, Qty: d, Now: now, Unit: now + head/d}
		if be.Unit <= 0 {
			be.Unit, be.Never = 0, true
		}
		if now > 0 {
			be.Headroom = (be.Unit - now) / now
		}
		out = append(out, be)
	}
	// 花钱多的在前:主原料一般排第一;平局按 id,保证确定
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := out[i].Qty*out[i].Now, out[j].Qty*out[j].Now
		if math.Abs(ci-cj) > costEps {
			return ci > cj
		}
		return out[i].Item < out[j].Item
	})
	var all *float64
	if buyAll > costEps {
		v := head / buyAll
		all = &v
	}
	return out, all
}

// fillRawBreakevens 把推荐路线(第一条普通候选)的盈亏平衡价回填进「原料来路」表:
// 每种原料的到手均价上限,以及每个起点在生产城吃卖单的买价上限 = (x* − 转化实付) ÷ 买方系数。
// 外城还要再扣运费,界面按 MixedSplit.City 自己除。
func (e *engine) fillRawBreakevens(res *Result) {
	var rec *Candidate
	for i := range res.Candidates {
		if res.Candidates[i].Reference == "" {
			rec = &res.Candidates[i]
			break
		}
	}
	if rec == nil {
		return
	}
	for item, rs := range res.RawSources {
		var be *Breakeven
		for i := range rec.Breakeven {
			if rec.Breakeven[i].Item == item {
				x := rec.Breakeven[i]
				be = &x
				break
			}
		}
		if be == nil {
			continue
		}
		rs.Breakeven = be
		for i := range rs.Pure {
			v := 0.0
			if e.mkt.TakeAsk > 0 {
				v = math.Max(0, (be.Unit-rs.Pure[i].Route)/e.mkt.TakeAsk)
			}
			rs.Pure[i].MaxPrice = &v
		}
		res.RawSources[item] = rs
	}
}
