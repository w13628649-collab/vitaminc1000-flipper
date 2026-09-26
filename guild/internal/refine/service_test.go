package refine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"albion-guild/internal/conf"
)

// conf 不能 import refine(这边 import 了 conf),默认值只好两边各写一份。这里断言是同一组数:
// 界面上的「未核实」徽章按 is_default 判,两边对不上就会把默认值标成用户改过的
func Test配置默认值和引擎默认值一致(t *testing.T) {
	r := conf.Default().Refining
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"window_hours", r.WindowHours, DefaultMaxAgeHours},
		{"station_fee_per100", r.StationFeePer100, DefaultStationFeePer100},
		{"transmute_factor", r.TransmuteFactor, DefaultTransmuteFactor},
		{"transmute_station_fee_per100", r.TransmuteStationFeePer100, DefaultTransmuteStationFeePer100},
		{"heart_returns", r.HeartReturns, DefaultHeartReturns},
		{"leftover", r.Leftover, LeftoverCarry},
		{"focus_per_day", r.FocusPerDay, DefaultFocusBudget},
		{"focus_cap", r.FocusCap, FocusCap},
		{"volume_share", r.VolumeShare, DefaultVolumeShare},
	} {
		if c.got != c.want {
			t.Errorf("%s:配置 %v,引擎 %v", c.name, c.got, c.want)
		}
	}
	var keys []string
	for _, l := range Lines {
		keys = append(keys, l.Key)
	}
	if !slices.Equal(keys, conf.RefiningLines) {
		t.Fatalf("conf.RefiningLines %v 和 refine.Lines %v 对不上", conf.RefiningLines, keys)
	}
	// 配置里的城市列表要包含四个专精城,不然默认的生产城市过不了服务端的城市校验
	for _, l := range Lines {
		if !slices.Contains(conf.Default().Cities, l.City) {
			t.Errorf("默认城市列表里没有专精城 %s", l.City)
		}
	}
}

// 默认配置下服务端打底的参数就是 DefaultParams:引擎测试用的和线上跑的是同一组默认值
func TestDefaultsFrom在默认配置下等于DefaultParams(t *testing.T) {
	for _, l := range Lines {
		for _, target := range l.Targets() {
			got, want := DefaultsFrom(target, "", conf.Default()), DefaultParams(target)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s:\n配置打底 %+v\nDefaultParams %+v", target, got, want)
			}
		}
	}
}

func TestDefaultsFrom按城市取站费按线取每日加成(t *testing.T) {
	cfg := conf.Default()
	cfg.Refining.StationFeeByCity = map[string]float64{"Martlock": 800}
	cfg.Refining.DailyBonus = map[string]float64{"ORE": 10}
	cfg.Refining.TransmuteFactor = 1.1
	cfg.Refining.WindowHours = 96
	cfg.Economics.MarketTax = 0.08

	p := DefaultsFrom("T8_METALBAR_LEVEL4@4", "Martlock", cfg)
	if p.City != "Martlock" || p.Sell.City != "Martlock" || !slices.Equal(p.Sources.Cities, []string{"Martlock"}) {
		t.Fatalf("给了城市,生产、出售、买料都应在那里: %+v %+v %+v", p.City, p.Sell, p.Sources)
	}
	if p.Fees.StationPer100 != 800 || p.RRR.DailyBonus != 10 || p.Fees.TransmuteFactor != 1.1 ||
		p.Sources.MaxAgeHours != 96 || p.Fees.Tax != 0.08 {
		t.Fatalf("配置没用上: %+v %+v %+v", p.Fees, p.RRR, p.Sources)
	}
	// 没覆盖的城市用全局站费;每日加成只加给那条线
	q := DefaultsFrom("T8_LEATHER_LEVEL4@4", "", cfg)
	if q.City != "Martlock" || q.Fees.StationPer100 != 800 || q.RRR.DailyBonus != 0 {
		t.Fatalf("皮革线: %+v %+v", q.Fees, q.RRR)
	}
	r := DefaultsFrom("T8_LEATHER_LEVEL4@4", "Thetford", cfg)
	if r.Fees.StationPer100 != 500 {
		t.Fatalf("Thetford 没覆盖,应为全局 500,得到 %g", r.Fees.StationPer100)
	}
}

func TestLine的目标和物品(t *testing.T) {
	l := Lines[0]
	ts := l.Targets()
	if len(ts) != 25 || ts[0] != "T4_METALBAR" || ts[24] != "T8_METALBAR_LEVEL4@4" {
		t.Fatalf("目标 %v", ts)
	}
	items := l.Items()
	// T2、T3 各 1 档,T4~T8 各 5 档,原料 + 半成品,再加 1 种心
	if len(items) != (2+5*5)*2+1 || !slices.Contains(items, l.Heart) || !slices.IsSorted(items) {
		t.Fatalf("物品 %d 个: %v", len(items), items)
	}
	for _, key := range []string{"ORE", "HIDE", "FIBER", "WOOD"} {
		if got, ok := LineByKey(key); !ok || got.Key != key {
			t.Fatalf("LineByKey(%s)", key)
		}
	}
	if _, ok := LineByKey("ROCK"); ok {
		t.Fatal("石头线本期不做")
	}
	if l, ok := LineOfItem("T1_FACTION_STEPPE_TOKEN_1"); !ok || l.Key != "HIDE" {
		t.Fatal("心要认得出是哪条线的")
	}
}

