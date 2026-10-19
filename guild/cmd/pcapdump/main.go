// pcapdump 离线分析抓包录下来的 .pcap / .pcapng(客户端 -record 录的,或者 Wireshark 存的)。
//
// 用途:照着真实数据写解码器、核对字段布局、看一段游戏里都飞过哪些消息。
//
//	pcapdump 录包.pcap                          每种消息出现几次(带码表里的名字)
//	pcapdump -dump event:448 录包.pcap           把门的事件逐条打出来,参数带 Go 类型
//	pcapdump -dump op_response:92,event:81 -out x.jsonl *.pcap
//	pcapdump -dump all -out 全部.jsonl 录包.pcap
//
// 回放时解析器用包上的抓包时间,不用墙钟。只在本地解析,不连服务端、不上传。
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"albion-guild/internal/capture"
	"albion-guild/internal/model"
	"albion-guild/internal/protocol"
)

type msgKey struct {
	kind string // op_request / op_response / event,和样本的方向一致
	code int    // -1 表示没有码(市场挂单走的那条没有参数表的路)
}

type dumpLine struct {
	Time   time.Time       `json:"t"`
	Kind   string          `json:"kind"`
	Code   int             `json:"code"`
	Name   string          `json:"name,omitempty"`
	Params json.RawMessage `json:"params"`
}

