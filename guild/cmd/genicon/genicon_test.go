package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 仓库里提交的生成物必须和 logo.go 的几何对得上:改了几何忘了重新生成,这里会红。
//
// 比的是解码后的像素,不是文件字节——PNG 压缩器的输出随 Go 版本会变,
// 字节比对会在升级 Go 之后无缘无故地红。

var (
	clientDir = filepath.Join("..", "client")
	webuiDir  = filepath.Join("..", "..", "internal", "webui", "assets")
)

func TestCommittedPNGs(t *testing.T) {
	for _, s := range sizes {
		name := filepath.Join(clientDir, "icon", fmt.Sprintf("icon-%d.png", s))
		samePixels(t, name, decodePNG(t, name, readFile(t, name)), want(s))
	}
}

func TestCommittedWebPNGs(t *testing.T) {
	for _, s := range webSizes {
		name := filepath.Join(webuiDir, fmt.Sprintf("logo-%d.png", s))
		samePixels(t, name, decodePNG(t, name, readFile(t, name)), want(s))
	}
}

// 吸附之后每块锭的顶点都在整像素上,且每块锭自己左右对称——
// 以前顶边两个顶点各自吸附,32/40px 下铜锭一侧斜 1px、一侧斜 2px。
func TestIngotsSnapSymmetric(t *testing.T) {
	// 坐标是网格单位,k 不是 2 的幂时换算有浮点误差
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	for _, s := range slices.Concat(sizes, webSizes) {
		px := pixelsFor(s)
		var ingots int
		for i, l := range logo(px) {
			p, ok := l.shape.(poly)
			if !ok {
				continue
			}
			ingots++
			for _, v := range p {
				for _, c := range []float64{v.X, v.Y} {
					if f := c * px.k; !near(f, math.Round(f)) {
						t.Errorf("%dpx 第 %d 层顶点 %v 不在整像素上(%g px)", s, i, v, f)
					}
				}
			}
			// 顶点顺序:左上、右上、右下、左下
			left, right := (p[0].X-p[3].X)*px.k, (p[2].X-p[1].X)*px.k
			if !near(left, right) || left < 1-1e-9 {
				t.Errorf("%dpx 第 %d 层左侧内收 %gpx、右侧 %gpx", s, i, left, right)
			}
		}
		if ingots != 3 {
			t.Fatalf("%dpx 有 %d 块锭", s, ingots)
		}
	}
}

// 整张图左右镜像一致。几何对称而光栅化有偏差(采样点、半开区间)的话这里会红。
func TestRenderMirrorSymmetric(t *testing.T) {
	for _, s := range slices.Concat(sizes, webSizes) {
		img := want(s)
		for y := range s {
			for x := range s / 2 {
				a, b := img.NRGBAAt(x, y), img.NRGBAAt(s-1-x, y)
				for _, d := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}, {a.A, b.A}} {
					if diff := int(d[0]) - int(d[1]); diff > 2 || diff < -2 {
						t.Fatalf("%dpx 第 %d 行:第 %d 列 %v 和镜像列 %v 不一样", s, y, x, a, b)
					}
				}
			}
		}
	}
}

func TestCommittedSVG(t *testing.T) {
	// Windows 检出是 CRLF
	got := strings.ReplaceAll(string(readFile(t, filepath.Join(webuiDir, "logo.svg"))), "\r\n", "\n")
	if got != svgDoc() {
		t.Fatalf("logo.svg 和几何对不上,在 guild 目录下跑 go generate ./cmd/client\n仓库里:\n%s\n应该是:\n%s", got, svgDoc())
	}
}

func TestCommittedICO(t *testing.T) {
	name := filepath.Join(clientDir, "icon", "icon.ico")
	b := readFile(t, name)
	if binary.LittleEndian.Uint16(b[0:]) != 0 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		t.Fatal("ICONDIR 头不对")
	}
	if n := int(binary.LittleEndian.Uint16(b[4:])); n != len(sizes) {
		t.Fatalf("ICO 里有 %d 张,想要 %d", n, len(sizes))
	}
	for i, s := range sizes {
		e := b[6+16*i:]
		if e[0] != dimByte(s) || e[1] != dimByte(s) {
			t.Fatalf("第 %d 项宽高是 %d×%d,想要 %d", i, e[0], e[1], s)
		}
		size, off := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
		label := fmt.Sprintf("icon.ico[%dpx]", s)
		samePixels(t, label, decodePNG(t, label, b[off:off+size]), want(s))
	}
}

func TestCommittedSyso(t *testing.T) {
	res := parseSyso(t, readFile(t, filepath.Join(clientDir, "rsrc_windows_amd64.syso")))

	grp, ok := res[[2]uint16{rtGroupIcon, iconGroupID}]
	if !ok {
		t.Fatalf("没有 RT_GROUP_ICON #%d,go-webview2 的 IconId 会取不到", iconGroupID)
	}
	if n := int(binary.LittleEndian.Uint16(grp[4:])); n != len(sizes) {
		t.Fatalf("图标组里有 %d 张,想要 %d", n, len(sizes))
	}
	for i, s := range sizes {
		e := grp[6+14*i:]
		if e[0] != dimByte(s) {
			t.Fatalf("图标组第 %d 项宽 %d,想要 %d", i, e[0], s)
		}
		id := binary.LittleEndian.Uint16(e[12:])
		img, ok := res[[2]uint16{rtIcon, id}]
		if !ok {
			t.Fatalf("图标组引用了不存在的 RT_ICON #%d", id)
		}
		if int(binary.LittleEndian.Uint32(e[8:])) != len(img) {
			t.Fatalf("RT_ICON #%d 的长度和图标组里记的不一样", id)
		}
		label := fmt.Sprintf("syso RT_ICON #%d", id)
		samePixels(t, label, decodePNG(t, label, img), want(s))
	}

	ver, ok := res[[2]uint16{rtVersion, versionID}]
	if !ok {
		t.Fatal("没有版本信息资源")
	}
	if !bytes.Contains(ver, utf16z("Albion 行情终端")) {
		t.Fatal("版本信息里没有 FileDescription")
	}
}

