package refine

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"albion-guild/internal/catalog"
)

var update = flag.Bool("update", false,
	"用 FLIPPER_ITEMS_JSON 指向的原始 items.json 重新生成 data/recipes_20260923.json")

// ── 快照和上游 dump 一致 ────────────────────────────────────────────────

// 内置快照必须就是 catalog 的解析器对原始 items.json 解析、再裁到四条线的结果。
// 需要本地有一份原始 items.json(17MB,不进仓库):没设 FLIPPER_ITEMS_JSON 就跳过,
// 下面那些结构断言照样对快照本身跑。
func Test内置快照和items_json一致(t *testing.T) {
	path := os.Getenv("FLIPPER_ITEMS_JSON")
	if path == "" {
		t.Skip("没设 FLIPPER_ITEMS_JSON(原始 items.json 的本地路径),跳过全文件比对")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	all, err := catalog.ParseRecipes(f)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := TrimRecipes(all)
	if *update {
		writeSnapshot(t, trimmed)
		return
	}
	got := EmbeddedRecipes().All()
	if !reflect.DeepEqual(trimmed, got) {
		for i := range min(len(trimmed), len(got)) {
			if !reflect.DeepEqual(trimmed[i], got[i]) {
				t.Fatalf("第 %d 条不一致\ndump  %+v\n快照 %+v", i, trimmed[i], got[i])
			}
		}
		t.Fatalf("条数不一致:dump %d,快照 %d", len(trimmed), len(got))
	}

	// 范围外的精炼/转化配方只应该是石头线(A8:本期不做)
	for _, r := range all {
		if InScope(r) {
			continue
		}
		family, _, _, _ := ParseItemID(r.ItemID)
		if family != "ROCK" && family != "STONEBLOCK" {
			t.Fatalf("范围外出现了不认识的配方 %s(%s),要么是新的线,要么解析错了", r.ItemID, r.Kind)
		}
	}
}

// writeSnapshot 一条配方一行,改动时 diff 好读。换 dump 版本时连文件名、
// SnapshotName、SnapshotDataDate 一起改。
func writeSnapshot(t *testing.T, recipes []catalog.Recipe) {
	t.Helper()
	var buf bytes.Buffer
	source, _ := json.Marshal("ao-bin-dumps items.json,数据版本 90b141e(2026-09-23)")
	scope, _ := json.Marshal("矿/皮/纤维/木四条线 T2~T8:半成品精炼(含心配方)和原料转化")
	buf.WriteString("{\n \"source\": ")
	buf.Write(source)
	buf.WriteString(",\n \"scope\": ")
	buf.Write(scope)
	buf.WriteString(",\n \"recipes\": [\n")
	for i, r := range recipes {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		buf.WriteString("  ")
		buf.Write(line)
		if i < len(recipes)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(" ]\n}\n")
	path := filepath.Join("data", "recipes_20260923.json")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写 %s:%d 条配方", path, len(recipes))
}

// ── 结构断言:对快照本身跑,不依赖本地文件 ─────────────────────────────

func book(t *testing.T) *catalog.RecipeBook {
	t.Helper()
	b := EmbeddedRecipes()
	if b.Len() == 0 {
		t.Fatal("内置配方快照是空的")
	}
	return b
}

// 每条线:半成品 T2、T3 各 1 条,T4~T8 × .0~.3 各 2 条(普通 + 心),.4 各 1 条,
// 共 2 + 20×2 + 5 = 47;转化 T5~T8 .0 各 1、T4 .1~.4 各 1、T5~T8 .1~.4 各 2,共 40。
func Test快照条数和范围(t *testing.T) {
	b := book(t)
	if b.Len() != 4*(47+40) {
		t.Fatalf("快照 %d 条,想要 %d", b.Len(), 4*(47+40))
	}
	count := map[string]map[catalog.RecipeKind]int{}
	for _, r := range b.All() {
		if !InScope(r) {
			t.Fatalf("快照里混进了范围外的 %s", r.ItemID)
		}
		family, _, _, _ := ParseItemID(r.ItemID)
		l, _ := LineOf(family)
		if count[l.Key] == nil {
			count[l.Key] = map[catalog.RecipeKind]int{}
		}
		count[l.Key][r.Kind]++
	}
	for _, l := range Lines {
		if count[l.Key][catalog.RecipeRefine] != 47 || count[l.Key][catalog.RecipeTransmute] != 40 {
			t.Fatalf("%s 线:精炼 %d 条、转化 %d 条,想要 47 / 40",
				l.Key, count[l.Key][catalog.RecipeRefine], count[l.Key][catalog.RecipeTransmute])
		}
	}
}

// A3:四条线的配方数量、专注、物品价值、银币完全相同,只有用的心不同。
// 把每条线的族名和心换成占位符,四条线得到的配方集合必须一模一样。
func Test四条线同构(t *testing.T) {
	b := book(t)
	canon := func(l Line) []string {
		rep := strings.NewReplacer("_"+l.Raw, "_RAW", "_"+l.Refined, "_REF", l.Heart, "HEART")
		var out []string
		for _, r := range b.All() {
			family, _, _, _ := ParseItemID(r.ItemID)
			if rl, _ := LineOf(family); rl.Key != l.Key {
				continue
			}
			line, _ := json.Marshal(r)
			out = append(out, rep.Replace(string(line)))
		}
		sort.Strings(out)
		return out
	}
	want := canon(Lines[0])
	for _, s := range want {
		if strings.Contains(s, "FACTION") {
			t.Fatalf("占位替换漏了心: %s", s)
		}
	}
	for _, l := range Lines[1:] {
		if got := canon(l); !reflect.DeepEqual(got, want) {
			for i := range min(len(got), len(want)) {
				if got[i] != want[i] {
					t.Fatalf("%s 线和 %s 线不同构\n%s\n%s", l.Key, Lines[0].Key, got[i], want[i])
				}
			}
			t.Fatalf("%s 线 %d 条,%s 线 %d 条", l.Key, len(got), Lines[0].Key, len(want))
		}
	}
}

// counts 把材料表变成 id → 件数,不在意 dump 里的先后顺序。
func counts(in []catalog.RecipeInput) map[string]int {
	out := map[string]int{}
	for _, x := range in {
		out[x.ItemID] += x.Count
	}
	return out
}

// A1、A2、A4、A6:半成品配方的形状。
//   - T(t).e = n_t × 同 tier 同附魔原料 + 1 × 下级半成品,n:T3=2、T4=2、T5=3、T6=4、T7=5、T8=5
//   - T4.x 的下级料是 T3(.0),T3 没有附魔版本;T2 只有 1 个 T2 原料
//   - T4~T8 .0~.3 另有一条心配方:(n−1)×原料 + 1×心 + 1×下级料;**.4 没有心配方**
//   - 不要银币,每次 1 件;材料都没写 maxreturnamount,参与返还
func Test半成品配方形状(t *testing.T) {
	b := book(t)
	n := map[int]int{3: 2, 4: 2, 5: 3, 6: 4, 7: 5, 8: 5}
	for _, l := range Lines {
		for tier := MinTier; tier <= MaxTier; tier++ {
			for _, e := range Enchants(tier) {
				id := l.RefinedID(tier, e)
				recipes := b.Of(id)
				if len(b.OfKind(id, catalog.RecipeTransmute)) != 0 {
					t.Fatalf("%s:半成品不能转化(B2),快照里却有转化配方", id)
				}
				want := []map[string]int{}
				switch {
				case tier == 2:
					want = append(want, map[string]int{l.RawID(2, 0): 1})
				default:
					lower := l.RefinedID(tier-1, e)
					if tier == MinEnchantTier {
						lower = l.RefinedID(3, 0)
					}
					want = append(want, map[string]int{l.RawID(tier, e): n[tier], lower: 1})
					if tier >= MinEnchantTier && e < MaxEnchant {
						want = append(want, map[string]int{l.RawID(tier, e): n[tier] - 1, l.Heart: 1, lower: 1})
					}
				}
				if len(recipes) != len(want) {
					t.Fatalf("%s:%d 条配方,想要 %d 条", id, len(recipes), len(want))
				}
				for i, r := range recipes {
					if r.Kind != catalog.RecipeRefine || r.Silver != 0 || r.Amount != 1 {
						t.Fatalf("%s 第 %d 条:kind=%s silver=%d amount=%d", id, i, r.Kind, r.Silver, r.Amount)
					}
					if got := counts(r.Inputs); !reflect.DeepEqual(got, want[i]) {
						t.Fatalf("%s 第 %d 条材料 %v,想要 %v", id, i, got, want[i])
					}
					for _, in := range r.Inputs {
						if in.MaxReturn != nil {
							t.Fatalf("%s 的材料 %s 写了 maxreturnamount=%d,精炼材料本来都没写(A6)",
								id, in.ItemID, *in.MaxReturn)
						}
					}
				}
			}
		}
	}
}

// .4 没有心配方——单独拎出来,这条决定了 .4 链上永远不会出现心(A4)。
func Test点4没有心配方(t *testing.T) {
	b := book(t)
	for _, l := range Lines {
		for tier := MinEnchantTier; tier <= MaxTier; tier++ {
			for _, r := range b.Of(l.RefinedID(tier, MaxEnchant)) {
				if counts(r.Inputs)[l.Heart] != 0 {
					t.Fatalf("%s 出现了心配方", r.ItemID)
				}
			}
			if got := len(b.Of(l.RefinedID(tier, 3))); got != 2 {
				t.Fatalf("%s 应当有普通 + 心两条配方,得到 %d", l.RefinedID(tier, 3), got)
			}
		}
	}
}

// D1:每次精炼的基础专注,两条配方(普通 / 心)相同。
func Test基础专注(t *testing.T) {
	b := book(t)
	want := map[int][]int{
		2: {18},
		3: {31},
		4: {54, 94, 164, 287, 503},
		5: {94, 164, 287, 503, 880},
		6: {164, 287, 503, 880, 1539},
		7: {287, 503, 880, 1539, 2694},
		8: {503, 880, 1539, 2694, 4714},
	}
	for _, l := range Lines {
		for tier, row := range want {
			for e, focus := range row {
				for _, r := range b.Of(l.RefinedID(tier, e)) {
					if r.Focus != focus {
						t.Fatalf("%s 第 %d 条专注 %d,想要 %d", r.ItemID, r.Variant, r.Focus, focus)
					}
				}
			}
		}
	}
}

// E2:半成品的 IV 每 +1 tier 或 +1 附魔翻倍,T4 条 16,T8.4 条 4096,即 2^(tier+附魔)。
// A10:T2 半成品带 unlockedtocraft,T3 起要先在命运之轮解锁。
func Test半成品IV逐级翻倍(t *testing.T) {
	b := book(t)
	for _, l := range Lines {
		for tier := MinTier; tier <= MaxTier; tier++ {
			for _, e := range Enchants(tier) {
				id := l.RefinedID(tier, e)
				want := float64(int(1) << (tier + e))
				for _, r := range b.Of(id) {
					if r.ItemValue != want {
						t.Fatalf("%s IV %v,想要 %v", id, r.ItemValue, want)
					}
					if r.UnlockedToCraft != (tier == 2) {
						t.Fatalf("%s unlocked_to_craft=%v", id, r.UnlockedToCraft)
					}
				}
			}
		}
	}
	if r := b.Of("T8_METALBAR_LEVEL4@4"); len(r) != 1 || r[0].ItemValue != 4096 {
		t.Fatalf("T8.4 钢条 = %+v", r)
	}
}

// B1–B3:转化只对原料,1 换 1,每步同 tier +1 附魔或同附魔 +1 tier;
// 不用专注、不返还(输入显式 maxreturnamount=0);原料带 unlockedtocraft,不用解锁(A10)。
func Test转化配方形状(t *testing.T) {
	b := book(t)
	for _, l := range Lines {
		for tier := MinTier; tier <= MaxTier; tier++ {
			for _, e := range Enchants(tier) {
				id := l.RawID(tier, e)
				if len(b.OfKind(id, catalog.RecipeRefine)) != 0 {
					t.Fatalf("%s:原料不该有精炼配方", id)
				}
				var from []string
				if tier >= 5 {
					from = append(from, l.RawID(tier-1, e)) // 升 tier
				}
				if tier >= MinEnchantTier && e > 0 {
					from = append(from, l.RawID(tier, e-1)) // 升附魔
				}
				recipes := b.OfKind(id, catalog.RecipeTransmute)
				var got []string
				for _, r := range recipes {
					if len(r.Inputs) != 1 || r.Inputs[0].Count != 1 {
						t.Fatalf("%s:转化应当 1 换 1,得到 %+v", id, r.Inputs)
					}
					if r.Inputs[0].MaxReturn == nil || *r.Inputs[0].MaxReturn != 0 || r.Inputs[0].Returns() {
						t.Fatalf("%s:转化输入应当显式不返还", id)
					}
					if r.Focus != 0 || r.Amount != 1 || !r.UnlockedToCraft || r.Silver <= 0 {
						t.Fatalf("%s:focus=%d amount=%d unlocked=%v silver=%d",
							id, r.Focus, r.Amount, r.UnlockedToCraft, r.Silver)
					}
					got = append(got, r.Inputs[0].ItemID)
				}
				sort.Strings(got)
				sort.Strings(from)
				if !reflect.DeepEqual(got, from) && !(len(got) == 0 && len(from) == 0) {
					t.Fatalf("%s 能从 %v 转上来,想要 %v", id, got, from)
				}
			}
		}
	}
}

// transmuteBase 取「从 from 转到 to」那一条的基础银币。
func transmuteBase(t *testing.T, b *catalog.RecipeBook, to, from string) catalog.Recipe {
	t.Helper()
	for _, r := range b.OfKind(to, catalog.RecipeTransmute) {
		if r.Inputs[0].ItemID == from {
			return r
		}
	}
	t.Fatalf("没有 %s → %s 的转化配方", from, to)
	return catalog.Recipe{}
}

// B4:转化基础银币和规则表一致。8.3→8.4 是 748,440——wiki 写的 784,440 是笔误(B4′,
// dump 第 8734 行;论坛 187902 游戏内实测 8.0→8.4 为 958,281,差额正好是使用费)。
// 四条线同价(B3),每条线都查。
func Test转化基础价与规则表B4一致(t *testing.T) {
	b := book(t)
	for _, l := range Lines {
		sum := int64(0)
		for e, want := range map[int]int64{1: 14_400, 2: 45_360, 3: 149_688, 4: 748_440} {
			got := transmuteBase(t, b, l.RawID(8, e), l.RawID(8, e-1)).Silver
			if got != want {
				t.Fatalf("%s 8.%d→8.%d 基础价 %d,想要 %d", l.Key, e-1, e, got, want)
			}
			sum += got
		}
		if sum != 957_888 {
			t.Fatalf("%s 8.0→8.4 基础价合计 %d,想要 957,888", l.Key, sum)
		}
		if got := transmuteBase(t, b, l.RawID(8, 4), l.RawID(8, 3)).Silver; got == 784_440 {
			t.Fatal("8.3→8.4 读成了 wiki 的笔误 784,440")
		}
		for e, want := range []int64{5_000, 15_000, 47_250, 155_925, 779_625} {
			if got := transmuteBase(t, b, l.RawID(8, e), l.RawID(7, e)).Silver; got != want {
				t.Fatalf("%s 7.%d→8.%d 基础价 %d,想要 %d", l.Key, e, e, got, want)
			}
		}
		for e, want := range map[int]int64{1: 1_500, 2: 3_000, 3: 6_000, 4: 24_000} {
			if got := transmuteBase(t, b, l.RawID(4, e), l.RawID(4, e-1)).Silver; got != want {
				t.Fatalf("%s 4.%d→4.%d 基础价 %d,想要 %d", l.Key, e-1, e, got, want)
			}
		}
	}

	// 和第 1 步的规则接起来:数据里的 748,440 和 409.6,按默认 k、默认主人费率,
	// 实付 863,930(表格 J27 手抄 863,926)
	r := transmuteBase(t, b, "T8_ORE_LEVEL4@4", "T8_ORE_LEVEL3@3")
	got := TransmuteCost(r.Silver, DefaultTransmuteFactor, r.ItemValue, DefaultTransmuteStationFeePer100)
	if !near(got, 863_930, 0.5) {
		t.Fatalf("8.3→8.4 实付 %.2f,想要 863,930", got)
	}
}

// 精炼链能一路接到底:每个下级料自己也是快照里的一个半成品,心和原料不需要配方。
func Test下级料都在快照里(t *testing.T) {
	b := book(t)
	for _, r := range b.All() {
		if r.Kind != catalog.RecipeRefine {
			continue
		}
		for _, in := range r.Inputs {
			family, _, _, ok := ParseItemID(in.ItemID)
			if !ok {
				if _, isHeart := heartLine(in.ItemID); !isHeart {
					t.Fatalf("%s 的材料 %s 既不是资源也不是心", r.ItemID, in.ItemID)
				}
				continue
			}
			l, _ := LineOf(family)
			if family == l.Refined && len(b.OfKind(in.ItemID, catalog.RecipeRefine)) == 0 {
				t.Fatalf("%s 的下级料 %s 在快照里没有配方", r.ItemID, in.ItemID)
			}
		}
	}
}

func heartLine(id string) (Line, bool) {
	for _, l := range Lines {
		if l.Heart == id {
			return l, true
		}
	}
	return Line{}, false
}

// id 映射能来回转:快照里出现的每个 id,dump 形式和市场形式一一对应;
// 拼出来的 id 也能解析回 (族, tier, 附魔)。
func TestID映射能来回转(t *testing.T) {
	b := book(t)
	seen := map[string]bool{}
	for _, r := range b.All() {
		seen[r.ItemID] = true
		for _, in := range r.Inputs {
			seen[in.ItemID] = true
		}
	}
	for id := range seen {
		if got := catalog.MarketID(catalog.DumpID(id), -1); got != id {
			t.Fatalf("%s → dump %s → %s", id, catalog.DumpID(id), got)
		}
	}
	for _, l := range Lines {
		for tier := MinTier; tier <= MaxTier; tier++ {
			for _, e := range Enchants(tier) {
				for _, family := range []string{l.Raw, l.Refined} {
					id := ItemID(family, tier, e)
					f, gt, ge, ok := ParseItemID(id)
					if !ok || f != family || gt != tier || ge != e {
						t.Fatalf("%s 解析成 (%s, %d, %d, %v)", id, f, gt, ge, ok)
					}
					// dump 里附魔资源是独立条目,名字带 _LEVELn、不带 @n
					dump := catalog.DumpID(id)
					if want := strings.TrimSuffix(id, "@"+strconv.Itoa(e)); dump != want {
						t.Fatalf("%s 的 dump id 是 %s,想要 %s", id, dump, want)
					}
					if catalog.MarketID(dump, e) != id {
						t.Fatalf("%s 按 @enchantmentlevel=%d 转不回来", dump, e)
					}
				}
			}
		}
	}
	if got := ItemID("ORE", 8, 4); got != "T8_ORE_LEVEL4@4" {
		t.Fatalf("ItemID = %s", got)
	}
	for _, bad := range []string{"T8_ORE_LEVEL4", "T8_ORE_LEVEL4@3", "T1_FACTION_MOUNTAIN_TOKEN_1", "T8_2H_FIRESTAFF@1"} {
		if _, _, _, ok := ParseItemID(bad); ok {
			t.Fatalf("%s 不该被当成市场资源 id", bad)
		}
	}
}

// 快照是编进二进制的兜底,来源要标清楚,排查时能看出用的不是库里那份。
func Test内置快照标明来源(t *testing.T) {
	b := book(t)
	if b.Source() != SnapshotName || b.SyncedAt() != SnapshotDataDate {
		t.Fatalf("来源 %s,版本 %s", b.Source(), b.SyncedAt())
	}
}
