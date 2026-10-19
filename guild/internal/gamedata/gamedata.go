// Package gamedata 是从游戏客户端静态数据蒸馏出来的下标表:专精节点在 achievements.xml
// 里排第几、哪种建筑类型是哪条线的精炼站。
//
// 这些都是"在 xml 里排第几",客户端升版就可能整体漂移,所以 tables_gen.go 由
// cmd/gengamedata 生成,不手写。客户端解专精事件、服务端按建筑类型挑站费都用这一份。
package gamedata

import (
	"fmt"
	"slices"
)

//go:generate go run ../../cmd/gengamedata -root ../../../rea-test/investigations/albion-online/static-data -out tables_gen.go

// Lines 是有专精节点和精炼站的线。前四条和 refine.Lines 的 Key 一致,石头线精炼页不做,
// 只记录(Bridgewatch 的数据也要攒着)
var Lines = []string{"ORE", "HIDE", "FIBER", "WOOD", "ROCK"}

// Tiers 是专精节点的 tier,SpecIndex 的五格依次对应它们
var Tiers = [5]int{4, 5, 6, 7, 8}

// MaxSpecLevel 是精炼专精节点的满级。事件 150 把满级的节点单独放在 key1,不给等级
const MaxSpecLevel = 100

// SpecIndex 是这条线 T4~T8 精炼专精节点的成就下标。
func SpecIndex(line string) ([5]int, bool) {
	s, ok := specIndex[line]
	return s, ok
}

// StationLine 是建筑类型 id(op 197 的 key17)对应的精炼线;不是精炼站返回 false。
func StationLine(buildingType int32) (string, bool) {
	l, ok := stationLines[buildingType]
	return l, ok
}

// StationTypes 是这条线全部精炼站的建筑类型 id(T2~T8 各一种),升序。
func StationTypes(line string) []int32 {
	var out []int32
	for id, l := range stationLines {
		if l == line {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// SpecLevels 从事件 150 的三组数里取出各线 T4~T8 精炼专精节点的等级。调用前先 CheckAchievements。
//
// 等级是这个节点已经完成的行数:第 0 行是解锁(T5 节点要 T2~T8 布料累计 45,000 名望,
// 完成后才能精炼 T5),之后每行一级,共 100 行。所以
//   - 0:还没解锁,这一级炼不了
//   - 1~99:解锁了,FCE 按这个数算
//   - 满级不在 idx 里,在 maxed 里,记 MaxSpecLevel
//
// 2026-10-19 拿真样本核过:矿石 T6 报 84 级时事件 152 给的本级名望 53,730 / 进度 0.40,
// 正好是模板第 84 行要的 134,340
func SpecLevels(maxed, idx []int, levels []byte) map[string][5]int {
	lv := make(map[int]int, len(idx)+len(maxed))
	for k, i := range idx {
		if k < len(levels) {
			lv[i] = int(levels[k])
		}
	}
	for _, i := range maxed {
		lv[i] = MaxSpecLevel
	}
	out := make(map[string][5]int, len(Lines))
	for _, l := range Lines {
		var s [5]int
		for t, i := range specIndex[l] {
			s[t] = lv[i]
		}
		out[l] = s
	}
	return out
}

// CheckAchievements 拿内置的下标表核对一次事件 150,对不上返回原因。
//
// 事件里只有下标,没有成就名,游戏升版插进一条成就,下标就整体错位,读出来的是别的节点的
// 等级——不报错,数却全是错的。这里用表里每个下标的类型当指纹:
//   - 下标不能超出表
//   - 普通成就只有完成 / 没完成两态,完成的进 key1,所以在 key2 里等级只能是 0
//   - 模板成就的等级不能到它的行数(到了就是满级,该在 key1 里)
//
// 错位一格,普通成就和模板成就交错的地方就会撞上这几条
func CheckAchievements(maxed, idx []int, levels []byte) error {
	if len(idx) != len(levels) {
		return fmt.Errorf("节点 %d 个、等级 %d 个,对不上", len(idx), len(levels))
	}
	n := len(achievementRows)
	for _, i := range maxed {
		if i < 0 || i >= n {
			return fmt.Errorf("满级节点下标 %d 超出内置表(%d 个,客户端 %s)", i, n, ClientBuild)
		}
	}
	for k, i := range idx {
		if i < 0 || i >= n {
			return fmt.Errorf("节点下标 %d 超出内置表(%d 个,客户端 %s)", i, n, ClientBuild)
		}
		rows, lv := achievementRows[i], levels[k]
		if rows == 0 && lv != 0 {
			return fmt.Errorf("下标 %d 在内置表里是普通成就,却报了等级 %d", i, lv)
		}
		if rows != 0 && lv >= rows {
			return fmt.Errorf("下标 %d 的模板只有 %d 级,却报了等级 %d", i, rows, lv)
		}
	}
	return nil
}
