package flip

import (
	"context"
	"testing"

	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/refine"
)

// 还没载入过配方(目录没同步、库读失败)时,精炼也要有配方可用:退回内置快照。
func TestRecipes没载入时用内置快照(t *testing.T) {
	s := New(nil, conf.Default())
	b := s.Recipes()
	if b == nil || b.Len() == 0 {
		t.Fatal("没载入时应当退回内置快照")
	}
	if b.Source() != refine.SnapshotName {
		t.Fatalf("来源 %s,想要 %s", b.Source(), refine.SnapshotName)
	}
	if got := b.OfKind("T8_ORE_LEVEL4@4", catalog.RecipeTransmute); len(got) != 2 {
		t.Fatalf("8.4 矿转化配方 %d 条", len(got))
	}
}

// 上游解析不出配方时不能把已有的换成空的——整表替换会把好数据一起抹掉。
func TestSaveRecipes上游为空时保持原样(t *testing.T) {
	s := New(nil, conf.Default())
	prev := catalog.NewRecipeBook([]catalog.Recipe{{ItemID: "T4_ORE_LEVEL1@1", Kind: catalog.RecipeTransmute,
		Silver: 1500, Amount: 1, Inputs: []catalog.RecipeInput{{ItemID: "T4_ORE", Count: 1}}}},
		"2026-09-23T00:00:00Z", catalog.RecipesFromDB)
	s.recipes.Store(prev)
	// Store 是 nil:要是走到了写库这一步会直接 panic
	s.saveRecipes(context.Background(), nil)
	if s.Recipes() != prev {
		t.Fatal("上游为空时内存里的配方被换掉了")
	}
	if st := s.LastRecipeSync(); st == nil || st.Rejected == "" || st.Upstream != 0 {
		t.Fatalf("要记下这次没收: %+v", st)
	}
}

// 上游只解析出一部分(比如转化按钮的本地化键改了名,转化配方整批没了):整份不收,
// 不换内存、不写库,并记下原因给 /api/catalog/sync(审查 finding 6)。以前只告警一句就照样整表替换。
func TestSaveRecipes上游不完整时整份不收(t *testing.T) {
	all := refine.EmbeddedRecipes().All()
	var refineOnly, missingOne []catalog.Recipe
	for i, r := range all {
		if r.Kind == catalog.RecipeRefine {
			refineOnly = append(refineOnly, r)
		}
		if i != len(all)/2 {
			missingOne = append(missingOne, r)
		}
	}
	// 范围外的配方(石头线)多出来不算,缺的照样是缺
	extra := catalog.Recipe{ItemID: "T5_STONEBLOCK", Kind: catalog.RecipeRefine, Amount: 1,
		Inputs: []catalog.RecipeInput{{ItemID: "T5_ROCK", Count: 1}}}
	for name, upstream := range map[string][]catalog.Recipe{
		"只有精炼没有转化":  refineOnly,
		"少一条":       missingOne,
		"少一条但多了石头线": append(append([]catalog.Recipe(nil), missingOne...), extra),
	} {
		s := New(nil, conf.Default())
		// Store 是 nil:走到写库就会 panic,所以没 panic 就说明没写库
		s.saveRecipes(context.Background(), upstream)
		if src := s.Recipes().Source(); src != refine.SnapshotName {
			t.Fatalf("%s:不完整的上游配方不该换进内存,来源 %s", name, src)
		}
		st := s.LastRecipeSync()
		if st == nil || st.Rejected == "" || st.Upstream != len(upstream) {
			t.Fatalf("%s:要记下没收和原因: %+v", name, st)
		}
		t.Logf("%s:%s", name, st.Rejected)
	}

	// 完整的(和快照一样,或者比快照多)可以用
	if ok, why := recipesUsable(all); !ok {
		t.Fatalf("和快照一样的配方应当可用: %s", why)
	}
	if ok, why := recipesUsable(append(append([]catalog.Recipe(nil), all...), extra)); !ok {
		t.Fatalf("比快照多的配方应当可用: %s", why)
	}
	if ok, _ := recipesUsable(refineOnly); ok {
		t.Fatal("缺整批转化配方的不能用")
	}
}
