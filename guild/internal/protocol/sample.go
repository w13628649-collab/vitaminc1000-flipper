package protocol

import "albion-guild/internal/model"

// 原始样本记录器:**每一条认得出码的明文消息**(请求、响应、事件)都原样记一份全键样本,
// 之后要用哪条就照着样本写解码器。这是 2026-10-19 起的抓包方针(明文全抓全解,
// 见仓库 CLAUDE.md)。样本在白名单过滤之前记:处理器只拿自己声明的键,样本拿全部。
//
// 每个参数都带 Go 类型标签({"k":17,"t":"[]int32","v":[…]}),因为同一个字段线上
// 可能是几种 Go 类型,解码器要照着类型写。

// MaxSamplesPerCode 是每个 (方向, 码) 每次客户端运行最多记几条。样本是给人看布局的,
// 几条就够;不封顶的话 Move 这种一秒十几条的事件会一直往上传
const MaxSamplesPerCode = 20

// MaxCode 是认的码的上限。线上码是 int16,客户端码表目前排到操作 555、事件 711。
// 服务端收上传时也按它卡(/api/upload 不鉴权,换着码传会让表无限增长),
// capture_sample 表因此有上限:3 个方向 × (MaxCode+1) 个码 × store.SamplesPerCode 条
const MaxCode = 2047

func (p *Parser) recordSample(kind Kind, code int, params map[byte]any) {
	if p.OnSample == nil || code < 0 || code > MaxCode {
		return
	}
	k := ruleKey{kind, code}
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
		Code:          code,
		Kind:          kind.String(),
		Params:        taggedParams(params),
		ObservedAt:    p.now(),
		ClientVersion: p.Version,
	})
}
