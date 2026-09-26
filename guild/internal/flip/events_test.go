package flip

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"albion-guild/internal/hub"
)

// recPublisher 记下每一次 PublishTopic。
type recPublisher struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (p *recPublisher) PublishTopic(topic string, payload []byte) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if topic != hub.TopicScan {
		panic("意外的主题 " + topic)
	}
	p.msgs = append(p.msgs, payload)
	return 1
}

func (p *recPublisher) events(t *testing.T) []ScanEvent {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ScanEvent, len(p.msgs))
	for i, m := range p.msgs {
		if err := json.Unmarshal(m, &out[i]); err != nil {
			t.Fatalf("第 %d 条通知不是合法 JSON: %s", i, m)
		}
	}
	return out
}

// 全量扫描和快速重算发布结果后都推一条;摘要只随内容变,内容没变也照发
func TestPublish_全量和快速重算都推且摘要只随内容变(t *testing.T) {
	s, _, books := newRVService(t, rvConfig())
	rec := &recPublisher{}
	s.Events = rec
	ctx := context.Background()

	full, err := s.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evs := rec.events(t)
	if len(evs) != 1 {
		t.Fatalf("全量扫描应推 1 条,得到 %d", len(evs))
	}
	e := evs[0]
	pub := s.LastScan()
	if e.Type != "scan" || !e.Full || e.Digest == "" || e.Digest != pub.Digest || full.Digest != pub.Digest {
		t.Fatalf("全量:type=scan、full=true、摘要和 /api/scan 的 digest 相同,得到 %+v / %q", e, pub.Digest)
	}
	if !e.EvaluatedAt.Equal(pub.EvaluatedAt) || !e.StartedAt.Equal(pub.StartedAt) ||
		e.Opportunities != len(pub.Opportunities) || e.Routes != len(pub.Routes) || e.Opportunities == 0 {
		t.Fatalf("时刻和条数应和对外结果一致,得到 %+v", e)
	}
	if got := s.LastScanEvent(); got == nil || got.Digest != e.Digest || !got.EvaluatedAt.Equal(e.EvaluatedAt) || !got.Full {
		t.Fatalf("LastScanEvent 应是刚推的那条,得到 %+v", got)
	}

	// 抓包没变:重算照推,full=false,摘要不变(数据龄变了不算)
	time.Sleep(5 * time.Millisecond)
	if _, err := s.Reevaluate(ctx); err != nil {
		t.Fatal(err)
	}
	evs = rec.events(t)
	if len(evs) != 2 || evs[1].Full || evs[1].Digest != e.Digest || !evs[1].EvaluatedAt.After(e.EvaluatedAt) {
		t.Fatalf("内容没变的重算:照推、full=false、摘要不变、evaluated_at 前进,得到 %+v", evs)
	}

	// 翻到了新卖价:摘要变
	books.setAsk("T5_CLOTH", 1400, 50, time.Now().UTC().Add(-time.Minute))
	if _, err := s.Reevaluate(ctx); err != nil {
		t.Fatal(err)
	}
	evs = rec.events(t)
	if len(evs) != 3 || evs[2].Full || evs[2].Digest == e.Digest || evs[2].Digest != s.LastScan().Digest {
		t.Fatalf("内容变了摘要要变,得到 %+v", evs)
	}

	// 下一次全量:AODP 还是那些数、抓包也没变,机会和路线一模一样,但摘要照样变——
	// started_at 换了。界面页脚的"上次全量"、AODP 覆盖率的新鲜度分桶都要跟着重画,
	// 那几个分桶不进摘要(见 scan.Digest),只能靠全量换 started_at 带着刷新
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	evs = rec.events(t)
	if len(evs) != 4 || !evs[3].Full || evs[3].Digest == evs[2].Digest || evs[3].Digest != s.LastScan().Digest ||
		evs[3].StartedAt.Equal(evs[2].StartedAt) {
		t.Fatalf("全量也推;started_at 换了,摘要要变,得到 %+v", evs)
	}

	// 全量之后什么都没变的重算:摘要回到稳定
	if _, err := s.Reevaluate(ctx); err != nil {
		t.Fatal(err)
	}
	evs = rec.events(t)
	if len(evs) != 5 || evs[4].Full || evs[4].Digest != evs[3].Digest {
		t.Fatalf("全量之后内容没变的重算,摘要应和全量那次相同,得到 %+v", evs)
	}
}

