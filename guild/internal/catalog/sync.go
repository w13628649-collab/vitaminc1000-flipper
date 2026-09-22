package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	_ "embed"
)

const (
	itemsJSONURL = "https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/formatted/items.json"
	rawItemsURL  = "https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/items.json"
	// IconURL 的 {id} 由调用方替换。
	IconURL = "https://render.albiononline.com/v1/item/%s.png?size=64"
)

// categoriesSeed 是分类的中文名。它来自 90MB 的 localization.json,
// 极少变动,不值得让每个人 sync 时都下一遍,所以随包带着。
//
//go:embed data/categories.json
var categoriesSeed []byte

type namedItem struct {
	UniqueName     string            `json:"UniqueName"`
	LocalizedNames map[string]string `json:"LocalizedNames"`
}

type rawMeta struct {
	category    string
	subcategory string
	maxQuality  int
}

// Fetch 拉上游两份 dump,合并成可以直接写库的行。
func Fetch(ctx context.Context) ([]Item, []Category, error) {
	client := &http.Client{Timeout: 5 * time.Minute}

	var named []namedItem
	if err := getJSON(ctx, client, itemsJSONURL, &named); err != nil {
		return nil, nil, fmt.Errorf("拉 formatted/items.json: %w", err)
	}
	var rawPayload struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := getJSON(ctx, client, rawItemsURL, &rawPayload); err != nil {
		return nil, nil, fmt.Errorf("拉 items.json: %w", err)
	}
	meta := parseRawItems(rawPayload.Items)

	var items []Item
	for _, row := range named {
		if row.UniqueName == "" {
			continue
		}
		zh := trim(row.LocalizedNames["ZH-CN"])
		en := trim(row.LocalizedNames["EN-US"])
		if zh == "" && en == "" {
			continue // dump 里的占位条目,没有交易价值
		}
		base := row.UniqueName
		if idx := indexByte(base, '@'); idx >= 0 {
			base = base[:idx]
		}
		m, ok := meta[base]
		if !ok {
			m = rawMeta{maxQuality: 1}
		}
		tier, enchant := parseIDParts(row.UniqueName)
		items = append(items, Item{
			ItemID:      row.UniqueName,
			NameZH:      zh,
			NameEN:      en,
			Category:    m.category,
			Subcategory: m.subcategory,
			Family:      FamilyOf(row.UniqueName),
			Tier:        tier,
			Enchantment: enchant,
			MaxQuality:  m.maxQuality,
		})
	}

	categories, err := seedCategories()
	if err != nil {
		return nil, nil, err
	}
	return items, categories, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// 不要自己设 Accept-Encoding:一旦手动设了,Go 就认为调用方要自己
	// 处理压缩,不再透明解压,拿到的会是 gzip 原始字节。让它自己加
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// parseRawItems 从原始 dump 抽出 id → (大类, 子类, 品质档数)。
//
// 物品按类型散在 simpleitem / equipmentitem / weapon 等十几个节点里,
// 结构一致,统一扫一遍。
func parseRawItems(root map[string]json.RawMessage) map[string]rawMeta {
	meta := map[string]rawMeta{}
	for key, blob := range root {
		if key == "" || key[0] == '@' || key == "shopcategories" {
			continue
		}
		// 同一个节点可能是对象也可能是数组,两种都试
		var rows []map[string]any
		if err := json.Unmarshal(blob, &rows); err != nil {
			var single map[string]any
			if err := json.Unmarshal(blob, &single); err != nil {
				continue
			}
			rows = []map[string]any{single}
		}
		for _, row := range rows {
			id, _ := row["@uniquename"].(string)
			if id == "" {
				continue
			}
			maxQuality := 1
			switch v := row["@maxqualitylevel"].(type) {
			case string:
				if n, err := strconv.Atoi(v); err == nil {
					maxQuality = n
				}
			case float64:
				maxQuality = int(v)
			}
			cat, _ := row["@shopcategory"].(string)
			sub, _ := row["@shopsubcategory1"].(string)
			meta[id] = rawMeta{category: cat, subcategory: sub, maxQuality: maxQuality}
		}
	}
	return meta
}

func seedCategories() ([]Category, error) {
	var spec struct {
		Tree []struct {
			ID   string   `json:"id"`
			Subs []string `json:"subs"`
		} `json:"tree"`
		Names map[string][]string `json:"names"`
	}
	if err := json.Unmarshal(categoriesSeed, &spec); err != nil {
		return nil, fmt.Errorf("解析内置分类表: %w", err)
	}
	label := func(id string) (zh, en string) {
		if pair, ok := spec.Names[id]; ok && len(pair) == 2 {
			return pair[0], pair[1]
		}
		return "", id
	}
	var out []Category
	for index, node := range spec.Tree {
		zh, en := label(node.ID)
		out = append(out, Category{ID: node.ID, LabelZH: zh, LabelEN: en, Sort: index})
		for subIndex, sid := range node.Subs {
			szh, sen := label(sid)
			out = append(out, Category{
				ID: sid, Parent: node.ID, LabelZH: szh, LabelEN: sen, Sort: subIndex,
			})
		}
	}
	return out, nil
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}
