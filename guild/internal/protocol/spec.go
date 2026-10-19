package protocol

import (
	"log/slog"
	"maps"

	"albion-guild/internal/gamedata"
	"albion-guild/internal/model"
)

// handleSpec:事件 FullAchievementInfo(150),命运之轮上全部节点的等级。
//
//	key0 对象 id   key1 满级节点的下标(short[])   key2 节点下标(short[])   key3 对应等级(byte[])
//
// 布局和 AFMDataClientCore 的 FullAchievementInfoEvent 一致,2026-10-19 拿真样本核过
// (docs/client-data.md 第二点十一节)。下标是 achievements.xml 里的位置,随客户端版本漂移,
// 所以先拿内置表(internal/gamedata)核一遍指纹,对不上就不用——宁可让界面回到手填,
// 也不报一套错位的等级。
//
// 登录和每次切区都推一遍(Join 响应之后一两毫秒),这里只在等级变了、或换了角色时报
func (p *Parser) handleSpec(params map[byte]any) {
	maxed, _ := intSlice(params[1]) // 一个满级节点都没有时可能不带
	idx, okIdx := intSlice(params[2])
	levels, okLv := params[3].([]byte)
	if !okIdx || !okLv {
		slog.Debug("专精事件缺节点或等级", "key2", typeName(params[2]), "key3", typeName(params[3]))
		return
	}
	if err := gamedata.CheckAchievements(maxed, idx, levels); err != nil {
		p.mu.Lock()
		first := !p.ledger.specWarned
		p.ledger.specWarned = true
		p.mu.Unlock()
		if first {
			slog.Warn("专精事件和内置下标表对不上(游戏更新了?),专精先不自动填;"+
				"重跑 go generate ./internal/gamedata 换新表", "err", err, "table_client", gamedata.ClientBuild)
		}
		return
	}
	lv := gamedata.SpecLevels(maxed, idx, levels)
	p.mu.Lock()
	if !p.isSelfLocked(params[0]) ||
		(p.ledger.lastSpecChar == p.characterName && maps.Equal(lv, p.ledger.lastSpec)) {
		p.mu.Unlock()
		return
	}
	p.ledger.lastSpec, p.ledger.lastSpecChar = lv, p.characterName
	obs := model.SpecObs{Character: p.characterName, Levels: lv}
	p.mu.Unlock()
	obs.ObservedAt = p.now()
	if p.OnSpec != nil {
		p.OnSpec(obs)
	}
}

// intSlice 取整数数组(short[] / int[] / long[] 都认)
func intSlice(v any) ([]int, bool) {
	a, ok := int64Slice(v)
	if !ok {
		return nil, false
	}
	out := make([]int, len(a))
	for i, n := range a {
		out[i] = int(n)
	}
	return out, true
}

func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return tag(v).T
}
