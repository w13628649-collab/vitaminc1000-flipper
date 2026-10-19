package gamedata

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// event150 读 testdata/event150.json:作者 2026-10-19 登录时抓到的真样本(只留 key0~3)。
func event150(t *testing.T) (maxed, idx []int, levels []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/event150.json")
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
	for _, p := range s.Params {
		var err error
		switch p.K {
		case 1:
			err = json.Unmarshal(p.V, &maxed)
		case 2:
			err = json.Unmarshal(p.V, &idx)
		case 3:
			var h string
			if err = json.Unmarshal(p.V, &h); err == nil {
				levels, err = hex.DecodeString(h)
			}
		}
		if err != nil {
			t.Fatalf("key%d: %v", p.K, err)
		}
	}
	return maxed, idx, levels
}

func TestRealSample(t *testing.T) {
	maxed, idx, levels := event150(t)
	if err := CheckAchievements(maxed, idx, levels); err != nil {
		t.Fatalf("真样本没通过核对: %v", err)
	}
	got := SpecLevels(maxed, idx, levels)
	// 和游戏里对过的:纤维 T5/T6、矿石 T5 满级;各线 T7/T8 的 1 是"刚解锁、还没升级"
	want := map[string][5]int{
		"FIBER": {60, 100, 100, 1, 1},
		"ORE":   {76, 100, 91, 1, 1},
		"HIDE":  {15, 27, 2, 0, 0},
		"WOOD":  {21, 1, 0, 0, 0},
		"ROCK":  {0, 0, 0, 0, 0},
	}
	for l, w := range want {
		if got[l] != w {
			t.Errorf("%s = %v, want %v", l, got[l], w)
		}
	}
}

// 游戏升版插进一条成就,下标整体挪一格:指纹要能认出来,不能读出一套错位的等级
func TestShiftedIndicesRejected(t *testing.T) {
	maxed, idx, levels := event150(t)
	for _, d := range []int{-1, 1, 2} {
		m2 := make([]int, len(maxed))
		for i, v := range maxed {
			m2[i] = v + d
		}
		i2 := make([]int, len(idx))
		for i, v := range idx {
			i2[i] = v + d
		}
		if err := CheckAchievements(m2, i2, levels); err == nil {
			t.Errorf("下标挪 %+d 格没被认出来", d)
		}
	}
	if err := CheckAchievements(maxed, idx, levels[:len(levels)-1]); err == nil || !strings.Contains(err.Error(), "对不上") {
		t.Errorf("等级少一个: %v", err)
	}
	if err := CheckAchievements([]int{len(achievementRows)}, nil, nil); err == nil {
		t.Error("超出表的下标没被认出来")
	}
}

func TestStationTypes(t *testing.T) {
	// N 地图里各城抓到最多的那种:Thetford 冶炼炉、Fort Sterling 木工坊、Lymhurst 织布坊
	for id, line := range map[int32]string{431: "ORE", 438: "WOOD", 459: "FIBER", 452: "HIDE", 445: "ROCK"} {
		if l, ok := StationLine(id); !ok || l != line {
			t.Errorf("类型 %d = %q %v, want %s", id, l, ok, line)
		}
	}
	for _, id := range []int32{410, 473, 27, 0} { // 铁匠铺、厨房、绿区融合建筑
		if l, ok := StationLine(id); ok {
			t.Errorf("类型 %d 不是精炼站,得到 %s", id, l)
		}
	}
	for _, l := range Lines {
		ids := StationTypes(l)
		if len(ids) != 7 {
			t.Errorf("%s 有 %d 种站,应当是 T2~T8 七种", l, len(ids))
		}
		for i, id := range ids { // 每条线 T2~T8 按 id 升序排
			if s, _ := StationOf(id); s.Tier != i+2 {
				t.Errorf("%s 第 %d 种站(类型 %d)tier = %d", l, i, id, s.Tier)
			}
		}
	}
	// N 地图里实际抓到的:457 是 T6 纺织匠,炼不了 T7/T8 布
	if s, ok := StationOf(457); !ok || s.Line != "FIBER" || s.Tier != 6 {
		t.Errorf("457 = %+v %v", s, ok)
	}
}
