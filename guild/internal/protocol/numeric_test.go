package protocol

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
)

// Photon 解出来的 long[] 可能是这些形态里的任何一种,全都要认
func TestInt64s_各种形态(t *testing.T) {
	want := []int64{1, 300, 70000}
	cases := map[string]any{
		"[]int64":         []int64{1, 300, 70000},
		"[]int32":         []int32{1, 300, 70000},
		"[]int":           []int{1, 300, 70000},
		"[]any 混合宽度":     []any{byte(1), int16(300), int32(70000)},
		"[]any int64":     []any{int64(1), int64(300), int64(70000)},
		"[]float64 整数值":   []float64{1, 300, 70000},
		"[]any 里的整数浮点": []any{float32(1), int64(300), float64(70000)},
	}
	for name, v := range cases {
		got, ok := int64s(v)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("%s: 得到 %v, %v", name, got, ok)
		}
	}
	if got, ok := int64s([]int16{-2, 5}); !ok || !slices.Equal(got, []int64{-2, 5}) {
		t.Errorf("[]int16: %v", got)
	}
	if got, ok := int64s([]byte{7, 255}); !ok || !slices.Equal(got, []int64{7, 255}) {
		t.Errorf("[]byte: %v", got)
	}
	// 标量当成长度 1 的数组(只有一个建筑时发送方可能不包数组)
	if got, ok := int64s(int64(42)); !ok || !slices.Equal(got, []int64{42}) {
		t.Errorf("标量: %v", got)
	}
	// long 不能被截:超过 int32 的银币原样保留
	if got, _ := int64s([]any{int64(1) << 40}); got[0] != 1<<40 {
		t.Errorf("大数被截了: %v", got)
	}
}

// 混进一个认不出的元素整组作废,不跳过——跳过会让后面的元素错位
func TestInt64s_认不出就整组作废(t *testing.T) {
	for name, v := range map[string]any{
		"nil":          nil,
		"字符串":          "12",
		"[]any 混了字符串":  []any{int64(1), "x", int64(3)},
		"[]any 混了 nil": []any{int64(1), nil},
		"非整数浮点":        []float32{1.5},
		"NaN":          []float64{math.NaN()},
		"字符串数组":        []string{"1"},
	} {
		if got, ok := int64s(v); ok {
			t.Errorf("%s 不该通过,得到 %v", name, got)
		}
	}
}

func TestFloatMatrix_各种形态(t *testing.T) {
	want := [][]float64{{1.5, 2}, {3, -4.25}}
	cases := map[string]any{
		"[][]float32":    [][]float32{{1.5, 2}, {3, -4.25}},
		"[]any 套 []float32": []any{[]float32{1.5, 2}, []float32{3, -4.25}},
		"[]any 套 []any":     []any{[]any{float32(1.5), int32(2)}, []any{float64(3), float32(-4.25)}},
		"[][]float64":        [][]float64{{1.5, 2}, {3, -4.25}},
	}
	for name, v := range cases {
		got, ok := floatMatrix(v)
		if !ok || len(got) != 2 || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
			t.Errorf("%s: 得到 %v, %v", name, got, ok)
		}
	}
	if _, ok := floatMatrix([]any{[]float32{1}, "x"}); ok {
		t.Error("混了字符串的不该通过")
	}
	if _, ok := floatMatrix([]float32{1, 2}); ok {
		t.Error("一维数组不是矩阵")
	}
}

// 所有者要按位置对齐:nil 记空串,混了非字符串整组作废
func TestStrs_按位置对齐(t *testing.T) {
	got, ok := strs([]any{"甲", nil, "丙"})
	if !ok || !slices.Equal(got, []string{"甲", "", "丙"}) {
		t.Fatalf("得到 %v", got)
	}
	if _, ok := strs([]any{"甲", int32(1)}); ok {
		t.Fatal("混了数字的不该通过")
	}
}

// 类型标签要带上 Go 类型,值要能序列化(NaN、[]byte、字典这些 json 原生不收)
func TestTag_带类型且一定能序列化(t *testing.T) {
	params := map[byte]any{
		17:  []int32{1, 2},
		3:   []byte{0xAB, 0x01},
		5:   float32(math.NaN()),
		9:   []any{int64(1), "x", nil},
		20:  map[any]any{"b": int16(2), "a": []float32{1}},
		21:  map[string]any{"type": byte(3), "data": []byte{1}},
		22:  [][]float32{{1, 2}},
		252: int16(153),
	}
	raw := taggedParams(params)
	var got []TaggedParam
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("序列化出来的不是合法 JSON: %v\n%s", err, raw)
	}
	if len(got) != len(params) || got[0].K != 3 || got[len(got)-1].K != 252 {
		t.Fatalf("要按键排序、一个不少: %s", raw)
	}
	s := string(raw)
	for _, want := range []string{
		`{"k":17,"t":"[]int32","v":[1,2]}`,
		`{"k":3,"t":"[]uint8","v":"ab01"}`,
		`{"k":5,"t":"float32","v":"NaN"}`,
		`{"k":252,"t":"int16","v":153}`,
		`"t":"[][]float32"`,
		`{"t":"nil","v":null}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("缺 %s\n全文 %s", want, s)
		}
	}
}
