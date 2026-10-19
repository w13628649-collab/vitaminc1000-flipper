package flip

import (
	"context"
	"slices"
	"testing"

	"albion-guild/internal/refine"
)

// 料的盘口有限、越买越贵:这一批的利润先涨后跌,最好的件数在中间,不是越多越好
func TestRefineQtyScan找最赚的件数(t *testing.T) {
	s, _ := newRefineService(barFixture())
	p := s.RefineDefaults(tBar, "")
	res, err := s.RefineQtyScan(context.Background(), p, RefineOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Best == nil || res.Best.Qty <= 1 || res.Best.Qty >= refine.MaxQty || !res.Best.Filled || res.Unbounded {
		t.Fatalf("最好的件数该在中间: best %+v unbounded %v", res.Best, res.Unbounded)
	}
	if !slices.IsSortedFunc(res.Points, func(a, b RefineQtyPoint) int { return a.Qty - b.Qty }) {
		t.Fatal("points 要按件数排")
	}
	for _, pt := range res.Points {
		if pt.Filled && pt.Total > res.Best.Total {
			t.Fatalf("%d 件比 best(%d 件)还赚: %+v", pt.Qty, res.Best.Qty, pt)
		}
	}
	if res.Current == nil || res.Current.Qty != p.Qty {
		t.Fatalf("current 该是请求的 %d 件: %+v", p.Qty, res.Current)
	}
	// 和单算那个件数是同一个数
	pq := p
	pq.Qty = res.Best.Qty
	single := mustRefine(t, s, pq)
	var top float64
	for _, c := range single.Candidates {
		if c.Reference == "" && c.Filled && c.Profit.Post != nil && c.Profit.Post.Total > top {
			top = c.Profit.Post.Total
		}
	}
	if top != res.Best.Total {
		t.Fatalf("单算 %d 件最赚 %v,扫出来 %v", pq.Qty, top, res.Best.Total)
	}
	// 再多就拿不满料了:最大件数那一格不能当真
	if last := res.Points[len(res.Points)-1]; last.Qty != refine.MaxQty || last.Filled {
		t.Fatalf("1 万件该拿不满料: %+v", last)
	}
}
