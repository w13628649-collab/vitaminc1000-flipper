package scan

import (
	"slices"
	"sort"

	"albion-guild/internal/catalog"
)

// GameQualities 是游戏里的五档品质(1 普通 / 2 良好 / 3 优秀 / 4 杰出 / 5 不凡)。
// 列抓包挂单扩品质时只认这几档:0 或越界的是解析出错的单,拿去问 AODP 只会白占请求。
var GameQualities = []int{1, 2, 3, 4, 5}

func validQuality(q int) bool { return q >= 1 && q <= 5 }

// QualitySet 是每个物品评估哪几档品质:物品 → 升序、不重复的品质。
//
// 以前扫描只看 cfg.Qualities(默认只有普通),成员抓到的装备良好~不凡品质挂单
// 进不了机会板。现在扫描的 (物品, 品质) 集 = 配置清单 × cfg.Qualities
// ∪ 抓包窗口里 cfg.Cities 实际有挂单的 (物品, 品质)。
//
// 品质切片一律当只读:add 换一个新切片再写回,从不原地改。快照发布之后还有别的
// goroutine 在读它(重算、查价格子),共享底层数组的话补拉会改到正在用的那份。
type QualitySet map[string][]int

func (qs QualitySet) has(item string, q int) bool {
	_, ok := slices.BinarySearch(qs[item], q)
	return ok
}

// add 把 q 并进 item 的品质表,已有时返回 false。
func (qs QualitySet) add(item string, q int) bool {
	cur := qs[item]
	i, ok := slices.BinarySearch(cur, q)
	if ok {
		return false
	}
	qs[item] = slices.Insert(slices.Clone(cur), i, q)
	return true
}

// Count 是 (物品, 品质) 组合数。
func (qs QualitySet) Count() int {
	n := 0
	for _, v := range qs {
		n += len(v)
	}
	return n
}

// clone 是浅拷贝:切片只读(见类型说明),共享没关系,map 本身要新的。
func (qs QualitySet) clone() QualitySet {
	out := make(QualitySet, len(qs))
	for k, v := range qs {
		out[k] = v
	}
	return out
}

// normQualities 把配置里的品质排序去重。配置写成 [2, 1, 1] 和 [1, 2] 是同一个意思,
// 分组拉 AODP 时(fetchAODP)要能认出"这个物品的品质表就是配置那一套"。
func normQualities(v []int) []int {
	out := slices.Clone(v)
	slices.Sort(out)
	return slices.Compact(out)
}

// uniform 是 items × qualities:配置清单那一部分。
func uniform(items []string, qualities []int) QualitySet {
	base := normQualities(qualities)
	out := make(QualitySet, len(items))
	for _, id := range items {
		out[id] = base
	}
	return out
}

// Pairs 是一批 (物品, 品质):Items 定顺序(拼 AODP 请求、读簿 key 都照它,结果才确定),
// Qualities 给每个物品的品质。
type Pairs struct {
	Items     []string
	Qualities QualitySet
}

// Count 是组合数。
func (p Pairs) Count() int { return p.Qualities.Count() }

// mergeCaptured 挑出抓到了、have 里还没有的 (物品, 品质),have 本身不动。
//
// **名额按物品算,不按 (物品, 品质)**:limit(capture.max_extra_items)限的是配置清单外
// 并进来多少个物品,每个并进来的物品带上它抓到的全部品质;have 里已有的物品(配置清单里的、
// 之前并进来的)抓到新品质不占名额。按物品算是因为成本在 id 上:多一个物品就多一个 id
// 塞进 AODP 的 URL(api.max_url_length,见 aodp.chunkByURLLength),多一档品质只是
// qualities 参数里多一个数、同一个 id。一个物品最多五档,组合数也就封顶在
// 5 × (清单物品数 + limit)。
//
// 物品按窗口内所有品质的件数合计从多到少排,同件数按 id;超出名额的整个物品截掉
// (计进 dropped)。目录里查不到的物品跳过(计进 unknown):多半是游戏更新后的新物品还没
// 同步目录,或者客户端解析出了怪 id,拿去问 AODP 只会白占 URL 预算。品质不在 1..5 的行跳过。
//
// 返回的 add.Items 是有新品质的物品,按上面的顺序;extra 是其中 have 里原本没有的物品。
func mergeCaptured(captured []CapturedItem, have QualitySet, cat *catalog.Catalog,
	limit int) (add Pairs, extra []string, dropped, unknown int) {

	type agg struct {
		id    string
		qty   int64
		quals []int
	}
	byItem := map[string]*agg{}
	var order []*agg
	for _, c := range captured {
		if c.ItemID == "" || !validQuality(c.Quality) {
			continue
		}
		a := byItem[c.ItemID]
		if a == nil {
			a = &agg{id: c.ItemID}
			byItem[c.ItemID] = a
			order = append(order, a)
		}
		a.qty += c.Qty
		if !slices.Contains(a.quals, c.Quality) {
			a.quals = append(a.quals, c.Quality)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].qty != order[j].qty {
			return order[i].qty > order[j].qty
		}
		return order[i].id < order[j].id
	})

	add = Pairs{Qualities: QualitySet{}}
	for _, a := range order {
		if _, known := have[a.id]; !known {
			if cat != nil {
				if _, ok := cat.Get(a.id); !ok {
					unknown++
					continue
				}
			}
			if len(extra) >= limit {
				dropped++
				continue
			}
			extra = append(extra, a.id)
		}
		touched := false
		for _, q := range a.quals {
			if !have.has(a.id, q) && add.Qualities.add(a.id, q) {
				touched = true
			}
		}
		if touched {
			add.Items = append(add.Items, a.id)
		}
	}
	return add, extra, dropped, unknown
}
