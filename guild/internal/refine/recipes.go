package refine

import (
	"encoding/json"
	"fmt"
	"sync"

	_ "embed"

	"albion-guild/internal/catalog"
)

// 内置配方快照:库里还没有配方(第一次启动、同步失败)时兜底。
//
// 平时用的是和物品目录一起从 ao-bin-dumps 同步进库的那份(catalog.Fetch →
// recipe 表),游戏更新后跟着上游走;这份只在那条路走不通时顶上,
// 同时是 recipes_test 里 golden 断言的基准。
//
// 只收四条线 T2~T8 的原料转化和半成品精炼(含心配方),约 350 条、几十 KB,
// 全是公开的游戏数据。重新生成:
//
//	FLIPPER_ITEMS_JSON=<原始 items.json 路径> go test ./internal/refine -run Test内置快照 -update
const (
	// SnapshotName 是内置快照在 RecipeBook.Source() 里的名字
	SnapshotName = "embedded:recipes_20260923"
	// SnapshotDataDate 是快照对应的 dump 版本日期(ao-bin-dumps 90b141e)
	SnapshotDataDate = "2026-09-23"
)

//go:embed data/recipes_20260923.json
var snapshotJSON []byte

type snapshotFile struct {
	Source  string           `json:"source"`
	Scope   string           `json:"scope"`
	Recipes []catalog.Recipe `json:"recipes"`
}

var embedded = sync.OnceValues(func() (*catalog.RecipeBook, error) {
	var snap snapshotFile
	if err := json.Unmarshal(snapshotJSON, &snap); err != nil {
		return nil, fmt.Errorf("解析内置配方快照: %w", err)
	}
	return catalog.NewRecipeBook(snap.Recipes, SnapshotDataDate, SnapshotName), nil
})

// EmbeddedRecipes 是内置快照。快照是编进二进制的,解析失败只可能是改坏了文件,
// 测试会先挂;这里直接 panic,不让一份坏数据悄悄变成「没有配方」。
func EmbeddedRecipes() *catalog.RecipeBook {
	book, err := embedded()
	if err != nil {
		panic(err)
	}
	return book
}

// InScope:这条配方的产物是不是四条线 T2~T8 的原料或半成品。
//
// 只按产物筛,不按配方种类筛:上游哪天给原料加了精炼配方,应该在 golden 测试里
// 露出来,而不是被这里悄悄滤掉。
func InScope(r catalog.Recipe) bool {
	family, tier, _, ok := ParseItemID(r.ItemID)
	if !ok || tier < MinTier || tier > MaxTier {
		return false
	}
	_, ok = LineOf(family)
	return ok
}

// TrimRecipes 从全部配方里挑出本期范围内的,生成快照用。
func TrimRecipes(all []catalog.Recipe) []catalog.Recipe {
	var out []catalog.Recipe
	for _, r := range all {
		if InScope(r) {
			out = append(out, r)
		}
	}
	catalog.SortRecipes(out)
	return out
}
