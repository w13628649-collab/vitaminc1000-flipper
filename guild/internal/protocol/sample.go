package protocol

import "albion-guild/internal/model"

// 原始样本记录器:把还没解过字段布局的消息原样记下来,之后照着写解码器。
//
// 只记白名单里 Sample 的那几条(allowlist.go):op 197 响应的全部键,
// 和用户 2026-10-17 批准的三条专精事件(150/152/153)。每个参数都带 Go 类型标签
// ({"k":17,"t":"[]int32","v":[…]}),因为同一个字段线上可能是几种 Go 类型,
// 解码器要照着类型写。

// MaxSamplesPerCode 是每个码每次客户端运行最多记几条。样本是给人看布局的,
// 几条就够;不封顶的话开着地图来回切区会一直往上传
const MaxSamplesPerCode = 20

func (p *Parser) recordSample(rule Rule, params map[byte]any) {
	if p.OnSample == nil {
		return
	}
	k := ruleKey{rule.Kind, rule.Code}
	p.mu.Lock()
	if p.sampled == nil {
		p.sampled = map[ruleKey]int{}
	}
	if p.sampled[k] >= MaxSamplesPerCode {
		p.mu.Unlock()
		return
	}
	p.sampled[k]++
	p.mu.Unlock()

	p.OnSample(model.CaptureSample{
		Code:          rule.Code,
		Kind:          rule.Kind.String(),
		Params:        taggedParams(params),
		ObservedAt:    p.now(),
		ClientVersion: p.Version,
	})
}
