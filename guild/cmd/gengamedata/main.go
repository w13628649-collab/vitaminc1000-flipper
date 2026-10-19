// gengamedata 从游戏客户端解出来的静态数据生成 internal/gamedata/tables_gen.go:
//
//   - 专精:achievements.xml 里每个成就的下标(事件 150 FullAchievementInfo 报的就是它)
//     和类型,四条精炼线 + 石头 T4~T8 节点各在第几个
//   - 精炼站:buildings.xml 里每个建筑的类型 id(op 197 key17 报的就是它)→ 精炼线
//
// 这两种下标都是"在 xml 里排第几",客户端升版插一条就整体漂移,所以不手写,升版后重跑:
//
//	go run ./cmd/gengamedata -root ../rea-test/investigations/albion-online/static-data
//	go test ./internal/gamedata   # 有源数据时核对生成物和源数据一致
//
// 源数据是 rea-test 调查工作区解出来的 decoded/*.xml(docs/client-data.md),
// 客户端版本取 source-manifest.json 的 local_client_build。
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Lines 是生成表里的线,和 refine.Lines 的 Key 一致,另加石头(专精城 Bridgewatch 也要记录)
var Lines = []string{"ORE", "HIDE", "FIBER", "WOOD", "ROCK"}

// refinedOf 是每条线精炼产物的 uniquename 片段:T?_<它>(_LEVELn)
var refinedOf = map[string]string{
	"ORE": "METALBAR", "HIDE": "LEATHER", "FIBER": "CLOTH", "WOOD": "PLANKS", "ROCK": "STONEBLOCK",
}

// Tables 是生成出来的全部内容。
type Tables struct {
	ClientBuild string
	// AchievementRows[i] 是第 i 个成就的等级行数:0 = 普通成就(只有完成 / 没完成),
	// 否则是模板各段等级行的总数(精炼 100;战斗 100 + 精英 20)。事件 150 的核对用它
	AchievementRows []uint8
	// SpecIndex 是每条线 T4~T8 精炼专精节点的成就下标
	SpecIndex map[string][5]int
	// StationLines 是精炼站的建筑类型 id → 线
	StationLines map[int32]string
}

func main() {
	root := flag.String("root", "../rea-test/investigations/albion-online/static-data", "静态数据调查目录(含 decoded/ 和 source-manifest.json)")
	out := flag.String("out", "internal/gamedata/tables_gen.go", "生成的 Go 文件")
	flag.Parse()
	log.SetFlags(0)

	t, err := Build(*root)
	if err != nil {
		log.Fatal(err)
	}
	src, err := Render(t)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s:%s,%d 个成就,%d 种精炼站", *out, t.ClientBuild, len(t.AchievementRows), len(t.StationLines))
}

// Build 读源数据算出全部表。
func Build(root string) (Tables, error) {
	var t Tables
	mf, err := os.ReadFile(filepath.Join(root, "source-manifest.json"))
	if err != nil {
		return t, err
	}
	var m struct {
		Build string `json:"local_client_build"`
	}
	if err := json.Unmarshal(mf, &m); err != nil || m.Build == "" {
		return t, fmt.Errorf("source-manifest.json 里没有 local_client_build: %v", err)
	}
	t.ClientBuild = m.Build

	f, err := os.Open(filepath.Join(root, "decoded", "achievements.xml"))
	if err != nil {
		return t, err
	}
	defer f.Close()
	if t.AchievementRows, t.SpecIndex, err = achievements(f); err != nil {
		return t, fmt.Errorf("achievements.xml: %w", err)
	}

	b, err := os.Open(filepath.Join(root, "decoded", "buildings.xml"))
	if err != nil {
		return t, err
	}
	defer b.Close()
	if t.StationLines, err = stations(b); err != nil {
		return t, fmt.Errorf("buildings.xml: %w", err)
	}
	return t, nil
}