func TestItemsFor按配方图收集(t *testing.T) {
	rb := EmbeddedRecipes()
	got, err := ItemsFor(rb, "T8_METALBAR_LEVEL4@4")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"T8_METALBAR_LEVEL4@4", "T7_METALBAR_LEVEL4@4", "T4_METALBAR_LEVEL4@4", "T3_METALBAR", // 链到 T3 为止
		"T8_ORE_LEVEL4@4", "T8_ORE_LEVEL3@3", "T8_ORE", "T7_ORE", "T7_ORE_LEVEL4@4", "T4_ORE_LEVEL4@4", // 原料和转化祖先
	} {
		if !slices.Contains(got, want) {
			t.Errorf("T8.4 条要看 %s,没收进来", want)
		}
	}
	for _, not := range []string{"T2_METALBAR", "T8_METALBAR_LEVEL3@3", "T1_FACTION_MOUNTAIN_TOKEN_1", "T8_HIDE_LEVEL4@4"} {
		if slices.Contains(got, not) {
			t.Errorf("T8.4 条不该看 %s(链到 T3 只买;.4 没有心配方;别的线)", not)
		}
	}
	lowEnchant, _ := ItemsFor(rb, "T5_METALBAR_LEVEL1@1")
	if !slices.Contains(lowEnchant, "T1_FACTION_MOUNTAIN_TOKEN_1") {
		t.Fatal(".1 有心配方,心要收进来")
	}
	for _, l := range Lines {
		all := l.Items()
		for _, target := range l.Targets() {
			its, err := ItemsFor(rb, target)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range its {
				if !slices.Contains(all, it) {
					t.Fatalf("%s 收进了线外的 %s", target, it)
				}
			}
		}
	}
	if _, err := ItemsFor(rb, "T8_MAIN_FIRESTAFF"); err == nil {
		t.Fatal("线外的物品应报错")
	}
}

// ItemsFor 是完整的:只给它列出的物品的盘口,和给整条线的盘口,算出来一模一样。
// 少收一个,那个物品在服务层就会被当成缺价
func TestItemsFor收齐了引擎要看的盘口(t *testing.T) {
	for _, l := range Lines {
		full := synthBooks(l, l.City, 7)
		for _, target := range []string{l.RefinedID(8, 4), l.RefinedID(6, 1), l.RefinedID(4, 0), l.RefinedID(5, 3)} {
			its, err := ItemsFor(EmbeddedRecipes(), target)
			if err != nil {
				t.Fatal(err)
			}
			only := Books{}
			for k, b := range full {
				if slices.Contains(its, k.ItemID) {
					only[k] = b
				}
			}
			// 心的盘口 synthBooks 不造,补一个。心压到 1 银,让心配方一定胜出:少收了心就一定看得出来
			hb := Book{Asks: askSide(1, 500), Bids: askSide(1, 50)}
			full[Key{l.Heart, l.City}] = hb
			if slices.Contains(its, l.Heart) {
				only[Key{l.Heart, l.City}] = hb
			}
			p := params(target)
			p.Sources.BidOrders = true
			a, err := Plan(Input{Params: p, Books: full, Recipes: EmbeddedRecipes()})
			if err != nil {
				t.Fatal(err)
			}
			b, err := Plan(Input{Params: p, Books: only, Recipes: EmbeddedRecipes()})
			if err != nil {
				t.Fatal(err)
			}
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if !bytes.Equal(ja, jb) {
				t.Fatalf("%s:只给 ItemsFor 的盘口算出来不一样", target)
			}
		}
	}
}

func TestValidate和Plan同一份校验(t *testing.T) {
	p := params("T8_METALBAR_LEVEL4@4")
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []func(*Params){
		func(p *Params) { p.Target = "T8_ORE_LEVEL4@4" },
		func(p *Params) { p.Target = "T8_STONEBLOCK" },
		func(p *Params) { p.Qty = 0 },
		func(p *Params) { p.Fees.TransmuteFactor = 115.4 },
		func(p *Params) { p.Sort = "profit" },
	} {
		q := params("T8_METALBAR_LEVEL4@4")
		bad(&q)
		err := Validate(q)
		_, perr := Plan(Input{Params: q, Recipes: EmbeddedRecipes()})
		if err == nil || perr == nil || err.Error() != perr.Error() {
			t.Fatalf("Validate %v,Plan %v", err, perr)
		}
	}
	// Normalized 补齐出售、买料城市
	q := params("T8_METALBAR_LEVEL4@4")
	q.Sell.City, q.Sources.Cities = "", nil
	n := Normalized(q)
	if n.Sell.City != "Thetford" || !slices.Equal(n.Sources.Cities, []string{"Thetford"}) {
		t.Fatalf("Normalized: %+v %+v", n.Sell, n.Sources)
	}
}

