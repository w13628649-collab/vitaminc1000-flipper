package protocol

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"albion-guild/internal/model"
)

// spec150 把 gamedata/testdata/event150.json(作者 2026-10-19 登录时的真样本)还原成
// 解码器交上来的参数表:key1/key2 是 []int16,key3 是 []byte
func spec150(t *testing.T) (obj int64, params map[byte]any) {
	t.Helper()
	raw, err := os.ReadFile("../gamedata/testdata/event150.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Params []struct {
			K int             `json:"k"`
			V json.RawMessage `json:"v"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	params = map[byte]any{paramEventCode: int16(150)}
	for _, p := range s.Params {
		switch p.K {
		case 0:
			if err := json.Unmarshal(p.V, &obj); err != nil {
				t.Fatal(err)
			}
			params[0] = obj
		case 1, 2:
			var a []int16
			if err := json.Unmarshal(p.V, &a); err != nil {
				t.Fatal(err)
			}
			params[byte(p.K)] = a
		case 3:
			var h string
			if err := json.Unmarshal(p.V, &h); err != nil {
				t.Fatal(err)
			}
			b, err := hex.DecodeString(h)
			if err != nil {
				t.Fatal(err)
			}
			params[3] = b
		}
	}
	return obj, params
}

func TestSpec_真样本(t *testing.T) {
	obj, ev := spec150(t)
	p := New()
	var got []model.SpecObs
	p.OnSpec = func(s model.SpecObs) { got = append(got, s) }
	// 和真实时序一样:Join 先到(带自己的对象 id),一两毫秒后事件 150
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(2), 0: obj, 1: "guid", 2: "甲", 8: "4002"})
	p.HandleEvent(1, ev)
	if len(got) != 1 {
		t.Fatalf("应当报一次,得到 %d 次", len(got))
	}
	s := got[0]
	if s.Character != "甲" || s.ObservedAt.IsZero() {
		t.Fatalf("角色 / 时间: %+v", s)
	}
	if s.Levels["FIBER"] != [5]int{60, 100, 100, 1, 1} || s.Levels["ORE"] != [5]int{76, 100, 91, 1, 1} {
		t.Fatalf("等级: %v", s.Levels)
	}

	// 切区又推一遍、等级没变:不报
	p.HandleEvent(1, ev)
	if len(got) != 1 {
		t.Fatalf("没变也报了: %d", len(got))
	}
	// 别人的对象 id:不报
	other := map[byte]any{}
	for k, v := range ev {
		other[k] = v
	}
	other[0] = obj + 1
	lv := append([]byte(nil), ev[3].([]byte)...)
	lv[0]++ // 等级变了,确保不是被"没变"挡掉的
	other[3] = lv
	p.HandleEvent(1, other)
	if len(got) != 1 {
		t.Fatalf("别人的也报了: %d", len(got))
	}
	// 换了角色(同一套等级):要报
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(2), 0: obj, 1: "guid2", 2: "乙", 8: "4002"})
	p.HandleEvent(1, ev)
	if len(got) != 2 || got[1].Character != "乙" {
		t.Fatalf("换角色没报: %+v", got)
	}
}

// 下标表对不上(游戏升版挪了下标):不报,也不能报一套错位的等级
func TestSpec_下标对不上不报(t *testing.T) {
	obj, ev := spec150(t)
	p := New()
	var got []model.SpecObs
	p.OnSpec = func(s model.SpecObs) { got = append(got, s) }
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(2), 0: obj, 1: "guid", 2: "甲", 8: "4002"})
	for _, k := range []byte{1, 2} {
		a := append([]int16(nil), ev[k].([]int16)...)
		for i := range a {
			a[i]++
		}
		ev[k] = a
	}
	p.HandleEvent(1, ev)
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(150), 0: obj, 2: []int16{1}}) // 缺等级
	if len(got) != 0 {
		t.Fatalf("对不上还报了: %+v", got)
	}
}
