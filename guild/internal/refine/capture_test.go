package refine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/depth"
)

// 真实抓包回归用的本地数据(设计 §4.7 第 4 条)。
//
// **这些文件不进 git**:testdata/local/ 在仓库 .gitignore 里。里面是用户自己抓的挂单,
// 用户要求数据不上传到别处。文件不存在时测试 t.Skip,所以别的机器上跑测试不受影响。
//
// capture_20260926.json 的来源:本机运行中的服务 GET /api/lookup/book(book.Build
// 剔除幽灵单之后的阶梯,不含 stale 档),只读。重新导出(服务要在跑、抓包还在读单窗口内):
//
//	FLIPPER_EXPORT_BOOKS=http://127.0.0.1:18420 go test ./internal/refine -run Test导出抓包阶梯
//
// 2026-09-26 那份和 worked example 用的 scratchpad rf_books.json 逐档比对过(424 个盘口边
// 全部一致),并多了纤维 / 木两条线和 T2 的盘口。
//
// expect_20260926.json 是回归要断言的数:worked example 里的单根成本、利润、断点等等。
// **它们也只放本机**:这些数都是从用户的抓包算出来的,几个数一组合就能倒推出挂单价
// (比如挂卖到手 ÷ 0.935 + 1 就是卖一),所以和抓包一样不进 git。提交的测试代码里
// 只写断言的逻辑和期望值的键名,不写任何由抓包算出来的数。
const (
	captureFile = "testdata/local/capture_20260926.json"
	expectFile  = "testdata/local/expect_20260926.json"
)

// expect 是本地期望值:键 → 数。键的写法见 key()。
type expect map[string]float64

