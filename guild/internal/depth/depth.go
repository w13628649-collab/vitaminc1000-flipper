// Package depth 回答"我要这么多货,实际成交均价是多少"。
//
// 最优价只是**第一件**的价格。要买 5000 件就得往下吃好几档,
// 真实成交均价一定比卖一差。只看最优价会系统性高估每一条机会——
// 而且越是想做大单,高估得越厉害。
//
// 这是抓包数据独有的能力:AODP 的 prices 端点只给价格不给数量,
// 拿它根本算不出这件事。
package depth

// Level 是盘口的一档。
type Level struct {
	Price int64 `json:"price"`
	Qty   int64 `json:"qty"`
}

// Fill 是吃掉目标数量之后的结果。
type Fill struct {
	// Want 是想要的数量,Got 是盘口实际能给的。Got < Want 说明吃穿了
	Want int64 `json:"want"`
	Got  int64 `json:"got"`
	// VWAP 是成交量加权的实际均价,Best 是最优价(第一档)
	VWAP float64 `json:"vwap"`
	Best int64   `json:"best"`
	// Worst 是吃到的最差那一档的价格
	Worst int64 `json:"worst"`
	// Slippage 是 (VWAP - Best) / Best,买入为正、卖出取绝对值。
	// 这个数直接回答"做大单要多付多少代价"
	Slippage float64 `json:"slippage"`
	Total    float64 `json:"total"`
	// Levels 是实际吃到的档位,界面上画阶梯图
	Levels []Level `json:"levels"`
	// Filled 为 false 表示盘口不够深,拿不到想要的量
	Filled bool `json:"filled"`
}

// Walk 按价格从优到劣吃掉 want 件。
//
// 调用方负责把 levels 排好序:买入(吃卖单)时价格升序,
// 卖出(吃买单)时价格降序。函数本身不关心方向,只顺序吃——
// 这样同一份逻辑两边都能用。
func Walk(levels []Level, want int64) Fill {
	f := Fill{Want: want}
	if want <= 0 || len(levels) == 0 {
		return f
	}
	f.Best = levels[0].Price

	remaining := want
	for _, l := range levels {
		if remaining <= 0 {
			break
		}
		if l.Qty <= 0 {
			continue
		}
		take := l.Qty
		if take > remaining {
			take = remaining
		}
		f.Levels = append(f.Levels, Level{Price: l.Price, Qty: take})
		f.Total += float64(l.Price) * float64(take)
		f.Worst = l.Price
		f.Got += take
		remaining -= take
	}
	if f.Got > 0 {
		f.VWAP = f.Total / float64(f.Got)
		if f.Best > 0 {
			f.Slippage = (f.VWAP - float64(f.Best)) / float64(f.Best)
			if f.Slippage < 0 {
				f.Slippage = -f.Slippage // 卖单方向价格递减,滑点取绝对值
			}
		}
	}
	f.Filled = f.Got >= want
	return f
}

// MaxQtyWithin 是"不超过这个价的前提下最多能吃多少"。
//
// 另一个问法:与其问"5000 件要多少钱",不如问"我只接受到 1100 为止,
// 那能吃到几件"。挂单党更常问后面这个。
func MaxQtyWithin(levels []Level, limit int64, buying bool) (qty int64, cost float64) {
	for _, l := range levels {
		if buying && l.Price > limit {
			break
		}
		if !buying && l.Price < limit {
			break
		}
		qty += l.Qty
		cost += float64(l.Price) * float64(l.Qty)
	}
	return qty, cost
}
