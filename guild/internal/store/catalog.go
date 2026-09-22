package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/catalog"
)

// LoadCatalog 把整份目录读进内存。一万一千条、几 MB,
// 搜索和浏览在内存里做比每次打数据库快得多。
func (s *Store) LoadCatalog(ctx context.Context) ([]catalog.Item, []catalog.Category, string, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, name_zh, name_en, category,
		subcategory, family, tier, enchant, max_quality FROM item`)
	if err != nil {
		return nil, nil, "", err
	}
	var items []catalog.Item
	for rows.Next() {
		var it catalog.Item
		if err := rows.Scan(&it.ItemID, &it.NameZH, &it.NameEN, &it.Category,
			&it.Subcategory, &it.Family, &it.Tier, &it.Enchantment, &it.MaxQuality); err != nil {
			rows.Close()
			return nil, nil, "", err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, "", err
	}

	catRows, err := s.pool.Query(ctx,
		`SELECT id, parent, label_zh, label_en, sort FROM item_category ORDER BY sort`)
	if err != nil {
		return nil, nil, "", err
	}
	var cats []catalog.Category
	for catRows.Next() {
		var c catalog.Category
		if err := catRows.Scan(&c.ID, &c.Parent, &c.LabelZH, &c.LabelEN, &c.Sort); err != nil {
			catRows.Close()
			return nil, nil, "", err
		}
		cats = append(cats, c)
	}
	catRows.Close()
	if err := catRows.Err(); err != nil {
		return nil, nil, "", err
	}

	var syncedAt string
	err = s.pool.QueryRow(ctx,
		`SELECT value FROM catalog_meta WHERE key = 'synced_at'`).Scan(&syncedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, "", err
	}
	return items, cats, syncedAt, nil
}

// SaveCatalog 整份替换。目录是上游 dump 的镜像,增量合并没有意义,
// 反而会把上游删掉的条目永久留在库里。
func (s *Store) SaveCatalog(ctx context.Context, items []catalog.Item, cats []catalog.Category) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM item`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM item_category`); err != nil {
		return err
	}

	itemRows := make([][]any, 0, len(items))
	for _, i := range items {
		itemRows = append(itemRows, []any{i.ItemID, i.NameZH, i.NameEN, i.Category,
			i.Subcategory, i.Family, int16(i.Tier), int16(i.Enchantment), int16(i.MaxQuality)})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"item"},
		[]string{"item_id", "name_zh", "name_en", "category", "subcategory",
			"family", "tier", "enchant", "max_quality"},
		pgx.CopyFromRows(itemRows)); err != nil {
		return err
	}

	catRows := make([][]any, 0, len(cats))
	for _, c := range cats {
		catRows = append(catRows, []any{c.ID, c.Parent, c.LabelZH, c.LabelEN, c.Sort})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"item_category"},
		[]string{"id", "parent", "label_zh", "label_en", "sort"},
		pgx.CopyFromRows(catRows)); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO catalog_meta (key, value) VALUES
		('synced_at', $1), ('item_count', $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		time.Now().UTC().Format(time.RFC3339), strconv.Itoa(len(items))); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Icon 读缓存的图标。第二个返回值是 false 表示库里还没有这条记录,
// 调用方该去上游拉一次。
func (s *Store) Icon(ctx context.Context, itemID string) (png []byte, cached bool, err error) {
	var missing bool
	err = s.pool.QueryRow(ctx,
		`SELECT png, missing FROM item_icon WHERE item_id = $1`, itemID).Scan(&png, &missing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if missing {
		return nil, true, nil
	}
	return png, true, nil
}

// SaveIcon 记下这个图标,png 为空表示上游确实没有,记成 missing
// 就不会每次翻页都再去问一遍。
func (s *Store) SaveIcon(ctx context.Context, itemID string, png []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO item_icon (item_id, png, fetched_at, missing)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (item_id) DO UPDATE SET
			png = EXCLUDED.png, fetched_at = EXCLUDED.fetched_at, missing = EXCLUDED.missing`,
		itemID, png, len(png) == 0)
	return err
}