// topLevel 依次把根元素的每个直接子元素交给 fn(注释、文本跳过)。
// fn 必须把这个元素读完(DecodeElement 或 Skip)
func topLevel(r io.Reader, fn func(d *xml.Decoder, se xml.StartElement) error) error {
	d := xml.NewDecoder(r)
	depth := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch x := tok.(type) {
		case xml.StartElement:
			if depth == 1 {
				if err := fn(d, x); err != nil {
					return err
				}
				continue // fn 读到了它的结束标签,深度不变
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

var specRe = regexp.MustCompile(`^CRAFT_REFINE_([A-Z]+)_T([4-8])$`)

// achievements:下标是根下 achievement / templateachievement 按出现顺序数(从 0 起,
// 和 AFMDataClientCore 的 AchievementsService 一致;2026-10-19 拿真样本核过,
// 四条线的等级和游戏里的进度、事件 152 的名望逐级对得上)
func achievements(r io.Reader) ([]uint8, map[string][5]int, error) {
	type entry struct{ id, tmpl string }
	var list []entry
	tmplRows := map[string]int{}
	err := topLevel(r, func(d *xml.Decoder, se xml.StartElement) error {
		switch se.Name.Local {
		case "achievement":
			list = append(list, entry{id: attr(se, "id")})
			return d.Skip()
		case "templateachievement":
			list = append(list, entry{id: attr(se, "id"), tmpl: attr(se, "usetemplate")})
			return d.Skip()
		case "template":
			// 等级行分几段:baselevels(精炼 100 行)、战斗专精再加 elitelevels(20 行),都算
			var tp struct {
				Parts []struct {
					XMLName xml.Name
					Text    string `xml:",chardata"`
				} `xml:",any"`
			}
			if err := d.DecodeElement(&tp, &se); err != nil {
				return err
			}
			n := 0
			for _, p := range tp.Parts {
				if !strings.HasSuffix(p.XMLName.Local, "levels") {
					continue
				}
				for _, l := range strings.Split(p.Text, "\n") {
					if strings.TrimSpace(l) != "" {
						n++
					}
				}
			}
			tmplRows[attr(se, "name")] = n
			return nil
		}
		return d.Skip()
	})
	if err != nil {
		return nil, nil, err
	}
	rows := make([]uint8, len(list))
	spec := map[string][5]int{}
	found := map[string]int{}
	for i, e := range list {
		if e.tmpl != "" {
			n, ok := tmplRows[e.tmpl]
			if !ok || n <= 0 || n > 255 {
				return nil, nil, fmt.Errorf("%s 用的模板 %q 没有等级行(或超过 255 行)", e.id, e.tmpl)
			}
			rows[i] = uint8(n)
		}
		if m := specRe.FindStringSubmatch(e.id); m != nil && slices.Contains(Lines, m[1]) {
			if e.tmpl == "" {
				return nil, nil, fmt.Errorf("%s 不是模板成就,不像专精节点", e.id)
			}
			s := spec[m[1]]
			s[m[2][0]-'4'] = i
			spec[m[1]] = s
			found[m[1]]++
		}
	}
	for _, l := range Lines {
		if found[l] != 5 {
			return nil, nil, fmt.Errorf("%s 线找到 %d 个 T4~T8 精炼节点,应当是 5 个", l, found[l])
		}
	}
	return rows, spec, nil
}

// stations:类型 id 是根下全部元素按出现顺序数、**从 1 起**。
// 2026-10-19 拿 N 地图 495 台站对过:Thetford / Fort Sterling / Lymhurst 的 27/28/29 正好是
// 沼泽 / 山地 / 森林的绿区建筑,三城抓到最多的分别是冶炼炉 88 台、木工坊 32 台、织布坊 34 台,
// 正是各城的精炼专精线。从 0 数的话 Thetford 会落在山地
//
// 一个建筑能造某条线 T?_<产物>(_LEVELn) 就算那条线的精炼站。教学关卡的不要
func stations(r io.Reader) (map[int32]string, error) {
	out := map[int32]string{}
	idx := 0
	err := topLevel(r, func(d *xml.Decoder, se xml.StartElement) error {
		idx++
		var b struct {
			Items []struct {
				Name string `xml:"uniquename,attr"`
			} `xml:"craftingitemlist>craftitem"`
		}
		if err := d.DecodeElement(&b, &se); err != nil {
			return err
		}
		name := attr(se, "uniquename")
		if strings.Contains(name, "TUTORIAL") {
			return nil
		}
		var lines []string
		for _, it := range b.Items {
			if l := lineOf(it.Name); l != "" && !slices.Contains(lines, l) {
				lines = append(lines, l)
			}
		}
		switch len(lines) {
		case 0:
		case 1:
			out[int32(idx)] = lines[0]
		default:
			return fmt.Errorf("建筑 %s 能精炼 %v 几条线,一个类型只能对一条", name, lines)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, l := range Lines {
		n := 0
		for _, v := range out {
			if v == l {
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("%s 线一种精炼站都没找到", l)
		}
	}
	return out, nil
}

var refinedRe = regexp.MustCompile(`^T[2-8]_([A-Z]+)(_LEVEL[1-4])?$`)

func lineOf(item string) string {
	m := refinedRe.FindStringSubmatch(item)
	if m == nil {
		return ""
	}
	for l, p := range refinedOf {
		if m[1] == p {
			return l
		}
	}
	return ""
}

// Render 把表写成 Go 源码(gofmt 过)。
func Render(t Tables) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by cmd/gengamedata; DO NOT EDIT.\n\npackage gamedata\n\n")
	fmt.Fprintf(&b, "// ClientBuild 是生成这份表的客户端版本(source-manifest.json)。\nconst ClientBuild = %q\n\n", t.ClientBuild)
	b.WriteString("// achievementRows[i]:0 = 普通成就,否则是模板的等级行数。\nvar achievementRows = [...]uint8{")
	for i, n := range t.AchievementRows {
		if i%32 == 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d,", n)
	}
	b.WriteString("\n}\n\n// specIndex:线 → T4~T8 精炼专精节点的成就下标。\nvar specIndex = map[string][5]int{\n")
	for _, l := range Lines {
		s := t.SpecIndex[l]
		fmt.Fprintf(&b, "%q: {%d, %d, %d, %d, %d},\n", l, s[0], s[1], s[2], s[3], s[4])
	}
	b.WriteString("}\n\n// stationLines:精炼站的建筑类型 id → 线。\nvar stationLines = map[int32]string{\n")
	ids := make([]int32, 0, len(t.StationLines))
	for id := range t.StationLines {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "%d: %q,\n", id, t.StationLines[id])
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
