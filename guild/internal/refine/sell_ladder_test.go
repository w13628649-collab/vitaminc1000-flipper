package refine

import "testing"

// 挂单价试算要数「挂这个价前面排着几件」:挂卖信息带上卖单阶梯,从卖一起、从低到高、最多 sellLadderMax 档
func TestPostSell带卖单阶梯(t *testing.T) {
	l := Lines[0]
	target := l.RefinedID(6, 4)
	books := synthBooks(l, l.City, 7)
	ladder := make([]int64, 0, 2*(sellLadderMax+10))
	for i := range sellLadderMax + 10 {
		ladder = append(ladder, int64(100000+i*500), int64(i+1))
	}
	setAsks(books, target, l.City, ladder...)
	res, err := Plan(Input{Params: params(target), Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	ps := res.Sell.Post
	if ps == nil || len(ps.Asks) != sellLadderMax || ps.Asks[0].Price != ps.BestAsk || ps.Asks[0].Qty != 1 {
		t.Fatalf("阶梯要从卖一起、截到 %d 档:%+v", sellLadderMax, ps)
	}
	for i := 1; i < len(ps.Asks); i++ {
		if ps.Asks[i].Price <= ps.Asks[i-1].Price {
			t.Fatalf("阶梯要从低到高:%+v", ps.Asks)
		}
	}
}
