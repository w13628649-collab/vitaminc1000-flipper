package flip

import (
	"context"
	"log/slog"
	"time"

	"albion-guild/internal/catalog"
	"albion-guild/internal/refine"
)

// 配方表的载入和同步。配方和物品目录出自同一份 items.json,同步时一起拉、
// 一起写库(SyncCatalog),这里不发任何额外的外部请求。
//
// 配方出问题不影响倒爷那一套:读不出、解析不出、写库失败都只告警,
// 精炼先用编进二进制的快照(refine.EmbeddedRecipes),Source() 能看出用的是哪份。

// Recipes 是当前生效的配方表,永远不是 nil。
func (s *Service) Recipes() *catalog.RecipeBook {
	if b := s.recipes.Load(); b != nil {
		return b
	}
	return refine.EmbeddedRecipes()
}

// loadRecipes 从库里读配方。库里没有(0007 之前同步的库只有目录)就先用内置快照,
// 在后台补同步一次——还是那两份 dump。不在这里等:目录已经在了,
// 不该为配方把服务端启动拖住;同步失败下次启动再试。
func (s *Service) loadRecipes(ctx context.Context) {
	recipes, syncedAt, err := s.Store.LoadRecipes(ctx)
	if err != nil {
		slog.Warn("读配方失败,精炼先用内置快照", "snapshot", refine.SnapshotName, "err", err)
		return
	}
	if len(recipes) > 0 {
		s.recipes.Store(catalog.NewRecipeBook(recipes, syncedAt, catalog.RecipesFromDB))
		slog.Info("配方已载入", "recipes", len(recipes), "synced_at", syncedAt)
		return
	}
	slog.Info("库里还没有配方,先用内置快照,后台同步一次", "snapshot", refine.SnapshotName)
	go func() {
		if err := s.SyncCatalog(ctx); err != nil {
			slog.Warn("后台同步配方失败,继续用内置快照", "err", err)
		}
	}()
}

// saveRecipes 把刚同步下来的配方换进内存、写进库。
func (s *Service) saveRecipes(ctx context.Context, recipes []catalog.Recipe) {
	if len(recipes) == 0 {
		// 多半是上游改了 items.json 的结构。整表替换成空的话,之前那份好数据就没了
		slog.Warn("上游 items.json 里没解析出配方,库里和内存里的保持不动")
		return
	}
	inScope := len(refine.TrimRecipes(recipes))
	if want := refine.EmbeddedRecipes().Len(); inScope < want {
		slog.Warn("上游配方里四条线的条数比内置快照少,可能是上游改了命名或结构",
			"in_scope", inScope, "snapshot", want)
	}
	s.recipes.Store(catalog.NewRecipeBook(recipes,
		time.Now().UTC().Format(time.RFC3339), catalog.RecipesFromUpstream))
	if err := s.Store.SaveRecipes(ctx, recipes); err != nil {
		// 内存里已经是新的;库里还是旧的,下次启动读旧的,不影响这次
		slog.Warn("配方写库失败", "err", err)
		return
	}
	slog.Info("配方已同步", "recipes", len(recipes), "in_scope", inScope)
}
