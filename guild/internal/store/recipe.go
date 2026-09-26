package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/catalog"
)

// LoadRecipes 读出全部配方。第二个返回值是最近一次同步时间(RFC3339),
// 表是空的时候为空串。
func (s *Store) LoadRecipes(ctx context.Context) ([]catalog.Recipe, string, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, variant, kind, silver, focus, amount,
		item_value, unlocked_to_craft, inputs, synced_at FROM recipe ORDER BY item_id, variant`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var (
		out    []catalog.Recipe
		latest time.Time
	)
	for rows.Next() {
		var (
			r        catalog.Recipe
			variant  int16
			kind     string
			inputs   []byte
			syncedAt time.Time
		)
		if err := rows.Scan(&r.ItemID, &variant, &kind, &r.Silver, &r.Focus, &r.Amount,
			&r.ItemValue, &r.UnlockedToCraft, &inputs, &syncedAt); err != nil {
			return nil, "", err
		}
		r.Variant = int(variant)
		r.Kind = catalog.RecipeKind(kind)
		if err := json.Unmarshal(inputs, &r.Inputs); err != nil {
			return nil, "", fmt.Errorf("配方 %s/%d 的材料表: %w", r.ItemID, r.Variant, err)
		}
		if syncedAt.After(latest) {
			latest = syncedAt
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(out) == 0 {
		return nil, "", nil
	}
	return out, latest.UTC().Format(time.RFC3339), nil
}

// SaveRecipes 整份替换。和 SaveCatalog 一样:配方是上游 dump 的镜像,
// 增量合并会把上游删掉的配方永久留在库里。
func (s *Store) SaveRecipes(ctx context.Context, recipes []catalog.Recipe) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM recipe`); err != nil {
		return err
	}
	now := time.Now().UTC()
	rows := make([][]any, 0, len(recipes))
	for _, r := range recipes {
		inputs, err := json.Marshal(r.Inputs)
		if err != nil {
			return err
		}
		rows = append(rows, []any{r.ItemID, int16(r.Variant), string(r.Kind), r.Silver,
			int32(r.Focus), int32(r.Amount), r.ItemValue, r.UnlockedToCraft, string(inputs), now})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"recipe"},
		[]string{"item_id", "variant", "kind", "silver", "focus", "amount",
			"item_value", "unlocked_to_craft", "inputs", "synced_at"},
		pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