// 新编出来的 .syso 也过一遍结构检查,不依赖仓库里的文件。
func TestEncodeSysoLayout(t *testing.T) {
	a, err := build()
	if err != nil {
		t.Fatal(err)
	}
	res := parseSyso(t, a.client["rsrc_windows_amd64.syso"])
	if len(res) != len(sizes)+2 {
		t.Fatalf("资源数 %d,想要 %d 张图 + 图标组 + 版本信息", len(res), len(sizes))
	}
}

// parseSyso 按 Windows 加载器的走法解析 .syso 里的资源树,顺带检查
// 链接器要的那些约定。返回 (类型, ID) → 数据。
func parseSyso(t *testing.T, b []byte) map[[2]uint16][]byte {
	t.Helper()
	u16 := func(b []byte, o uint32) uint16 { return binary.LittleEndian.Uint16(b[o:]) }
	u32 := func(b []byte, o uint32) uint32 { return binary.LittleEndian.Uint32(b[o:]) }

	if u16(b, 0) != 0x8664 || u16(b, 2) != 1 || u16(b, 16) != 0 {
		t.Fatal("COFF 头不对:要 amd64、一个节、没有可选头")
	}
	const sec = 20
	if name := string(bytes.TrimRight(b[sec:sec+8], "\x00")); name != ".rsrc" {
		t.Fatalf("节名 %q", name)
	}
	rawLen, rawAt, relAt := u32(b, sec+16), u32(b, sec+20), u32(b, sec+24)
	raw := b[rawAt : rawAt+rawLen]

	relocs := map[uint32]bool{}
	for i := range uint32(u16(b, sec+32)) {
		r := b[relAt+10*i:]
		if u32(r, 4) != 0 || u16(r, 8) != 3 {
			t.Fatal("重定位要指向符号 0、类型 IMAGE_REL_AMD64_ADDR32NB")
		}
		relocs[u32(r, 0)] = true
	}
	symAt := u32(b, 8)
	if u32(b, 12) != 1 || string(bytes.TrimRight(b[symAt:symAt+8], "\x00")) != ".rsrc" ||
		u16(b, symAt+12) != 1 || b[symAt+16] != 3 {
		t.Fatal("符号表要恰好一个指向 .rsrc 节的静态符号")
	}

	dir := func(at uint32) (entries [][2]uint32) {
		if u16(raw, at+12) != 0 {
			t.Fatal("不该有按名字的目录项")
		}
		for i := range uint32(u16(raw, at+14)) {
			e := at + 16 + 8*i
			entries = append(entries, [2]uint32{u32(raw, e), u32(raw, e+4)})
		}
		if !slices.IsSortedFunc(entries, func(a, b [2]uint32) int { return int(a[0]) - int(b[0]) }) {
			t.Fatal("目录项没按 ID 升序,加载器二分查找会找不到")
		}
		return entries
	}
	const subdir = 0x80000000
	out := map[[2]uint16][]byte{}
	for _, te := range dir(0) {
		for _, ie := range dir(te[1] &^ subdir) {
			langs := dir(ie[1] &^ subdir)
			if te[1]&subdir == 0 || ie[1]&subdir == 0 || len(langs) != 1 || langs[0][1]&subdir != 0 {
				t.Fatal("资源树要是 类型 → ID → 语言(叶子)三层")
			}
			entry := langs[0][1]
			if !relocs[entry] {
				t.Fatalf("数据项 0x%x 没有重定位,链接后 OffsetToData 不是 RVA", entry)
			}
			delete(relocs, entry)
			off, size := u32(raw, entry), u32(raw, entry+4)
			out[[2]uint16{uint16(te[0]), uint16(ie[0])}] = raw[off : off+size]
		}
	}
	if len(relocs) != 0 {
		t.Fatalf("有 %d 条重定位不指向任何数据项", len(relocs))
	}
	return out
}

var rendered = map[int]*image.NRGBA{}

func want(size int) *image.NRGBA {
	if rendered[size] == nil {
		rendered[size] = render(size)
	}
	return rendered[size]
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("%v(生成物没提交?在 guild 目录下跑 go generate ./cmd/client)", err)
	}
	return b
}

func decodePNG(t *testing.T, label string, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	return img
}

// samePixels 允许每个通道差 2:浮点运算在别的架构上(比如 arm64 的乘加融合)
// 可能让个别子样本落到边界另一侧。几何改了的话差的是成片的像素,远不止 2。
func samePixels(t *testing.T, label string, got image.Image, want *image.NRGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("%s: 尺寸 %v,想要 %v", label, got.Bounds().Size(), want.Bounds().Size())
	}
	for y := range want.Bounds().Dy() {
		for x := range want.Bounds().Dx() {
			r1, g1, b1, a1 := got.At(x, y).RGBA()
			r2, g2, b2, a2 := want.At(x, y).RGBA()
			for _, d := range [][2]uint32{{r1, r2}, {g1, g2}, {b1, b2}, {a1, a2}} {
				if diff := int(d[0]>>8) - int(d[1]>>8); diff > 2 || diff < -2 {
					t.Fatalf("%s: (%d,%d) 像素对不上,在 guild 目录下跑 go generate ./cmd/client", label, x, y)
				}
			}
		}
	}
}
