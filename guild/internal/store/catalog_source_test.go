package store

import (
	"context"
	"testing"

	"albion-guild/internal/catalog"
)

// 老库(client-data 合并之前存的)没有 source 这一行,要读成 "" 而不是报错;
// 存过之后读回来就是最后一次写的那个来源。flip.preferBuiltin 靠这两点判断要不要换内置目录
func TestCatalogSource_老库读成空_存过之后读得回(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	got, err := st.CatalogSource(ctx)
	if err != nil || got != "" {
		t.Fatalf("没存过目录时应是空来源且不报错,得到 %q / %v", got, err)
	}

	items := []catalog.Item{{ItemID: "T4_METALBAR", NameZH: "钢条", NameEN: "Steel Bar",
		Category: "resources", Subcategory: "metalbar", Family: "METALBAR", Tier: 4, MaxQuality: 1}}
	for _, want := range []string{"client/" + catalog.ClientVersion, "ao-bin-dumps"} {
		if err := st.SaveCatalog(ctx, items, nil, want); err != nil {
			t.Fatalf("存目录(%s): %v", want, err)
		}
		got, err := st.CatalogSource(ctx)
		if err != nil || got != want {
			t.Fatalf("来源应是 %q,得到 %q / %v", want, got, err)
		}
	}
}
