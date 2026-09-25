package scan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"albion-guild/internal/arb"
	"albion-guild/internal/screen"
)

// Digest 是一份扫描结果对外内容(同城机会 + 跨城路线 + 拒绝统计)的摘要。
//
// 给 WS 的 scan 通知用:界面拿它和手上那份比,不同才重拉 /api/scan。
// 抓包重算一分钟一次,大多数时候内容其实没变,每次都整份重拉是白费。
//
// **数据龄不算内容。** 同一份快照隔一分钟再评估一次,所有 *_age_hours 都会
// 跟着 now 变大,不剔掉的话摘要每次都变,等于没有摘要。所以对 JSON 里
// 键名以 age_hours 结尾的字段一律剔除——新加的数据龄字段只要照这个命名,
// 就自动不进摘要。数据龄跨过阈值引起的真实变化(置信度降级、stale 拒绝、
// 深度档滑出可信窗口)会反映在别的字段上,照样改变摘要。界面要显示实时的
// 数据龄,拿 evaluated_at 加上本地流逝的时间自己算。
//
// 文本里也不能嵌随时间走的数(比如"深度快照 1.3h 前"),否则同样每隔几分钟变一次。
func Digest(res *Result) string {
	if res == nil {
		return ""
	}
	opps, routes, counts := res.Opportunities, res.Routes, res.RejectCounts
	// nil 和空是同一个内容
	if opps == nil {
		opps = []screen.Opportunity{}
	}
	if routes == nil {
		routes = []arb.Route{}
	}
	if counts == nil {
		counts = map[string]int{}
	}
	raw, err := json.Marshal(struct {
		Opportunities []screen.Opportunity `json:"opportunities"`
		Routes        []arb.Route          `json:"routes"`
		RejectCounts  map[string]int       `json:"reject_counts"`
	}{opps, routes, counts})
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
	// 重新编码时 map 按键排序,和结构体字段顺序无关,结果是确定的
	canon, err := json.Marshal(stripAges(v))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:16])
}

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
