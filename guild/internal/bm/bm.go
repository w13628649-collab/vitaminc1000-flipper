// Package bm 解黑市倒卖的分配问题:主城各品质的卖单(秒买进货)分别卖进黑市哪个品质的买单,
// 总利润最大。设计见 docs/black-market.md。
//
// 两条游戏规则决定了它不能每个品质各算各的:
//   - 高品质的货能卖进低品质的买单(杰出卖进收普通的单照样成交)
//   - 黑市每张买单要的件数有限,同一张单被几座城的货抢时只能算一次
//
// 所以是一个带容量的二部图最大收益流:左边是主城卖单的每一档(城市 × 品质 × 价),
// 右边是黑市买单的每一档(品质 × 价),货的品质不低于单的品质才连边,
// 边上每件的收益 = 黑市价扣税 − 主城价。
//
// 贪心(先满足最贵的黑市单,给它配最便宜的货)会错:最便宜的那件高品质货被低品质的单先拿走,
// 后面的高品质单就没货了(见 TestSolve_贪心会错的那种)。开源工具 mattias-wiberg/AlbionFlipper
// 是暴力枚举 6⁵ 种组合、假设每档只有一单;我们有抓包的件数,直接解流。
//
// 纯函数:不连库、不看钟。
package bm

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// Ask 是主城的一档卖单:秒买进货付这个价。
type Ask struct {
	City    string
	Quality int
	Price   int64
	Qty     int64
	Seen    time.Time
	// Est:件数不知道(AODP 只给最优价),按 1 件算。装备的卖单多半一单一件,
	// 按 1 件算不会把这一行吹大;界面上写成"1+"
	Est bool
}

// Bid 是黑市的一档买单:卖给黑市拿这个价。
type Bid struct {
	Quality int
	Price   int64
	Qty     int64
	Seen    time.Time
	Est     bool // 同 Ask.Est
}

// Fill 是分配结果里的一笔:从 Ask 那档买 Qty 件,卖进 Bid 那档。
type Fill struct {
	Ask  Ask
	Bid  Bid
	Qty  int64
	Unit int64 // 每件赚:Net(Bid.Price) − Ask.Price
}

// Net 是卖进黑市一件到手多少。黑市是吃 NPC 的买单,只交销售税,不收创建费。
func Net(price int64, tax float64) int64 {
	return price - int64(math.Round(float64(price)*tax))
}

// Solve 求一个物品的最优分配:总利润最大,每档卖单、每档黑市买单都不超过自己的件数。
// 只出有利润的笔数,没有就是 nil。输入顺序不影响结果。
func Solve(asks []Ask, bids []Bid, tax float64) []Fill {
	asks, bids = slices.Clone(asks), slices.Clone(bids)
	slices.SortStableFunc(asks, func(a, b Ask) int {
		return cmp.Or(cmp.Compare(a.Price, b.Price), cmp.Compare(a.Quality, b.Quality), cmp.Compare(a.City, b.City))
	})
	slices.SortStableFunc(bids, func(a, b Bid) int {
		return cmp.Or(cmp.Compare(b.Price, a.Price), cmp.Compare(a.Quality, b.Quality))
	})

	// 只连有利润的边:主城价不比黑市扣税后低的那些档,多送一件就亏
	type arc struct {
		a, b int
		unit int64
	}
	var arcs []arc
	for i, a := range asks {
		if a.Qty <= 0 || a.Price <= 0 {
			continue
		}
		for j, b := range bids {
			if b.Qty <= 0 || b.Price <= 0 || a.Quality < b.Quality {
				continue
			}
			if u := Net(b.Price, tax) - a.Price; u > 0 {
				arcs = append(arcs, arc{i, j, u})
			}
		}
	}
	if len(arcs) == 0 {
		return nil
	}

	// 节点:0 是源,1..len(asks) 是卖单档,接着是黑市档,最后是汇
	nA := len(asks)
	src, sink := 0, nA+len(bids)+1
	f := newFlow(sink + 1)
	usedA, usedB := make([]bool, nA), make([]bool, len(bids))
	for _, c := range arcs {
		usedA[c.a], usedB[c.b] = true, true
	}
	for i, ok := range usedA {
		if ok {
			f.add(src, 1+i, asks[i].Qty, 0)
		}
	}
	for j, ok := range usedB {
		if ok {
			f.add(1+nA+j, sink, bids[j].Qty, 0)
		}
	}
	refs := make([]edgeRef, len(arcs))
	for k, c := range arcs {
		refs[k] = f.add(1+c.a, 1+nA+c.b, min(asks[c.a].Qty, bids[c.b].Qty), -c.unit)
	}
	f.run(src, sink)

	var out []Fill
	for k, c := range arcs {
		if q := f.flowOn(refs[k]); q > 0 {
			out = append(out, Fill{Ask: asks[c.a], Bid: bids[c.b], Qty: q, Unit: c.unit})
		}
	}
	return out
}

