package depth

// FracLevel 是按小数件数吃到的一档。
type FracLevel struct {
	Price int64   `json:"price"`
	Qty   float64 `json:"qty"`
}

// FracFill 是 WalkFrac 的结果,字段含义和 Fill 一样,只是件数可以是小数。
type FracFill struct {
	Want   float64     `json:"want"`
	Got    float64     `json:"got"`
	VWAP   float64     `json:"vwap"`
	Best   int64       `json:"best"`
	Worst  int64       `json:"worst"`
	Total  float64     `json:"total"`
	Levels []FracLevel `json:"levels"`
	Filled bool        `json:"filled"`
}

// fracEps 是小数件数的比较容差。件数来自 N·n·(1−r) 这种连乘,
// 23.0415 − 23.0415 可能差出 1e-15,不能拿 == 判吃满。
const fracEps = 1e-9

// WalkFrac 是 Walk 的小数件数版本。
//
// 精炼按稳态算消耗:做 10 根 T8.4 钢条每根耗 5 × (1 − 0.539) = 2.304 个矿,
// 合计 23.04 个。按整数件吃会把 0.04 件进成 1 件、多算一档的价,
// 而返还的料本来就会顶掉下一轮的购买(规则表 C8:按期望值建模),所以这里按小数吃。
//
// 排序约定和 Walk 一样:调用方把 levels 排成从优到劣。
func WalkFrac(levels []Level, want float64) FracFill {
	f := FracFill{Want: want}
	if !(want > 0) || len(levels) == 0 {
		return f
	}
	remaining := want
	for _, l := range levels {
		if remaining <= fracEps {
			break
		}
		if l.Qty <= 0 {
			continue
		}
		if f.Best == 0 {
			f.Best = l.Price
		}
		take := float64(l.Qty)
		if take > remaining {
			take = remaining
		}
		f.Levels = append(f.Levels, FracLevel{Price: l.Price, Qty: take})
		f.Total += float64(l.Price) * take
		f.Worst = l.Price
		f.Got += take
		remaining -= take
	}
	if f.Got > 0 {
		f.VWAP = f.Total / f.Got
	}
	f.Filled = f.Got >= want-fracEps
	return f
}
