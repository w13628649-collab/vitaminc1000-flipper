package refine

import (
	"fmt"
	"sort"

	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
)

// 给服务层(flip/refine.go)用的几样东西:参数默认值从配置来、要读哪些盘口、参数先校验一遍。
// 引擎本身仍然只认填好的 Params,不看配置。

// Validate 在读库之前先把参数校验一遍:不合法的参数不该白读一次库、白打一次 AODP。
// 和 Plan 里的校验是同一份(normalized + validate)。
func Validate(p Params) error {
	_, _, _, err := p.normalized().validate()
	return err
}

// Normalized 是 Plan 实际会用的参数:补齐出售城市、买料城市这几个空字段。
// 服务层按它收集要读的盘口,才和引擎看的是同一批城市。
func Normalized(p Params) Params { return p.normalized() }

// DefaultsFrom 是服务端给一次请求打底的参数:待核对的默认值和精炼段的配置
// (conf.Refining,设计 §4.6),税费取 conf.Economics。city 为空时用这条线的专精城。
//
// 客户端带上的字段会盖掉这里(API 层把请求体解码到它上面);没带的字段保持这里的值,
// 零值有意义的字段(站费 0、心不返还)不会被换成默认值。
// 和 DefaultParams 的区别只在数从配置来:conf.Default() 下两者相同(service_test 断言)。
func DefaultsFrom(target, city string, cfg conf.Config) Params {
	p := DefaultParams(target)
	if city != "" {
		p.City = city
	}
	r := cfg.Refining
	line, _ := lineOfTarget(target)
	econ := cfg.Economics
	p.Sell.City = p.City
	p.Sources = SourceParams{Cities: []string{p.City}, TransportPct: r.TransportPct,
		BidOrders: r.BidOrders, MaxAgeHours: r.WindowHours}
	p.Focus.Budget = r.FocusPerDay
	p.RRR.DailyBonus = r.DailyBonus[line.Key]
	p.Fees = FeeParams{
		StationPer100: r.StationFeeFor(p.City), TransmuteFactor: r.TransmuteFactor,
		TransmuteStationPer100: r.TransmuteStationFeePer100,
		Tax:                    econ.MarketTax, Setup: econ.SetupFee, BuyOrderSetup: econ.BuyOrderSetupFee,
		Outbid: econ.OutbidSilver, Undercut: econ.UndercutSilver,
	}
	p.Daily.VolumeShare = r.VolumeShare
	p.HeartReturns = r.HeartReturns
	p.Leftover = r.Leftover
	p.IncludeTail = r.IncludeTail
	return p
}

// lineOfTarget:目标(或任意一档原料 / 半成品)属于哪条线。
func lineOfTarget(id string) (Line, bool) {
	family, _, _, ok := ParseItemID(id)
	if !ok {
		return Line{}, false
	}
	return LineOf(family)
}

// LineOfItem 是 lineOfTarget 的导出版:心也认(按它对应的线)。
func LineOfItem(id string) (Line, bool) {
	if l, ok := heartOf(id); ok {
		return l, true
	}
	return lineOfTarget(id)
}

// LineByKey 按线名(ORE / HIDE / FIBER / WOOD)找线。
func LineByKey(key string) (Line, bool) {
	for _, l := range Lines {
		if l.Key == key {
			return l, true
		}
	}
	return Line{}, false
}

// Targets 是一条线能算的全部目标:T4~T8 × .0~.4 的半成品,共 25 个,从低到高。
// 矩阵视图(GET /api/refine/matrix)按这个顺序出格子。
func (l Line) Targets() []string {
	out := make([]string, 0, 25)
	for t := MinEnchantTier; t <= MaxTier; t++ {
		for _, e := range Enchants(t) {
			out = append(out, l.RefinedID(t, e))
		}
	}
	return out
}

// Items 是一条线在 T2~T8 的全部原料、半成品,加这条线的心。服务层按线向 AODP 要价:
// 同一条线的几个目标共用一次请求、一份缓存。
func (l Line) Items() []string {
	var out []string
	for t := MinTier; t <= MaxTier; t++ {
		for _, e := range Enchants(t) {
			out = append(out, l.RawID(t, e), l.RefinedID(t, e))
		}
	}
	out = append(out, l.Heart)
	sort.Strings(out)
	return out
}

// ItemsFor 是算 target 时引擎可能去看的全部物品(设计 §4.3 第 1 步「由配方图收集 key」),
// 排好序:
//   - 链上每一级半成品,直到第一个只能买的那一级(T3 及以下只当链底的输入,§2.1)
//   - 每一级配方里的原料和心(普通配方和心配方都算)
//   - 原料的全部转化祖先:能一路转化上来的每一档原料(B3)
//
// 不管解锁到哪一级、开不开专注都列全:多读几个盘口不要紧,少读一个就会被当成缺价。
func ItemsFor(recipes *catalog.RecipeBook, target string) ([]string, error) {
	line, ok := lineOfTarget(target)
	if !ok {
		return nil, fmt.Errorf("%q 不属于矿/皮/纤维/木四条线", target)
	}
	g := newGraph(recipes)
	seen := map[string]bool{}
	var out []string
	add := func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		out = append(out, id)
		return true
	}
	var addRaw func(id string)
	addRaw = func(id string) {
		if !add(id) {
			return
		}
		for _, e := range g.transIn[id] {
			addRaw(e.from)
		}
	}
	item := target
	for depth := 0; depth < 16 && item != ""; depth++ {
		add(item)
		_, tier, _, _ := ParseItemID(item)
		if tier < MinEnchantTier {
			break // T3 及以下只买(§2.1),不往下展开
		}
		next := ""
		for _, r := range g.refineRecipes(item) {
			for _, in := range r.Inputs {
				switch family, _, _, _ := ParseItemID(in.ItemID); {
				case family == line.Refined:
					next = in.ItemID
				case family == line.Raw:
					addRaw(in.ItemID)
				default:
					add(in.ItemID) // 心
				}
			}
		}
		item = next
	}
	sort.Strings(out)
	return out, nil
}
