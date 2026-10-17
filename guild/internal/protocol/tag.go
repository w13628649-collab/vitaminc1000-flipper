package protocol

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
)

// 带类型标签的原样序列化。用在两处:op 197 里含义未知的键(23/31)原样存,
// 和原始样本记录器。
//
// **类型标签是要点**:Go 类型决定了解码器怎么写——同一个 long[] 线上可能是
// []int64 也可能是 []interface{},只存值的话事后看不出来,解码器照着写就会漏形态。

// Tagged 是一个值和它的 Go 类型。
type Tagged struct {
	T string `json:"t"`
	V any    `json:"v"`
}

// TaggedParam 是参数表里的一项。
type TaggedParam struct {
	K int    `json:"k"`
	T string `json:"t"`
	V any    `json:"v"`
}

// tag 给一个值打上类型标签,值转成 JSON 能装的形态。
func tag(v any) Tagged {
	if v == nil {
		return Tagged{T: "nil"}
	}
	return Tagged{T: fmt.Sprintf("%T", v), V: jsonSafe(v)}
}

// tagJSON 是 tag 之后序列化。序列化失败(理论上 jsonSafe 已经兜住了)退回 %v 字符串。
func tagJSON(v any) json.RawMessage {
	raw, err := json.Marshal(tag(v))
	if err != nil {
		raw, _ = json.Marshal(Tagged{T: fmt.Sprintf("%T", v), V: fmt.Sprintf("%v", v)})
	}
	return raw
}

// taggedParams 把整张参数表按键排序、逐项打标签后序列化。
func taggedParams(params map[byte]any) json.RawMessage {
	keys := make([]int, 0, len(params))
	for k := range params {
		keys = append(keys, int(k))
	}
	slices.Sort(keys)
	out := make([]TaggedParam, 0, len(keys))
	for _, k := range keys {
		t := tag(params[byte(k)])
		out = append(out, TaggedParam{K: k, T: t.T, V: t.V})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		// 不该走到这。真走到了也要留下点东西,别整条样本丢掉
		raw, _ = json.Marshal([]TaggedParam{{K: -1, T: "error", V: err.Error()}})
	}
	return raw
}

// jsonSafe 把 Photon 解出来的值转成 encoding/json 一定能序列化的形态:
//   - []byte → 十六进制串(自定义类型的载荷就是它,看字节比 base64 直观)
//   - NaN / ±Inf → 字符串(json 不收)
//   - []interface{} → 每个元素各带类型标签(元素类型可能各不相同)
//   - map[interface{}]interface{}(Photon 的 Dictionary / Hashtable)→ [{k, v}] 列表,键值都带标签
//   - map[string]interface{}(自定义类型 / 嵌套操作)→ 值带标签
func jsonSafe(v any) any {
	switch x := v.(type) {
	case nil, bool, string,
		int8, int16, int32, int64, int, uint16, uint32, uint64,
		[]bool, []string, []int16, []int32, []int64:
		return x
	case byte:
		return x
	case float32:
		return safeFloat(float64(x))
	case float64:
		return safeFloat(x)
	case []byte:
		return hex.EncodeToString(x)
	case []float32:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = safeFloat(float64(f))
		}
		return out
	case []float64:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = safeFloat(f)
		}
		return out
	case []any:
		out := make([]Tagged, len(x))
		for i, e := range x {
			out[i] = tag(e)
		}
		return out
	case map[any]any:
		type kv struct {
			K Tagged `json:"k"`
			V Tagged `json:"v"`
		}
		out := make([]kv, 0, len(x))
		for k, val := range x {
			out = append(out, kv{tag(k), tag(val)})
		}
		slices.SortFunc(out, func(a, b kv) int {
			return cmp.Compare(fmt.Sprint(a.K.V), fmt.Sprint(b.K.V))
		})
		return out
	case map[string]any:
		out := make(map[string]Tagged, len(x))
		for k, val := range x {
			out[k] = tag(val)
		}
		return out
	case map[byte]any:
		return json.RawMessage(taggedParams(x))
	}
	// 其他切片(嵌套的 [][]float32 之类)逐个元素打标签
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]Tagged, rv.Len())
		for i := range out {
			out[i] = tag(rv.Index(i).Interface())
		}
		return out
	}
	return fmt.Sprintf("%v", v)
}

func safeFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprint(f)
	}
	return f
}