func TestRecipeTree(t *testing.T) {
	rb := EmbeddedRecipes()
	tr, err := RecipeTree(rb, "T8_METALBAR_LEVEL4@4", DefaultTransmuteFactor, DefaultTransmuteStationFeePer100)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Kind != "refined" || tr.Line != "ORE" || tr.Specialty != "Thetford" || len(tr.Chain) != 6 {
		t.Fatalf("链: %+v", tr)
	}
	top, bottom := tr.Chain[0], tr.Chain[5]
	if top.ItemValue != 4096 || len(top.Recipes) != 1 || top.Recipes[0].Focus != 4714 || top.Lower != "T7_METALBAR_LEVEL4@4" {
		t.Fatalf("顶层: %+v", top)
	}
	in := top.Recipes[0].Inputs
	if len(in) != 2 || in[0].Kind != "raw" || in[0].Count != 5 || !in[0].Returns || in[1].Kind != "refined" {
		t.Fatalf("T8.4 条 = 5 × T8.4 矿 + 1 × T7.4 条(A1): %+v", in)
	}
	if bottom.Item != "T3_METALBAR" || bottom.Makeable {
		t.Fatalf("链底应是只能买的 T3 条: %+v", bottom)
	}
	// 自炼的五级各有一种原料要来路
	if len(tr.Raws) != 5 || tr.Raws[0].Item != "T8_ORE_LEVEL4@4" {
		t.Fatalf("原料: %+v", tr.Raws)
	}
	var r83 *TreeRoute
	for i, r := range tr.Raws[0].Routes {
		if r.Start == "T8_ORE_LEVEL3@3" {
			r83 = &tr.Raws[0].Routes[i]
		}
	}
	// 8.3 → 8.4 基础价 748,440(B4′,不是 wiki 的 784,440),k=1.154、费率 500 实付 863,930
	if r83 == nil || len(r83.Path) != 2 {
		t.Fatalf("8.3 起点: %+v", r83)
	}
	assertNear(t, "8.3→8.4 实付", r83.Unit, 863_930, 1)
	var e83 *TreeEdge
	for i, e := range tr.Edges {
		if e.From == "T8_ORE_LEVEL3@3" && e.To == "T8_ORE_LEVEL4@4" {
			e83 = &tr.Edges[i]
		}
	}
	if e83 == nil || e83.Silver != 748_440 || e83.ToIV != 409.6 {
		t.Fatalf("8.3→8.4 的边: %+v", e83)
	}
	// 转化格子里 8.4 矿有两条进边:8.3 升附魔、7.4 升 tier
	in84 := 0
	for _, e := range tr.Edges {
		if e.To == "T8_ORE_LEVEL4@4" {
			in84++
		}
	}
	if in84 != 2 {
		t.Fatalf("8.4 矿应有 2 条进边,得到 %d", in84)
	}
	// 路线从便宜到贵;目标自己是 0
	if tr.Raws[0].Routes[0].Start != "T8_ORE_LEVEL4@4" || tr.Raws[0].Routes[0].Unit != 0 {
		t.Fatalf("第一条应是目标自己: %+v", tr.Raws[0].Routes[0])
	}

	raw, err := RecipeTree(rb, "T8_ORE_LEVEL4@4", 1.0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Kind != "raw" || len(raw.Chain) != 0 || len(raw.Raws) != 1 {
		t.Fatalf("原料的树: %+v", raw)
	}
	for _, r := range raw.Raws[0].Routes {
		if r.Start == "T8_ORE_LEVEL3@3" {
			assertNear(t, "k=1、费率 0 时就是基础价", r.Unit, 748_440, 1e-6)
		}
	}

	heart, err := RecipeTree(rb, "T5_LEATHER_LEVEL2@2", 1.154, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(heart.Chain[0].Recipes) != 2 || heart.Chain[0].Recipes[0].Heart || !heart.Chain[0].Recipes[1].Heart {
		t.Fatalf(".2 应有普通 + 心两条配方,普通在前: %+v", heart.Chain[0].Recipes)
	}

	for _, bad := range []string{"T8_MAIN_FIRESTAFF", "T8_STONEBLOCK", "T3_ORE_LEVEL1@1", "T9_ORE", ""} {
		if _, err := RecipeTree(rb, bad, 1.154, 500); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
	if _, err := RecipeTree(rb, "T8_ORE", 115.4, 500); err == nil || !strings.Contains(err.Error(), "k") {
		t.Fatalf("k 录错应报错: %v", err)
	}
}
