package flip

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"albion-guild/internal/catalog"
	"albion-guild/internal/refine"
)

// 配方表的载入和同步。配方和物品目录出自同一份 items.json,同步时一起拉、
// 一起写库(SyncCatalog),这里不发任何额外的外部请求。
//
// 配方出问题不影响倒爷那一套:读不出、解析不出、写库失败都只告警,
// 精炼先用编进二进制的快照(refine.EmbeddedRecipes),Source() 能看出用的是哪份。
// 上游只解析出一部分(比内置快照少)时整份不收:不换内存、不写库(见 recipesUsable)。

// Recipes 是当前生效的配方表,永远不是 nil。
func (s *Service) Recipes() *catalog.RecipeBook {
	if b := s.recipes.Load(); b != nil {
		return b
	}
	return refine.EmbeddedRecipes()
}

// RecipeSync 是最近一次同步配方的结果,/api/catalog/sync 原样报出来。
type RecipeSync struct {
	At time.Time `json:"at"`
	// Upstream 是上游解析出的配方条数,InScope 是其中四条线范围内的
	Upstream int `json:"upstream"`
	InScope  int `json:"in_scope"`
	// Rejected 非空说明这次没收:原因写在这里,内存和库里还是之前那份
	Rejected string `json:"rejected,omitempty"`
}

// LastRecipeSync 是最近一次同步配方的结果;这个进程还没同步过是 nil。
func (s *Service) LastRecipeSync() *RecipeSync { return s.recipeSync.Load() }

// recipesUsable 判断一份配方能不能换上:不能是空的,也不能比内置快照少(refine.MissingFromSnapshot)。
// 不能用时返回原因。
func recipesUsable(recipes []catalog.Recipe) (bool, string) {
	if len(recipes) == 0 {
		return false, "没解析出任何配方,多半是上游改了 items.json 的结构"
	}
	if n, sample := refine.MissingFromSnapshot(recipes); n > 0 {
		return false, fmt.Sprintf("比内置快照少 %d 条配方(例如 %s),多半是上游改了命名或结构",
			n, strings.Join(sample, "、"))
	}
	return true, ""
}

// loadRecipes 从库里读配方。库里没有(0007 之前同步的库只有目录),或者库里那份不完整,
// 就先用内置快照,在后台补同步一次——还是那两份 dump。不在这里等:目录已经在了,
// 不该为配方把服务端启动拖住;同步失败下次启动再试。
func (s *Service) loadRecipes(ctx context.Context) {
	recipes, syncedAt, err := s.Store.LoadRecipes(ctx)
	if err != nil {
		slog.Warn("读配方失败,精炼先用内置快照", "snapshot", refine.SnapshotName, "err", err)
		return
	}
	if ok, why := recipesUsable(recipes); ok {
		s.recipes.Store(catalog.NewRecipeBook(recipes, syncedAt, catalog.RecipesFromDB))
		slog.Info("配方已载入", "recipes", len(recipes), "synced_at", syncedAt)
		return
	} else if len(recipes) > 0 {
		slog.Warn("库里的配方不完整,先用内置快照,后台重新同步一次", "snapshot", refine.SnapshotName, "why", why)
	} else {
		slog.Info("库里还没有配方,先用内置快照,后台同步一次", "snapshot", refine.SnapshotName)
	}
	go func() {
		// 只补配方。走 SyncCatalog 会把内置目录一起覆盖成 ao-bin-dumps(见 SyncRecipes)
		if err := s.SyncRecipes(ctx); err != nil {
			slog.Warn("后台同步配方失败,继续用内置快照", "err", err)
		}
	}()
}

// saveRecipes 把刚同步下来的配方换进内存、写进库;不完整的整份不收。结果记进 LastRecipeSync。
func (s *Service) saveRecipes(ctx context.Context, recipes []catalog.Recipe) {
	st := &RecipeSync{At: time.Now().UTC(), Upstream: len(recipes), InScope: len(refine.TrimRecipes(recipes))}
	defer s.recipeSync.Store(st)
	if ok, why := recipesUsable(recipes); !ok {
		// 整表替换成残缺的话,之前那份好数据就没了,之后每次启动都读残缺的那份
		st.Rejected = why + ";没换,继续用 " + s.Recipes().Source()
		slog.Warn("上游配方不收,库里和内存里的保持不动", "why", why, "upstream", st.Upstream, "in_scope", st.InScope)
		return
	}
	s.recipes.Store(catalog.NewRecipeBook(recipes,
		st.At.Format(time.RFC3339), catalog.RecipesFromUpstream))
	if err := s.Store.SaveRecipes(ctx, recipes); err != nil {
		// 内存里已经是新的;库里还是旧的,下次启动读旧的,不影响这次
		slog.Warn("配方写库失败", "err", err)
		return
	}
	slog.Info("配方已同步", "recipes", len(recipes), "in_scope", st.InScope)
}
