package protocol

import "math"

// Photon 反序列化出来的数值类型不固定:同一个 long[] 可能是 []int64(压缩长整型数组)、
// []interface{}(元素各带类型码,里面再是 int64 / int32 / byte……),标量也可能
// 被收窄成 byte 或 int16(看数值大小)。下面几个转换器把这些形态统一起来。
//
// 规矩:**整组要么全转成功,要么报失败**。混进一个认不出的元素就整组作废,
// 不跳过——跳过会让后面的元素整体错位,费率挂到别的建筑上。

// int64Of 把一个整数标量转成 int64。整数值的浮点也收(有的发送方把计数写成 float)。
func int64Of(v any) (int64, bool) {
	switch x := v.(type) {
	case byte:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case uint16:
		return int64(x), true
	case int32:
		return int64(x), true
	case uint32:
		return int64(x), true
	case int64:
		return x, true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case int:
		return int64(x), true
	case float32:
		return floatToInt(float64(x))
	case float64:
		return floatToInt(x)
	}
	return 0, false
}

func floatToInt(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) ||
		f > math.MaxInt64 || f < math.MinInt64 {
		return 0, false
	}
	return int64(f), true
}

// float64Of 把一个数值标量转成 float64。
func float64Of(v any) (float64, bool) {
	switch x := v.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	if n, ok := int64Of(v); ok {
		return float64(n), true
	}
	return 0, false
}

// int64s 把一个整数数组(各种形态)转成 []int64。标量当成长度 1 的数组。
func int64s(v any) ([]int64, bool) {
	switch a := v.(type) {
	case nil:
		return nil, false
	case []int64:
		return append([]int64(nil), a...), true
	case []int32:
		return convertInts(a), true
	case []int16:
		return convertInts(a), true
	case []byte:
		return convertInts(a), true
	case []int:
		return convertInts(a), true
	case []float32:
		return mapAll(a, func(f float32) (int64, bool) { return floatToInt(float64(f)) })
	case []float64:
		return mapAll(a, floatToInt)
	case []any:
		return mapAll(a, int64Of)
	}
	if n, ok := int64Of(v); ok {
		return []int64{n}, true
	}
	return nil, false
}

// float64s 把一个数值数组转成 []float64。标量当成长度 1 的数组。
func float64s(v any) ([]float64, bool) {
	switch a := v.(type) {
	case nil:
		return nil, false
	case []float32:
		out := make([]float64, len(a))
		for i, f := range a {
			out[i] = float64(f)
		}
		return out, true
	case []float64:
		return append([]float64(nil), a...), true
	case []any:
		return mapAll(a, float64Of)
	}
	if ns, ok := int64s(v); ok {
		out := make([]float64, len(ns))
		for i, n := range ns {
			out[i] = float64(n)
		}
		return out, true
	}
	return nil, false
}

// floatMatrix 把 float[][] 转成 [][]float64。外层可能是 [][]float32,
// 也可能是 []interface{} 里装着各行([]float32 / []interface{} ……)。
func floatMatrix(v any) ([][]float64, bool) {
	switch a := v.(type) {
	case [][]float32:
		return mapAll(a, func(row []float32) ([]float64, bool) { return float64s(row) })
	case [][]float64:
		return mapAll(a, func(row []float64) ([]float64, bool) { return float64s(row) })
	case []any:
		return mapAll(a, float64s)
	}
	return nil, false
}

// strs 把字符串数组转成 []string,**按位置对齐**:nil 元素记成空串,
// 非字符串元素整组作废(stringSlice 会跳过它们,那样会错位)。
func strs(v any) ([]string, bool) {
	switch a := v.(type) {
	case []string:
		return append([]string(nil), a...), true
	case []any:
		return mapAll(a, func(e any) (string, bool) {
			if e == nil {
				return "", true
			}
			s, ok := e.(string)
			return s, ok
		})
	}
	return nil, false
}

type integer interface {
	~byte | ~int16 | ~int32 | ~int
}

func convertInts[T integer](a []T) []int64 {
	out := make([]int64, len(a))
	for i, x := range a {
		out[i] = int64(x)
	}
	return out
}

func mapAll[T, U any](a []T, f func(T) (U, bool)) ([]U, bool) {
	out := make([]U, len(a))
	for i, x := range a {
		y, ok := f(x)
		if !ok {
			return nil, false
		}
		out[i] = y
	}
	return out, true
}
