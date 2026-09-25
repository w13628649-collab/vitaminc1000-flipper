package scan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// Digest 是一份扫描结果对外内容的摘要:/api/scan 输出的**全部**字段,
// 只去掉随时间流逝自己会变、却不代表内容变了的那几样(见下)。
//
// 给 WS 的 scan 通知用:界面拿它和手上那份比,不同才重拉 /api/scan、整页重画。
// 抓包重算一分钟一次,大多数时候内容其实没变,每次都整份重拉是白费。
//
// **范围是整份结果,不是挑几块。** 以前只算机会、路线、拒绝统计,界面各页从同一份
// /api/scan 画出来的还有覆盖率里抓包那几列、「抓包用了 N 个价」、被拒明细的来源、
// capture.error 横幅、页脚的物品数和请求数——这些单独变了摘要不变,界面按协议
// 不重拉,页面就一直停在旧值。所以改成默认全算、按名单剔:以后 Result 新加字段
// 自动进摘要,漏剔的后果是界面多拉几次,漏算的后果是界面不更新,前者便宜得多。
//
// 剔掉的只有这几样,都是"同一份内容、换个 now 再评估就会变"的:
//   - evaluated_at:每次评估都变,它本身就在通知里单独发
//   - 键名以 age_hours 结尾的字段(任意层级):数据龄跟着 now 变大。新加的数据龄字段
//     只要照这个命名就自动剔除。界面要显示实时数据龄,拿 evaluated_at 加上本地流逝的
//     时间自己算
//   - coverage[] 里 within_ 开头的新鲜度分桶:AODP 时间戳只在全量或补拉时才换,
//     两次之间这几个数只是随时间往下掉。全量和补拉会改 started_at / request_count /
//     price_rows,摘要照样变,界面照样重拉
//   - rejected[] 里 stale / stale_history / future_timestamp 这几种的 detail:
//     文案嵌着"7.2h > 6h""最后成交距今 22.6d",每几分钟就变一次。原因本身、
//     物品城市品质和两边来源照样算
//
// 数据龄跨过阈值引起的真实变化(置信度降级、stale 拒绝、深度档滑出可信窗口、
// 挂单滑出抓包窗口)会反映在别的字段上,照样改变摘要。
// 文本里也不能嵌随时间走的数(比如"深度快照 1.3h 前"),否则同样每隔几分钟变一次。
func Digest(res *Result) string {
	if res == nil {
		return ""
	}
	out := *res
	out.Digest = ""
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	// UseNumber:数字按原样的十进制文本重新写出,不经 float64 往返
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return ""
	}
	top, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	delete(top, "evaluated_at")
	delete(top, "digest")
	if cov, ok := top["coverage"].([]any); ok {
		for _, c := range cov {
			if m, ok := c.(map[string]any); ok {
				for k := range m {
					if strings.HasPrefix(k, "within_") {
						delete(m, k)
					}
				}
			}
		}
	}
	if rej, ok := top["rejected"].([]any); ok {
		for _, r := range rej {
			if m, ok := r.(map[string]any); ok {
				if reason, _ := m["reason"].(string); agedDetail[reason] {
					delete(m, "detail")
				}
			}
		}
	}
	// 重新编码时 map 按键排序,和结构体字段顺序无关,结果是确定的
	canon, err := json.Marshal(prune(stripAges(top)))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:16])
}

// agedDetail 是 detail 文案里嵌着数据龄的拒绝原因(见 screen.EvaluateSides)。
// 新加的拒绝原因要是也把数据龄写进 detail,得加到这里,
// 否则 TestDigest_只有时间流逝时不变 在测试数据覆盖到时会报出来
var agedDetail = map[string]bool{"stale": true, "stale_history": true, "future_timestamp": true}

// stripAges 递归剔除键名以 age_hours 结尾的字段。
func stripAges(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if strings.HasSuffix(k, "age_hours") {
				delete(x, k)
				continue
			}
			x[k] = stripAges(val)
		}
	case []any:
		for i := range x {
			x[i] = stripAges(x[i])
		}
	}
	return v
}

// prune 递归删掉值为 null、空数组、空对象的键:nil 切片和空切片、缺省和空 map
// 说的是同一个内容(没有),不该因为上游用了哪种写法摘要就不同。
// 数组里的元素不删,只往里递归——删了会改变其余元素的位置。
func prune(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			val = prune(val)
			if empty(val) {
				delete(x, k)
				continue
			}
			x[k] = val
		}
	case []any:
		for i := range x {
			x[i] = prune(x[i])
		}
	}
	return v
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
