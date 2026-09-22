package aodp

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestISOStringParsedAsUTC(t *testing.T) {
	got := ParseTime("2026-09-20T15:30:00")
	want := time.Date(2026, 9, 20, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("得到 %v,想要 %v", got, want)
	}
}

// AODP 用 0001-01-01 表示"没有数据"。当成"很久以前"会让新鲜度判断失效。
func TestEmptySentinelBecomesNoData(t *testing.T) {
	for _, raw := range []string{`"0001-01-01T00:00:00"`, `""`, `null`} {
		var s Stamp
		if err := s.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if s.Valid() {
			t.Fatalf("%s 解析成了 %v,应该判成没有数据", raw, s.T)
		}
	}
}

// REST v2 返回 ISO 字符串,NATS 流返回 ticks,两种都要认。
func TestCSharpTicks(t *testing.T) {
	ticks := int64(621_355_968_000_000_000 + 10_000_000*3600)
	var s Stamp
	if err := s.UnmarshalJSON([]byte(fmt.Sprint(ticks))); err != nil {
		t.Fatal(err)
	}
	want := time.Date(1970, 1, 1, 1, 0, 0, 0, time.UTC)
	if !s.T.Equal(want) {
		t.Fatalf("得到 %v,想要 %v", s.T, want)
	}
}

func TestOffsetInputNormalizedToUTC(t *testing.T) {
	got := ParseTime("2026-09-20T23:30:00+08:00")
	want := time.Date(2026, 9, 20, 15, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("得到 %v,想要 %v", got, want)
	}
}

func TestChunkingStaysWithinURLBudget(t *testing.T) {
	var ids []string
	for i := range 200 {
		ids = append(ids, fmt.Sprintf("T%d_ARMOR_CLOTH_SET1", i))
	}
	const prefix, suffix, limit = 60, 80, 500
	batches := chunkByURLLength(ids, prefix, suffix, limit)

	var flat []string
	for _, b := range batches {
		flat = append(flat, b...)
		if n := prefix + len(strings.Join(b, ",")) + suffix; n > limit {
			t.Fatalf("一批 URL 长 %d,超过上限 %d", n, limit)
		}
	}
	if len(flat) != len(ids) {
		t.Fatalf("分批后 %d 个 id,原本 %d 个", len(flat), len(ids))
	}
	for i := range ids {
		if flat[i] != ids[i] { // 不丢不乱序
			t.Fatalf("第 %d 个变成了 %s,原本 %s", i, flat[i], ids[i])
		}
	}
}

func TestOverlongSingleIDStillGetsItsOwnBatch(t *testing.T) {
	ids := []string{strings.Repeat("X", 400), "T4_CLOTH"}
	var flat []string
	for _, b := range chunkByURLLength(ids, 50, 50, 200) {
		flat = append(flat, b...)
	}
	if len(flat) != 2 || flat[0] != ids[0] || flat[1] != ids[1] {
		t.Fatalf("超长 id 被丢掉了: %v", flat)
	}
}

func TestFewItemsMeanOneRequest(t *testing.T) {
	var ids []string
	for i := range 66 {
		ids = append(ids, fmt.Sprintf("T%d_CLOTH", i))
	}
	if n := len(chunkByURLLength(ids, 60, 120, 3500)); n != 1 {
		t.Fatalf("分成了 %d 批,66 个物品应该一批发完", n)
	}
}

func TestLimiterBlocksWhenOverQuota(t *testing.T) {
	lim := newLimiter(1000, 2)
	// 用假时钟,不真等
	clock := time.Now()
	var slept time.Duration
	lim.now = func() time.Time { return clock }
	lim.sleep = func(d time.Duration) { slept += d; clock = clock.Add(d) }

	lim.acquire()
	lim.acquire()
	if slept != 0 {
		t.Fatalf("前两次就等了 %v,额度内不该等", slept)
	}
	// 第三次必须等 5 分钟窗口滑动
	lim.acquire()
	if slept < 5*time.Minute {
		t.Fatalf("第三次只等了 %v,应该等满 5 分钟窗口", slept)
	}
}
