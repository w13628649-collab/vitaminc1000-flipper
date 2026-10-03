package main

import "image/color"

// 配色取界面的 token(internal/webui/assets/app.css 的 :root)。
//
//   - 底板 = --ink。浅色界面的纸灰底上、Windows 浅色任务栏上,一块墨色圆角板很醒目;
//     图标没有底板的话,16×16 下就剩几粒彩点
//   - 描边 = --ink-mid。深色任务栏(#202020 / #101010)和墨色底板几乎同色,
//     对比度只有 1.1:1,没有这圈边底板就消失了,只剩三块锭漂在空中,
//     比旁边的系统图标小一圈。--ink-mid 在深色任务栏上是 2:1 左右,勾得出轮廓
//   - 银锭 = --paper。白银是这个游戏的货币,界面底色本来就取的银灰
//   - 铜锭是 --bronze 同色相(38°)再提亮一档。--bronze-lift #a87a2c 在墨色上
//     只有 4.7:1,16×16 下两块铜锭会糊进底板;#c4933d 是 6.5:1
var (
	colInk    = color.NRGBA{0x15, 0x18, 0x1c, 0xff}
	colRim    = color.NRGBA{0x4a, 0x51, 0x58, 0xff}
	colSilver = color.NRGBA{0xdf, 0xe1, 0xe3, 0xff}
	colBronze = color.NRGBA{0xc4, 0x93, 0x3d, 0xff}
)

// logo 是图标唯一的几何定义:墨色圆角底板上码着三块锭,上银下铜。
// 锭是精炼的产物,也是市场上倒来倒去的货;银是钱。
//
// 坐标是 16×16 的网格,一格 = 16px 图标的一个像素。边都落在整数格上,
// 再由 px 吸到输出尺寸的整像素上(16 的倍数天然对齐,20/24/40 要取整)。
// 吸附只动边,不动"每块锭自己左右对称"这件事——见 ingot。
//
// 两块铜锭之间空 2 格而不是 1 格:底板 1..15 的中线 x=8 正好是像素边界,
// 宽 1 格的缝没法居中,左右会差半个像素。缝的内侧边用 in 吸附(见 pixels.in)。
func logo(px pixels) []layer {
	x0, x1 := px.out(1), px.out(15)
	rim := px.hairline()
	return []layer{
		{colRim, rrect{x0, x0, x1, x1, 2.5}},
		{colInk, rrect{x0 + rim, x0 + rim, x1 - rim, x1 - rim, 2.5 - rim}},
		{colBronze, ingot(px, px.out(2), px.in(7), px.out(8), px.out(12))},
		{colBronze, ingot(px, px.in(9), px.out(14), px.out(8), px.out(12))},
		{colSilver, ingot(px, px.out(5), px.out(11), px.out(3), px.out(7))},
	}
}

// ingot 是一块锭:底边 x0..x1(已吸附),顶边每侧内收 0.75 格。纯矩形看着像砖。
// 整个字形纵向占 3..12,比底板中线高半格——上窄下宽的东西,几何居中会显得往下坠。
//
// 内收量按像素取整一次、两侧共用,而不是把顶边两个顶点各自吸附:
// 铜锭的两个顶点在中线同一侧,"远离中线取整"会把它们往同一个方向推,
// 32px 下一侧斜 1px、另一侧斜 2px,两块铜锭往中间倒。
func ingot(px pixels, x0, x1, y0, y1 float64) poly {
	ins := px.span(0.75, 1)
	return poly{
		{x0 + ins, y0},
		{x1 - ins, y0},
		{x1, y1},
		{x0, y1},
	}
}
