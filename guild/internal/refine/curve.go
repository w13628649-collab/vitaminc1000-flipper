package refine

import "math"

// curve 是「要 q 件,最少花多少钱」这个函数,用边际成本分段表示:每一段是
// 「再要 qty 件,每件 unit 银」,unit 从低到高排——凸的分段线性函数。
//
// 为什么用这种表示:半成品的「买 k 件 + 自炼其余」要在两条成本曲线之间精确分配。
// 买的成本沿卖单阶梯是凸分段线性;自炼的成本 = 原料曲线 + 下级料曲线 + 站费,
// 每一项也是凸分段线性,和也是。两条凸曲线分配同一个需求量,最优解就是把两边的
// 边际段按单价合并、取最便宜的 Q 件(下确卷积)。这比设计 §2.3 ② 里「只试卖单
// 各档的断点」更一般:自炼那条曲线自己也有拐点(原料吃到下一档),最优的 k 可能
// 落在那里,不在卖单的断点上。
type curve []seg

type seg struct {
	// qty 可以是 +Inf:深度未知的 AODP 价、不限量的挂买单
	qty  float64
	unit float64
	// buy:合并「买 / 做」两条曲线时,这一段来自买
	buy bool
}

// qtyEps 是件数的比较容差,和 depth.WalkFrac 一致。
const qtyEps = 1e-9

func (c curve) capacity() float64 {
	total := 0.0
	for _, s := range c {
		total += s.qty
	}
	return total
}

// cost 是前 q 件的总价;got < q 说明曲线不够长(深度不够)。
// buyQty 是其中来自 buy 段的件数。
func (c curve) cost(q float64) (total, got, buyQty float64) {
	remaining := q
	for _, s := range c {
		if remaining <= qtyEps {
			break
		}
		take := math.Min(s.qty, remaining)
		total += take * s.unit
		got += take
		if s.buy {
			buyQty += take
		}
		remaining -= take
	}
	return total, got, buyQty
}

// marginal 是第 q 件之后下一件的边际单价;曲线到头了返回 +Inf。
func (c curve) marginal(q float64) float64 {
	acc := 0.0
	for _, s := range c {
		acc += s.qty
		if acc > q+qtyEps {
			return s.unit
		}
	}
	return math.Inf(1)
}

// truncate 只留前 q 件。
func (c curve) truncate(q float64) curve {
	var out curve
	remaining := q
	for _, s := range c {
		if remaining <= qtyEps {
			break
		}
		if s.qty > remaining {
			s.qty = remaining
		}
		out = append(out, s)
		remaining -= s.qty
	}
	return out
}

// scale 把「要 x 件这种投入」的曲线换成「做 m 件产物」的曲线:每做 1 件要 a 件投入,
// 所以 m 件的成本 = C(a·m),边际段变成 (qty/a, unit·a)。
func (c curve) scale(a float64) curve {
	if a <= 0 {
		return curve{{qty: math.Inf(1), unit: 0}}
	}
	out := make(curve, len(c))
	for i, s := range c {
		out[i] = seg{qty: s.qty / a, unit: s.unit * a, buy: s.buy}
	}
	return out
}

// plus 给每件加一个固定成本(站费)。
func (c curve) plus(u float64) curve {
	out := make(curve, len(c))
	for i, s := range c {
		out[i] = seg{qty: s.qty, unit: s.unit + u, buy: s.buy}
	}
	return out
}

// sumCurves 是同一个自变量上几条曲线逐点相加:做 m 件,原料、下级料、心各花多少,
// 边际成本是各项边际之和。任何一项到头了,和也到头了。
func sumCurves(cs ...curve) curve {
	if len(cs) == 0 {
		return nil
	}
	acc := cs[0]
	for _, c := range cs[1:] {
		acc = add2(acc, c)
	}
	return acc
}

func add2(a, b curve) curve {
	var out curve
	i, j := 0, 0
	var ra, rb float64
	if len(a) > 0 {
		ra = a[0].qty
	}
	if len(b) > 0 {
		rb = b[0].qty
	}
	for i < len(a) && j < len(b) {
		q := math.Min(ra, rb)
		if q > qtyEps {
			out = append(out, seg{qty: q, unit: a[i].unit + b[j].unit})
		}
		if math.IsInf(q, 1) {
			break
		}
		ra -= q
		rb -= q
		if ra <= qtyEps {
			i++
			if i < len(a) {
				ra = a[i].qty
			}
		}
		if rb <= qtyEps {
			j++
			if j < len(b) {
				rb = b[j].qty
			}
		}
	}
	return out
}

// mergeCurves 是下确卷积:同一个需求量可以从 a 拿也可以从 b 拿,取最便宜的件。
// 单价相同时 a 的段在前(调用方把「买」放在 a:同价时买现成的,少一道工序)。
func mergeCurves(a, b curve) curve {
	out := make(curve, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if j >= len(b) || (i < len(a) && a[i].unit <= b[j].unit+costEps) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	return out
}

// markBuy 把整条曲线标成「来自买」。
func (c curve) markBuy() curve {
	out := make(curve, len(c))
	for i, s := range c {
		s.buy = true
		out[i] = s
	}
	return out
}