// loadExpect 读本地期望值;没有就跳过调用它的测试(和 loadCapture 一样)。
func loadExpect(t *testing.T) expect {
	t.Helper()
	raw, err := os.ReadFile(expectFile)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("没有本地期望值 %s(不进仓库,见 capture_test.go 的说明),跳过真实抓包回归", expectFile)
	}
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Values map[string]float64 `json:"values"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Values
}

// v 取一个期望值;文件里没有这个键说明期望值文件和测试对不上,直接失败而不是按 0 比。
func (x expect) v(t *testing.T, key string) float64 {
	t.Helper()
	v, ok := x[key]
	if !ok {
		t.Fatalf("期望值文件 %s 里没有 %q", expectFile, key)
	}
	return v
}

// key 拼期望值的键:name|k=1.154|A 这种;k 为 0 时不写 k 那一段。
func key(name string, k float64, parts ...string) string {
	s := name
	if k != 0 {
		s += "|k=" + strconv.FormatFloat(k, 'g', -1, 64)
	}
	for _, p := range parts {
		s += "|" + p
	}
	return s
}

// asks 取抓包里一个盘口边的卖单阶梯(从优到劣,导出时就排好了)。
func (d *captureDump) asks(item, city string) []depth.Level {
	return d.Books[item+"|"+city].Asks.Levels
}

// bids 取买单阶梯。
func (d *captureDump) bids(item, city string) []depth.Level {
	return d.Books[item+"|"+city].Bids.Levels
}

type captureLadder struct {
	Levels    []depth.Level `json:"levels"`
	Seen      time.Time     `json:"seen"`
	Truncated bool          `json:"truncated,omitempty"`
}

type captureBook struct {
	Asks captureLadder `json:"asks"`
	Bids captureLadder `json:"bids"`
}

type captureDump struct {
	Note      string                 `json:"note"`
	Source    string                 `json:"source"`
	FetchedAt time.Time              `json:"fetched_at"`
	Quality   int                    `json:"quality"`
	Books     map[string]captureBook `json:"books"` // "物品|城市"
}

// loadCapture 读本地抓包导出;没有就跳过调用它的测试。
func loadCapture(t *testing.T) *captureDump {
	t.Helper()
	raw, err := os.ReadFile(captureFile)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("没有本地抓包导出 %s(不进仓库,见 capture_test.go 的说明),跳过真实抓包回归", captureFile)
	}
	if err != nil {
		t.Fatal(err)
	}
	var d captureDump
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

// books 转成引擎的输入。only 非空时只收这些城市。
func (d *captureDump) books(only ...string) Books {
	keep := map[string]bool{}
	for _, c := range only {
		keep[c] = true
	}
	out := Books{}
	for k, b := range d.Books {
		item, city, _ := strings.Cut(k, "|")
		if len(keep) > 0 && !keep[city] {
			continue
		}
		out[Key{ItemID: item, City: city}] = Book{
			Asks: Ladder{Levels: b.Asks.Levels, Seen: b.Asks.Seen, Truncated: b.Asks.Truncated, Source: "capture"},
			Bids: Ladder{Levels: b.Bids.Levels, Seen: b.Bids.Seen, Truncated: b.Bids.Truncated, Source: "capture"},
		}
	}
	return out
}

// captureItems 是导出的物品:四条线 T2~T8 的原料和半成品,加四种心。
func captureItems() []string {
	var out []string
	for _, l := range Lines {
		for tier := MinTier; tier <= MaxTier; tier++ {
			for _, e := range Enchants(tier) {
				out = append(out, l.RawID(tier, e), l.RefinedID(tier, e))
			}
		}
		out = append(out, l.Heart)
	}
	sort.Strings(out)
	return out
}

// Test导出抓包阶梯 不是测试,是导出工具:只在设了 FLIPPER_EXPORT_BOOKS 时跑。
// 只读本机服务(只认 127.0.0.1 / localhost),写到 testdata/local/,不发往任何别的地方。
func Test导出抓包阶梯(t *testing.T) {
	base := os.Getenv("FLIPPER_EXPORT_BOOKS")
	if base == "" {
		t.Skip("没设 FLIPPER_EXPORT_BOOKS,不导出")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatalf("只从本机服务导出,得到 %q", base)
	}
	type lookupSide struct {
		Levels []struct {
			Price int64 `json:"price"`
			Qty   int64 `json:"qty"`
			Stale bool  `json:"stale"`
		} `json:"levels"`
		Truncated  bool       `json:"truncated"`
		NewestSeen *time.Time `json:"newest_seen"`
	}
	type lookupBook struct {
		Sell lookupSide `json:"sell"`
		Buy  lookupSide `json:"buy"`
	}
	conv := func(s lookupSide) captureLadder {
		var l captureLadder
		for _, lv := range s.Levels {
			if !lv.Stale { // stale 档是上一轮翻到、这一轮没翻到的,不能拿来吃单
				l.Levels = append(l.Levels, depth.Level{Price: lv.Price, Qty: lv.Qty})
			}
		}
		l.Truncated = s.Truncated
		if s.NewestSeen != nil {
			l.Seen = s.NewestSeen.UTC()
		}
		return l
	}
	client := &http.Client{Timeout: 40 * time.Second}
	dump := captureDump{
		Note:      "用户本机抓包,只供本机回归测试,不进仓库、不上传",
		Source:    strings.TrimRight(base, "/") + "/api/lookup/book(book.Build 剔除幽灵单后的阶梯,不含 stale 档)",
		FetchedAt: time.Now().UTC(),
		Quality:   1,
		Books:     map[string]captureBook{},
	}
	for _, city := range []string{"Thetford", "Martlock"} {
		for _, item := range captureItems() {
			q := url.Values{"item": {item}, "city": {city}, "quality": {"1"}}
			resp, err := client.Get(strings.TrimRight(base, "/") + "/api/lookup/book?" + q.Encode())
			if err != nil {
				t.Fatal(err)
			}
			var lb lookupBook
			err = json.NewDecoder(resp.Body).Decode(&lb)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("%s @ %s: HTTP %d %v", item, city, resp.StatusCode, err)
			}
			b := captureBook{Asks: conv(lb.Sell), Bids: conv(lb.Buy)}
			if len(b.Asks.Levels) == 0 && len(b.Bids.Levels) == 0 {
				continue
			}
			dump.Books[item+"|"+city] = b
		}
	}
	if err := os.MkdirAll(filepath.Dir(captureFile), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(dump, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(captureFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写 %s:%d 个盘口", captureFile, len(dump.Books))
}