// 通知的 JSON 形状是和界面约定好的,一个键都不能多、不能少
func TestPublish_通知的JSON形状(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	rec := &recPublisher{}
	s.Events = rec
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.msgs[0], &m); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"digest", "evaluated_at", "full", "opportunities", "routes", "started_at", "type"}
	if !slices.Equal(keys, want) {
		t.Fatalf("键应为 %v,得到 %v(%s)", want, keys, rec.msgs[0])
	}
	for _, k := range []string{"evaluated_at", "started_at"} {
		if _, err := time.Parse(time.RFC3339, m[k].(string)); err != nil {
			t.Fatalf("%s 应是 RFC3339,得到 %v", k, m[k])
		}
	}
	if m["type"] != "scan" || m["full"] != true {
		t.Fatalf("type/full 不对: %s", rec.msgs[0])
	}
}

// 接了入库计数时每条通知带上 ingest,取的是发布那一刻的值:运行中新出的串城,
// 下一次重算的通知里就有,界面横幅不用等下一次全量去读 /api/coverage
func TestPublish_通知带上入库计数(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	rec := &recPublisher{}
	s.Events = rec
	var mu sync.Mutex
	conflicts := 0
	s.IngestStats = func() any {
		mu.Lock()
		defer mu.Unlock()
		return map[string]any{"location_conflicts": conflicts}
	}
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	conflicts = 2
	mu.Unlock()
	if _, err := s.Reevaluate(ctx); err != nil {
		t.Fatal(err)
	}
	var got []float64
	for _, m := range rec.msgs {
		var v struct {
			Ingest struct {
				LocationConflicts float64 `json:"location_conflicts"`
			} `json:"ingest"`
		}
		if err := json.Unmarshal(m, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v.Ingest.LocationConflicts)
	}
	if !slices.Equal(got, []float64{0, 2}) {
		t.Fatalf("两条通知的串城计数应是 [0 2],得到 %v(%q)", got, rec.msgs)
	}
	if ev := s.LastScanEvent(); ev == nil || ev.Ingest == nil {
		t.Fatalf("/api/stats 的 last_scan_event 也应带上,得到 %+v", ev)
	}
}

// 接真的 hub:只有订了 scan 的连接收到;之后才订的连接一订阅就补到最近一条
func TestPublish_经hub只推给订阅了scan的连接(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	h := hub.NewHub()
	s.Events = h
	sub, keysOnly := hub.NewClient(8), hub.NewClient(8)
	h.Add(sub)
	h.Add(keysOnly)
	h.SubscribeTopics(sub, []string{hub.TopicScan})
	keysOnly.Subscribe([]string{"T5_CLOTH|Lymhurst|1|0"})

	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-sub.Out():
		var ev ScanEvent
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Digest != s.LastScan().Digest || !ev.Full {
			t.Fatalf("订阅者应收到这次全量的通知,得到 %s", msg)
		}
	default:
		t.Fatal("订阅了 scan 的连接没收到")
	}
	select {
	case msg := <-keysOnly.Out():
		t.Fatalf("只订盘口 key 的老客户端不该收到扫描通知: %s", msg)
	default:
	}

	late := hub.NewClient(8)
	h.Add(late)
	h.SubscribeTopics(late, []string{hub.TopicScan})
	select {
	case msg := <-late.Out():
		var ev ScanEvent
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Digest != s.LastScan().Digest {
			t.Fatalf("后订阅的应补到最近一条,得到 %s", msg)
		}
	default:
		t.Fatal("后订阅的连接没补到最近一条")
	}
}

// 没接 hub 时照常发布,只是不推
func TestPublish_没有Events时照常发布(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	if _, err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.LastScan().Digest == "" || s.LastScanEvent() == nil {
		t.Fatal("没接 hub 也要算摘要、记下最近一条通知")
	}
}