func main() {
	var (
		port      = flag.Int("port", capture.DefaultPort, "Photon 端口")
		codesPath = flag.String("codes", "", "码表 photon-codes.json;留空自动找 docs/client-data/photon-codes.json")
		dumpSpec  = flag.String("dump", "", "逐条打印这些消息的全部参数:方向:码,逗号分隔;all 全打")
		outPath   = flag.String("out", "", "-dump 的结果写到这个文件(JSON Lines);留空打到标准输出")
		top       = flag.Int("top", 0, "消息清单只列出现最多的前 N 种,0 全列")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: pcapdump [参数] 录包.pcap [更多.pcap ...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	names := loadNames(*codesPath)
	dumpAll, dumpSet, err := parseDump(*dumpSpec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var out io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	var pktTime time.Time
	counts := map[msgKey]int{}
	dumped := 0
	note := func(kind string, code int, params map[byte]any) {
		k := msgKey{kind, code}
		counts[k]++
		if dumpAll || dumpSet[k] {
			_ = enc.Encode(dumpLine{Time: pktTime, Kind: kind, Code: code,
				Name: names.name(kind, code), Params: protocol.TagParams(params)})
			dumped++
		}
	}

	// 业务层照常跑一遍,看解出来多少挂单、建筑
	p := protocol.New()
	p.SetClock(func() time.Time { return pktTime })
	var orders, stations, trades int
	p.OnOrders = func(o []model.MarketOrder) { orders += len(o) }
	p.OnStations = func(o []model.StationObs) { stations += len(o) }
	p.OnTrade = func(tr model.MyTrade) {
		trades++
		fmt.Fprintf(os.Stderr, "成交 %s %s %s ×%d @%d = %d (%s)\n", tr.ObservedAt.Format(time.DateTime),
			tr.Kind, tr.ItemID, tr.Amount, tr.UnitPrice, tr.Total, tr.Location)
	}

	h := capture.Handler{
		OnRequest: func(c byte, params map[byte]any) {
			note(model.SampleOpRequest, codeOf(params, 253), params)
			p.HandleRequest(c, params)
		},
		OnResponse: func(c byte, rc int16, _ string, params map[byte]any) {
			note(model.SampleOpResponse, codeOf(params, 253), params)
			p.HandleResponseCode(c, rc, params)
		},
		OnEvent: func(c byte, params map[byte]any) {
			// 没有 252 时真码就是派发字节(Move 就是这样,见 protocol.HandleEvent)
			code := codeOf(params, 252)
			if code < 0 {
				code = int(c)
			}
			note(model.SampleEvent, code, params)
			p.HandleEvent(c, params)
		},
	}

	ctx := context.Background()
	for _, path := range flag.Args() {
		st, err := capture.ReadFile(ctx, path, *port, h, func(t time.Time) { pktTime = t })
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "%s: %d 个包,%d 个是 Albion 的,%s → %s\n", filepath.Base(path),
			st.Packets, st.Fed, st.First.Format(time.DateTime), st.Last.Format(time.DateTime))
	}

	keys := make([]msgKey, 0, len(counts))
	total := 0
	for k, n := range counts {
		keys = append(keys, k)
		total += n
	}
	slices.SortFunc(keys, func(a, b msgKey) int {
		if c := cmp.Compare(counts[b], counts[a]); c != 0 {
			return c
		}
		if c := cmp.Compare(a.kind, b.kind); c != 0 {
			return c
		}
		return cmp.Compare(a.code, b.code)
	})
	if *top > 0 && len(keys) > *top {
		keys = keys[:*top]
	}
	tw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "次数\t占比\t方向\t码\t名字\t\n")
	for _, k := range keys {
		code := strconv.Itoa(k.code)
		if k.code < 0 {
			code = "-"
		}
		fmt.Fprintf(tw, "%d\t%.1f%%\t%s\t%s\t%s\t\n", counts[k], 100*float64(counts[k])/float64(max(total, 1)),
			k.kind, code, names.name(k.kind, k.code))
	}
	_ = tw.Flush()
	fmt.Fprintf(os.Stderr, "\n共 %d 条消息、%d 种;解出挂单 %d 条、建筑 %d 个、自己的成交 %d 笔",
		total, len(counts), orders, stations, trades)
	if dumpAll || len(dumpSet) > 0 {
		fmt.Fprintf(os.Stderr, ";逐条打印 %d 条", dumped)
	}
	fmt.Fprintln(os.Stderr)
}

// codeOf 取 params[key] 里的真码。没有就是 -1(市场挂单那条路没有参数表)
func codeOf(params map[byte]any, key byte) int {
	switch v := params[key].(type) {
	case byte:
		return int(v)
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	}
	return -1
}

func parseDump(spec string) (all bool, set map[msgKey]bool, err error) {
	set = map[msgKey]bool{}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false, set, nil
	}
	if spec == "all" {
		return true, set, nil
	}
	for _, item := range strings.Split(spec, ",") {
		kind, code, ok := strings.Cut(strings.TrimSpace(item), ":")
		n, convErr := strconv.Atoi(code)
		if !ok || convErr != nil {
			return false, nil, fmt.Errorf("-dump 写法是 方向:码,比如 event:448,得到 %q", item)
		}
		switch kind {
		case model.SampleOpRequest, model.SampleOpResponse, model.SampleEvent:
		default:
			return false, nil, fmt.Errorf("方向只能是 op_request / op_response / event,得到 %q", kind)
		}
		set[msgKey{kind, n}] = true
	}
	return false, set, nil
}

// codeNames 是码表:方向 → 码 → 名字。
type codeNames struct{ ops, events map[string][]string }

func (c codeNames) name(kind string, code int) string {
	m := c.ops
	if kind == model.SampleEvent {
		m = c.events
	}
	if v := m[strconv.Itoa(code)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// loadNames 读码表。找不到不报错,只是清单里没有名字
func loadNames(path string) codeNames {
	candidates := []string{path}
	if path == "" {
		candidates = []string{"docs/client-data/photon-codes.json", "../docs/client-data/photon-codes.json"}
		if exe, err := os.Executable(); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(exe), "photon-codes.json"))
		}
	}
	for _, c := range candidates {
		raw, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var v struct {
			Operations map[string][]string `json:"operations"`
			Events     map[string][]string `json:"events"`
		}
		if json.Unmarshal(raw, &v) == nil {
			return codeNames{v.Operations, v.Events}
		}
	}
	fmt.Fprintln(os.Stderr, "没找到码表 photon-codes.json,清单里不显示名字(用 -codes 指定)")
	return codeNames{}
}
