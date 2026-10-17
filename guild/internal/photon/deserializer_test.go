package photon

import (
	"encoding/binary"
	"math"
	"testing"
)

func f32le(f float32) []byte {
	return binary.LittleEndian.AppendUint32(nil, math.Float32bits(f))
}

// float[][](op 197 的 key18 坐标)按线上字节解:裸数组 0x40 的**每一行各带类型字节**。
// 以前按"类型只写一次"读,第二行的 0x45 被当成长度 69,后面的键全丢
func TestNestedArray_每行各带类型字节(t *testing.T) {
	data := []byte{2} // 参数表 2 项
	// key18 = 0x40, 2 行, [0x45, 2, x, y], [0x45, 2, x, y]
	data = append(data, 18, typeArray, 2)
	data = append(data, typeArray|typeFloat, 2)
	data = append(data, f32le(10.5)...)
	data = append(data, f32le(-3)...)
	data = append(data, typeArray|typeFloat, 2)
	data = append(data, f32le(20)...)
	data = append(data, f32le(40.25)...)
	// key34 = long[]{300, 5}(压缩长整型数组,zigzag varint)
	data = append(data, 34, typeArray|typeCompressedLong, 2, 0xD8, 0x04, 0x0A)

	params := deserializeParameterTable(data)
	rows, ok := params[18].([]interface{})
	if !ok || len(rows) != 2 {
		t.Fatalf("key18 应是 2 行,得到 %#v", params[18])
	}
	for i, want := range [][]float32{{10.5, -3}, {20, 40.25}} {
		row, ok := rows[i].([]float32)
		if !ok || len(row) != 2 || row[0] != want[0] || row[1] != want[1] {
			t.Fatalf("第 %d 行: %#v", i, rows[i])
		}
	}
	fees, ok := params[34].([]int64)
	if !ok || len(fees) != 2 || fees[0] != 300 || fees[1] != 5 {
		t.Fatalf("key18 之后的键不能跟着错位: %#v", params[34])
	}
}

// 长度前缀是线上读来的:错位后可能是几十亿,不能拿去 make
func TestLengthPrefix_放不下的长度当坏数据(t *testing.T) {
	huge := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F} // 2^32-1
	cases := map[string][]byte{
		"float[]": append([]byte{1, 5, typeArray | typeFloat}, huge...),
		"裸数组":     append([]byte{1, 5, typeArray}, huge...),
		"object[]": append([]byte{1, 5, typeObjectArray}, huge...),
		"bool[]":   append([]byte{1, 5, typeArray | typeBoolean}, huge...),
		"字典":       append([]byte{1, 5, typeDictionary, typeByte, typeByte}, huge...),
		"参数表":      append(append([]byte{}, huge...), 5, typeByte, 1),
	}
	for name, data := range cases {
		params := deserializeParameterTable(data) // 不 panic、不 OOM 就算过
		if v := params[5]; v != nil && name != "字典" && name != "参数表" {
			t.Errorf("%s: 放不下的数组应当作没有,得到 %T", name, v)
		}
	}
}
