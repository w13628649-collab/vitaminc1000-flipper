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
}