// Row 是采购单的一行:在 City 买 BuyQuality 的货 Qty 件,卖进黑市 SellQuality 的买单。
type Row struct {
	ItemID      string `json:"item_id"`
	City        string `json:"city"`
	BuyQuality  int    `json:"buy_quality"`
	SellQuality int    `json:"sell_quality"`
	Qty         int64  `json:"qty"`
	// BuyLow / BuyHigh 是要吃的主城卖单价位范围(只吃一档时两者相等),SellLow / SellHigh 是黑市那边
	BuyLow   int64 `json:"buy_low"`
	BuyHigh  int64 `json:"buy_high"`
	SellLow  int64 `json:"sell_low"`
	SellHigh int64 `json:"sell_high"`
	Cost     int64 `json:"cost"`    // 买入总额
	Revenue  int64 `json:"revenue"` // 卖进黑市到手,扣过税
	Profit   int64 `json:"profit"`
	// Margin = Profit / Cost
	Margin float64 `json:"margin"`
	// BuySeen / SellSeen 是用到的档里最旧的那一眼:数据龄按最旧的算
	BuySeen  time.Time `json:"buy_seen"`
	SellSeen time.Time `json:"sell_seen"`
	// BuyEst / SellEst:这一侧有档的件数不知道(AODP),Qty 只是下限
	BuyEst  bool `json:"buy_est,omitempty"`
	SellEst bool `json:"sell_est,omitempty"`
}

// Rows 把一个物品的分配按 (城市, 买的品质, 卖进的品质) 合成采购单的行,按这行总赚从大到小。
func Rows(itemID string, fills []Fill, tax float64) []Row {
	type key struct {
		city   string
		aq, bq int
	}
	idx := map[key]int{}
	var out []Row
	for _, f := range fills {
		k := key{f.Ask.City, f.Ask.Quality, f.Bid.Quality}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, Row{ItemID: itemID, City: k.city, BuyQuality: k.aq, SellQuality: k.bq,
				BuyLow: f.Ask.Price, BuyHigh: f.Ask.Price, SellLow: f.Bid.Price, SellHigh: f.Bid.Price,
				BuySeen: f.Ask.Seen, SellSeen: f.Bid.Seen})
		}
		r := &out[i]
		r.Qty += f.Qty
		r.Cost += f.Ask.Price * f.Qty
		r.Revenue += Net(f.Bid.Price, tax) * f.Qty
		r.BuyLow, r.BuyHigh = min(r.BuyLow, f.Ask.Price), max(r.BuyHigh, f.Ask.Price)
		r.SellLow, r.SellHigh = min(r.SellLow, f.Bid.Price), max(r.SellHigh, f.Bid.Price)
		r.BuySeen, r.SellSeen = older(r.BuySeen, f.Ask.Seen), older(r.SellSeen, f.Bid.Seen)
		r.BuyEst = r.BuyEst || f.Ask.Est
		r.SellEst = r.SellEst || f.Bid.Est
	}
	for i := range out {
		r := &out[i]
		r.Profit = r.Revenue - r.Cost
		if r.Cost > 0 {
			r.Margin = float64(r.Profit) / float64(r.Cost)
		}
	}
	SortRows(out)
	return out
}

// SortRows 按这单总赚从大到小;一样多的利润率高的在前,再按物品、城市、品质,顺序稳定。
//
// 不按利润率排:38% 只能做 1 件的不如 15% 能做 50 件的(docs/black-market.md)
func SortRows(rows []Row) {
	slices.SortStableFunc(rows, func(a, b Row) int {
		return cmp.Or(cmp.Compare(b.Profit, a.Profit), cmp.Compare(b.Margin, a.Margin),
			cmp.Compare(a.ItemID, b.ItemID), cmp.Compare(a.City, b.City),
			cmp.Compare(a.BuyQuality, b.BuyQuality), cmp.Compare(a.SellQuality, b.SellQuality))
	})
}

// Plan 是 Solve 再 Rows。
func Plan(itemID string, asks []Ask, bids []Bid, tax float64) []Row {
	return Rows(itemID, Solve(asks, bids, tax), tax)
}

func older(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}
