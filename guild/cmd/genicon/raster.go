package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// grid 是设计网格的边长,logo 的坐标都在 0..grid 里。
const grid = 16

// layer 是一块纯色图形。后画的盖住先画的。
type layer struct {
	fill  color.NRGBA
	shape shape
}

// shape 同时能被光栅化(contains)和写成 SVG(svg)——位图和矢量图
// 出自同一份几何,不会各画各的。
type shape interface {
	contains(x, y float64) bool
	svg(fill string) string
}

// rrect 是圆角矩形,半开区间 [X0,X1)×[Y0,Y1)。
type rrect struct{ X0, Y0, X1, Y1, R float64 }

func (r rrect) contains(x, y float64) bool {
	if x < r.X0 || x >= r.X1 || y < r.Y0 || y >= r.Y1 {
		return false
	}
	// 到最近的圆角圆心的距离;落在直边区域时这个距离就是 0
	cx := math.Max(r.X0+r.R, math.Min(x, r.X1-r.R))
	cy := math.Max(r.Y0+r.R, math.Min(y, r.Y1-r.R))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r.R*r.R
}

func (r rrect) svg(fill string) string {
	return fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s" rx="%s" fill="%s"/>`,
		num(r.X0), num(r.Y0), num(r.X1-r.X0), num(r.Y1-r.Y0), num(r.R), fill)
}

type pt struct{ X, Y float64 }

// poly 是简单多边形,奇偶规则判内外。
type poly []pt

func (p poly) contains(x, y float64) bool {
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		a, b := p[i], p[j]
		if (a.Y > y) != (b.Y > y) && x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

func (p poly) svg(fill string) string {
	pts := make([]string, len(p))
	for i, v := range p {
		pts[i] = num(v.X) + "," + num(v.Y)
	}
	return fmt.Sprintf(`<polygon points="%s" fill="%s"/>`, strings.Join(pts, " "), fill)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func hex(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// snapFor 返回 size 像素图标用的吸附函数:把网格坐标吸到最近的像素边界。
//
// 以中线 x=y=8 为原点、四舍五入远离中线,左右(上下)对称的边吸完还对称。
// 直接 math.Round(v*k)/k 的话 .5 一律远离 0:16px 下 2.5→3、13.5→14,
// 两条边都往右挪,整个字形偏半像素。所有尺寸都是偶数,中线本身一定是像素边界。
func snapFor(size int) func(float64) float64 {
	k := float64(size) / grid
	return func(v float64) float64 {
		d := v - grid/2
		return grid/2 + math.Copysign(math.Round(math.Abs(d)*k)/k, d)
	}
}

// samples 是每个像素每个方向的采样数。16×16 = 256 个子样本,
// 覆盖率精度 1/256,和 8 位 alpha 同一个量级。
const samples = 16

// render 把 logo 画成 size×size 的位图。
//
// 超采样:每个子样本取最上层覆盖它的颜色,在线性光空间里平均,再转回 sRGB。
// 直接平均 sRGB 值的话,铜锭在墨色底板上的边缘会发黑、看着比实际细。
// alpha 是覆盖率,只有底板圆角外面才不是 255。
func render(size int) *image.NRGBA {
	layers := logo(snapFor(size))
	lin := make([][3]float64, len(layers))
	for i, l := range layers {
		lin[i] = [3]float64{toLinear(l.fill.R), toLinear(l.fill.G), toLinear(l.fill.B)}
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(grid) / float64(size)
	hits := make([]int, len(layers)) // 这个像素里每一层各赢了几个子样本
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			clear(hits)
			covered := 0
			for sy := 0; sy < samples; sy++ {
				y := (float64(py) + (float64(sy)+0.5)/samples) * scale
				for sx := 0; sx < samples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/samples) * scale
					for i := len(layers) - 1; i >= 0; i-- {
						if layers[i].shape.contains(x, y) {
							hits[i]++
							covered++
							break
						}
					}
				}
			}
			if covered == 0 {
				continue // 全透明,NRGBA 零值就是
			}
			var c [3]float64
			for i, n := range hits {
				for k := range c {
					c[k] += lin[i][k] * float64(n)
				}
			}
			n := float64(covered)
			img.SetNRGBA(px, py, color.NRGBA{
				R: toSRGB(c[0] / n), G: toSRGB(c[1] / n), B: toSRGB(c[2] / n),
				A: uint8(math.Round(n / (samples * samples) * 255)),
			})
		}
	}
	return img
}

func toLinear(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func toSRGB(v float64) uint8 {
	v = math.Max(0, math.Min(1, v))
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(math.Round(v * 255))
}

// svgDoc 是矢量版,不吸附——浏览器按什么尺寸画都行。
func svgDoc() string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 %d %d">`+"\n", grid, grid)
	b.WriteString("<!-- 由 guild/cmd/genicon 生成,别手改:改 logo.go 再 go generate ./cmd/client -->\n")
	for _, l := range logo(func(v float64) float64 { return v }) {
		b.WriteString(l.shape.svg(hex(l.fill)))
		b.WriteByte('\n')
	}
	b.WriteString("</svg>\n")
	return b.String()
}
