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

// poly 是凸多边形(锭都是梯形),顶点顺时针逆时针都行。
type poly []pt

// contains 判断点是否在多边形内,边上算在内。
//
// 不用奇偶射线法:它把正好压在左边上的子样本算进来、压在右边上的算出去,
// 斜边吸到整像素之后子样本经常正好压在边上,左右两条斜边的抗锯齿就差几个色阶,
// 图不再左右对称。容差吸收 k 不是 2 的幂时(20/24/36/40px)坐标换算的浮点误差。
func (p poly) contains(x, y float64) bool {
	const eps = 1e-9
	var pos, neg bool
	for i := range p {
		a, b := p[i], p[(i+1)%len(p)]
		switch c := (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X); {
		case c > eps:
			pos = true
		case c < -eps:
			neg = true
		}
	}
	return !(pos && neg)
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

// pixels 是某个输出尺寸的像素网格,负责把设计网格上的坐标和长度吸到整像素。
// k 是每格多少像素;k == 0 表示不吸附(SVG,浏览器按什么尺寸画都行)。
type pixels struct{ k float64 }

func pixelsFor(size int) pixels { return pixels{float64(size) / grid} }

// vector 是不吸附的网格,给 SVG 用。
var vector = pixels{}

// out 把网格坐标吸到最近的像素边界,正好在两条像素线中间时远离中线。
//
// 以中线 x=y=8 为原点取整,左右(上下)对称的边吸完还对称。
// 直接 math.Round(v*k)/k 的话 .5 一律远离 0:16px 下 2.5→3、13.5→14,
// 两条边都往右挪,整个字形偏半像素。所有尺寸都是偶数,中线本身一定是像素边界。
func (p pixels) out(v float64) float64 { return p.snap(v, math.Round) }

// in 同 out,但正好在中间时靠向中线。给"两块东西之间的缝"用:
// 缝宽在 24/40px 下吸完是奇数半像素,往小了取,省下的像素归锭。
func (p pixels) in(v float64) float64 {
	return p.snap(v, func(x float64) float64 { return math.Ceil(x - 0.5) })
}

func (p pixels) snap(v float64, round func(float64) float64) float64 {
	if p.k == 0 {
		return v
	}
	d := v - grid/2
	return grid/2 + math.Copysign(round(math.Abs(d)*p.k)/p.k, d)
}

// span 把一段长度(网格单位)取整到整像素,至少 min 个像素,再换回网格单位。
// 吸附过的两条边各自加减同一个 span,中间那块就还是对称的。
func (p pixels) span(v, min float64) float64 {
	if p.k == 0 {
		return v
	}
	return math.Max(min, math.Round(v*p.k)) / p.k
}

// hairline 是底板描边的宽度(网格单位)。
//
// 16/20px 下是半个像素:一整个像素的边会把底板吃掉一圈,半个像素经过抗锯齿
// 是一道浅一档的灰线,够在深色任务栏上把轮廓勾出来。24px 起至少一整个像素,
// 大尺寸按比例加粗,256px 下 4px——1px 的线在那么大的图上就看不见了。
// SVG 按 16px 的比例给半格。
func (p pixels) hairline() float64 {
	switch size := p.k * grid; {
	case p.k == 0:
		return 0.5
	case size < 24:
		return 0.5 / p.k
	default:
		return math.Max(1, math.Round(size/64)) / p.k
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
	layers := logo(pixelsFor(size))
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
	for _, l := range logo(vector) {
		b.WriteString(l.shape.svg(hex(l.fill)))
		b.WriteByte('\n')
	}
	b.WriteString("</svg>\n")
	return b.String()
}
