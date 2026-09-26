package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"albion-guild/internal/catalog"
)

func TestRecipes入库再读回来一致(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// 迁移刚跑完表是空的:上层据此决定去同步还是用内置快照
	got, syncedAt, err := st.LoadRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || syncedAt != "" {
		t.Fatalf("空表读出 %d 条、同步时间 %q", len(got), syncedAt)
	}

	zero := 0
	recipes := []catalog.Recipe{
		{ItemID: "T8_METALBAR_LEVEL4@4", Variant: 0, Kind: catalog.RecipeRefine, Focus: 4714, Amount: 1,
			ItemValue: 4096, Inputs: []catalog.RecipeInput{
				{ItemID: "T8_ORE_LEVEL4@4", Count: 5}, {ItemID: "T7_METALBAR_LEVEL4@4", Count: 1}}},
		{ItemID: "T8_ORE_LEVEL4@4", Variant: 0, Kind: catalog.RecipeTransmute, Silver: 779_625, Amount: 1,
			ItemValue: 409.6, UnlockedToCraft: true,
			Inputs: []catalog.RecipeInput{{ItemID: "T7_ORE_LEVEL4@4", Count: 1, MaxReturn: &zero}}},
		{ItemID: "T8_ORE_LEVEL4@4", Variant: 1, Kind: catalog.RecipeTransmute, Silver: 748_440, Amount: 1,
			ItemValue: 409.6, UnlockedToCraft: true,
			Inputs: []catalog.RecipeInput{{ItemID: "T8_ORE_LEVEL3@3", Count: 1, MaxReturn: &zero}}},
	}
	before := time.Now().Add(-time.Minute)
	if err := st.SaveRecipes(ctx, recipes); err != nil {
		t.Fatal(err)
	}
	got, syncedAt, err = st.LoadRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 「没写 maxreturnamount」和「写了 0」意思相反(返还 / 不返还),过一趟 jsonb 不能混
	if !reflect.DeepEqual(got, recipes) {
		t.Fatalf("读回来不一致\n得到 %+v\n想要 %+v", got, recipes)
	}
	if got[0].Inputs[0].MaxReturn != nil || got[1].Inputs[0].MaxReturn == nil {
		t.Fatal("max_return 的缺省和 0 在库里混了")
	}
	at, err := time.Parse(time.RFC3339, syncedAt)
	if err != nil || at.Before(before) {
		t.Fatalf("同步时间 %q 不对: %v", syncedAt, err)
	}

	// 整份替换:上游删掉的配方不能留在库里
	if err := st.SaveRecipes(ctx, recipes[:1]); err != nil {
		t.Fatal(err)
	}
	got, _, err = st.LoadRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ItemID != "T8_METALBAR_LEVEL4@4" {
		t.Fatalf("替换后剩下 %+v", got)
	}

	// 同一个 (产物, variant) 出现两次是解析出了错,不能静默吞掉
	dup := []catalog.Recipe{recipes[1], recipes[1]}
	if err := st.SaveRecipes(ctx, dup); err == nil {
		t.Fatal("重复的 (产物, variant) 应当写库失败")
	}
	// 失败的那次整个回滚,原来的数据还在
	got, _, err = st.LoadRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("写库失败后剩下 %d 条,想要原来的 1 条", len(got))
	}
}
