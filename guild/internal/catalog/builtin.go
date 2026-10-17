package catalog

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	_ "embed"
)

// ClientVersion 是内置目录是从哪个客户端版本解出来的。
// 游戏大版本更新之后这个数就旧了,界面和日志上要显示出来。
const ClientVersion = "1.32.030.345319"

// builtinItems 是从**游戏客户端自己**的 items.xml + localization.xml 解出来的
// 物品目录,11,601 条,127 KB。
//
// 为什么内置而不是每次去 GitHub 拉:
//
//   - **准。** ao-bin-dumps 是社区按自己的节奏从某个版本导出的二手数据,
//     和成员实际装的客户端不保证同版本。这份是从安装目录直接解的
//   - **不依赖网络。** 服务端第一次起来就有完整目录,不会因为拉不到
//     GitHub 就整个倒爷页面 503
//   - **小。** 压缩后 127 KB,比一次 HTTP 请求的抖动代价还低
//
// 代价是它跟着客户端版本走,要人工更新(docs/client-data/extract.py)。
// 所以 Fetch(GitHub)那条路留着,当作"客户端数据过期时的临时补丁"。
//
//go:embed data/items.jsonl.gz
var builtinItems []byte

// 存成紧凑键名是因为这份文件要进二进制:
// 全称键名会让未压缩体积翻一倍多,压缩后也大三成
type rawItem struct {
	I string `json:"i"` // item_id
	Z string `json:"z"` // 中文名
	E string `json:"e"` // 英文名
	C string `json:"c"` // shopcategory
	S string `json:"s"` // shopsubcategory1
	T int    `json:"t"` // tier
	N int    `json:"n"` // 附魔等级
	Q int    `json:"q"` // 品质档数
}

// Builtin 读内置目录。返回的形状和 Fetch 一致,调用方可以互换。
func Builtin() ([]Item, []Category, error) {
	zr, err := gzip.NewReader(strings.NewReader(string(builtinItems)))
	if err != nil {
		return nil, nil, fmt.Errorf("内置目录解压失败: %w", err)
	}
	defer zr.Close()

	blob, err := io.ReadAll(zr)
	if err != nil {
		return nil, nil, fmt.Errorf("内置目录读取失败: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(blob)), "\n")
	items := make([]Item, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		var r rawItem
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, nil, fmt.Errorf("内置目录第 %d 行解析失败: %w", i+1, err)
		}
		if r.Q < 1 {
			r.Q = 1
		}
		items = append(items, Item{
			ItemID: r.I, NameZH: r.Z, NameEN: r.E,
			Category: r.C, Subcategory: r.S,
			Family:      FamilyOf(r.I),
			Tier:        r.T,
			Enchantment: r.N,
			MaxQuality:  r.Q,
		})
	}
	if len(items) == 0 {
		return nil, nil, fmt.Errorf("内置目录是空的")
	}

	cats, err := seedCategories()
	if err != nil {
		return nil, nil, err
	}
	return items, cats, nil
}
