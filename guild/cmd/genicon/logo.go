package main

import "image/color"

// 配色取界面的 token(internal/webui/assets/app.css 的 :root)。
//
//   - 底板 = --ink。浅色界面的纸灰底上、Windows 浅色和深色任务栏上,
//     一块墨色圆角板都站得住;图标没有底板的话,16×16 下就剩几粒彩点
//   - 银锭 = --paper。白银是这个游戏的货币,界面底色本来就取的银灰
//   - 铜锭是 --bronze 同色相(38°)再提亮一档。--bronze-lift #a87a2c 在墨色上
//     只有 4.7:1,16×16 下两块铜锭会糊进底板;#c4933d 是 6.5:1
var (
	colInk    = color.NRGBA{0x15, 0x18, 0x1c, 0xff}
	colSilver = color.NRGBA{0xdf, 0xe1, 0xe3, 0xff}
	colBronze = color.NRGBA{0xc4, 0x93, 0x3d, 0xff}
)

// logo 是图标唯一的几何定义:墨色圆角底板上码着三块锭,上银下铜。
// 锭是精炼的产物,也是市场上倒来倒去的货;银是钱。
//
// 坐标是 16×16 的网格,一格 = 16px 图标的一个像素。所有直边都落在整数格上,
// 所以 16/32/48/64/128/256 这些 16 的倍数天然对齐像素,16×16 下每块锭都是
// 实打实的整像素块。20/24/40 不是 16 的倍数,靠 snap 把顶点吸到最近的像素线上;
// 锭的梯形斜边吸不直,交给抗锯齿。
//
// 两块铜锭之间空 2 格而不是 1 格:底板 1..15 的中线 x=8 正好是像素边界,
// 宽 1 格的缝没法居中,左右会差半个像素。
func logo(snap func(float64) float64) []layer {
	return []layer{
		{colInk, rrect{snap(1), snap(1), snap(15), snap(15), 2.5}},
		{colBronze, ingot(snap, 2, 7, 8, 12)},
		{colBronze, ingot(snap, 9, 14, 8, 12)},
		{colSilver, ingot(snap, 5, 11, 3, 7)},
	}
}

// ingot 是一块锭:底边 x0..x1,顶边每侧内收 0.75 格。纯矩形看着像砖。
// 整个字形纵向占 3..12,比底板中线高半格——上窄下宽的东西,几何居中会显得往下坠。
func ingot(snap func(float64) float64, x0, x1, y0, y1 float64) poly {
	const inset = 0.75
	return poly{
		{snap(x0 + inset), snap(y0)},
		{snap(x1 - inset), snap(y0)},
		{snap(x1), snap(y1)},
		{snap(x0), snap(y1)},
	}
}
