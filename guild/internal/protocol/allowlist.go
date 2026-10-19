package protocol

import "sync"

// 业务层白名单:每个类型化处理器能拿到哪条消息的哪几个参数键。**全仓库只有这一处定义。**
//
// 2026-10-19 起抓包方针是**明文全抓全解**(用户决定,见仓库 CLAUDE.md):每一条认得出码的
// 请求、响应、事件都先记一份全键原始样本(sample.go),要用哪条就照着样本写解码器。
// 这张表管的是"处理器要哪几个键":
//   - 不在表里的 (方向, 码):样本照记,业务层拿不到参数——还没有处理器要它
//   - 在表里的:表外的键在进业务层之前剥掉(Rule.filter 拷一份新 map)。
//     处理器只看得到自己声明过的键,这张表就是各处理器读哪些字段的文档
//
// 加处理器时在这里加一行,Why 里写清楚这几个键是什么。

// Kind 是消息的方向。
type Kind uint8

const (
	KindRequest  Kind = iota + 1 // 我们发出去的操作请求
	KindResponse                 // 服务端回的操作响应
	KindEvent                    // 服务端主动推的事件
)

func (k Kind) String() string {
	switch k {
	case KindRequest:
		return "op_request"
	case KindResponse:
		return "op_response"
	case KindEvent:
		return "event"
	}
	return "unknown"
}

// Rule 是表里的一行。
type Rule struct {
	Kind Kind
	Code int
	Name string // 客户端码表里的名字,只给人看
	// Keys 是交给处理器的参数键。空 = 处理器只知道"这条消息发生了",一个参数都拿不到
	Keys []byte
	Why  string
}

// Allowlist 是当前这套码下的完整表。码随游戏版本漂移,所以按名字从 OpCodes 取,
// 不在这里写死数字;默认码下各行的数值见 allowlist_test.go。
func Allowlist(op OpCodes) []Rule {
	return []Rule{
		// ── 操作响应 ──
		{Kind: KindResponse, Code: op.AuctionGetOffers, Name: "AuctionGetOffers", Keys: []byte{0},
			Why: "卖单列表。params[0] 是挂单 JSON 字符串数组"},
		{Kind: KindResponse, Code: op.AuctionGetRequests, Name: "AuctionGetRequests", Keys: []byte{0},
			Why: "买单列表,同上"},
		{Kind: KindResponse, Code: op.Join, Name: "Join", Keys: []byte{1, 2, 8},
			Why: "自己的角色 id / 角色名 / 所在区。城市挂单的 LocationId 在包里是空的,靠所在区补"},
		{Kind: KindResponse, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo",
			Keys: []byte{17, 18, 20, 22, 23, 24, 25, 31, 33, 34},
			Why: "区域地图里的建筑:类型、坐标、耐久、营养、银、所有者、两档使用费(23/31 含义未知,原样存)。" +
				"坐标只当「哪个建筑」的连接键"},

		// ── 操作请求 ──
		{Kind: KindRequest, Code: op.GetGameServerByCluster, Name: "GetGameServerByCluster", Keys: []byte{0},
			Why: "切区:key0 是要去的区 id"},
		{Kind: KindRequest, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo", Keys: []byte{0},
			Why: "key0 是区 id。响应里不一定带区 id,靠它关联"},
		{Kind: KindRequest, Code: op.AuctionGetOffers, Name: "AuctionGetOffers",
			Why: "只要知道「发了市场请求」,给加密判定做门控,不读参数"},
		{Kind: KindRequest, Code: op.AuctionGetRequests, Name: "AuctionGetRequests",
			Why: "同上"},
	}
}

type ruleKey struct {
	kind Kind
	code int
}

// allowIndex 是 Allowlist 按 (方向, 码) 建的索引,码表变了就重建。
// 每个包都要查一次,不能每次现建一遍切片
type allowIndex struct {
	mu    sync.Mutex
	built bool
	codes OpCodes
	rules map[ruleKey]Rule
}

func (a *allowIndex) lookup(op OpCodes, kind Kind, code int) (Rule, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.built || a.codes != op {
		a.rules = map[ruleKey]Rule{}
		for _, r := range Allowlist(op) {
			a.rules[ruleKey{r.Kind, r.Code}] = r
		}
		a.codes, a.built = op, true
	}
	r, ok := a.rules[ruleKey{kind, code}]
	return r, ok
}

// filter 拷出只含表内键的新 map。原 map 不再往业务层传:
// 后面谁手滑多读一个键,读到的也只会是 nil
func (r Rule) filter(params map[byte]any) map[byte]any {
	out := make(map[byte]any, len(r.Keys))
	for _, k := range r.Keys {
		if v, ok := params[k]; ok {
			out[k] = v
		}
	}
	return out
}
