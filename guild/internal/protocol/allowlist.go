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

// Allowlist 是当前这套码下的完整表。码随游戏版本漂移,所以按名字从 OpCodes / EventCodes 取,
// 不在这里写死数字;默认码下各行的数值见 allowlist_test.go。
func Allowlist(op OpCodes, ev EventCodes) []Rule {
	return []Rule{
		// ── 操作响应 ──
		{Kind: KindResponse, Code: op.AuctionGetOffers, Name: "AuctionGetOffers", Keys: []byte{0},
			Why: "卖单列表。params[0] 是挂单 JSON 字符串数组"},
		{Kind: KindResponse, Code: op.AuctionGetRequests, Name: "AuctionGetRequests", Keys: []byte{0},
			Why: "买单列表,同上"},
		{Kind: KindResponse, Code: op.Join, Name: "Join", Keys: []byte{0, 1, 2, 8},
			Why: "自己的对象 id / 角色 id / 角色名 / 所在区。城市挂单的 LocationId 在包里是空的,靠所在区补;" +
				"对象 id 用来认 UpdateMoney / 专注事件是不是自己的"},
		{Kind: KindResponse, Code: op.ChangeCluster, Name: "ChangeCluster", Keys: []byte{0},
			Why: "切区的响应,key0 是目的地区 id。现在的游戏里切区走它(OpenRadar 两万九千个包的" +
				"语料里 op 17 一次都没出现),Join 响应的 key8 兜底"},
		{Kind: KindResponse, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo",
			Keys: []byte{17, 18, 20, 22, 23, 24, 25, 31, 33, 34},
			Why: "区域地图里的建筑:类型、坐标、耐久、营养、银、所有者、两档使用费(23/31 含义未知,原样存)。" +
				"坐标只当「哪个建筑」的连接键"},

		{Kind: KindResponse, Code: op.AuctionBuyOffer, Name: "AuctionBuyOffer",
			Why: "即时买的结果,只看 returnCode(0 = 成交),不读参数"},
		{Kind: KindResponse, Code: op.AuctionSellSpecificItemRequest, Name: "AuctionSellSpecificItemRequest",
			Why: "即时卖的结果,同上"},
		{Kind: KindResponse, Code: op.GetMailInfos, Name: "GetMailInfos", Keys: []byte{3, 7, 11, 12},
			Why: "邮件列表:邮件 id、地点、类型、收到时间(.NET ticks)。读邮件时靠类型拆正文"},
		{Kind: KindResponse, Code: op.ReadMail, Name: "ReadMail", Keys: []byte{0, 1},
			Why: "一封邮件:id 和正文。市场邮件的正文是 | 分隔的成交明细"},

		// ── 操作请求 ──
		{Kind: KindRequest, Code: op.GetGameServerByCluster, Name: "GetGameServerByCluster", Keys: []byte{0},
			Why: "切区:key0 是要去的区 id"},
		{Kind: KindRequest, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo", Keys: []byte{0},
			Why: "key0 是区 id。响应里不一定带区 id,靠它关联"},
		{Kind: KindRequest, Code: op.AuctionGetOffers, Name: "AuctionGetOffers",
			Why: "只要知道「发了市场请求」,给加密判定做门控,不读参数"},
		{Kind: KindRequest, Code: op.AuctionGetRequests, Name: "AuctionGetRequests",
			Why: "同上"},
		{Kind: KindRequest, Code: op.AuctionBuyOffer, Name: "AuctionBuyOffer", Keys: []byte{1, 2},
			Why: "即时买:key1 件数、key2 订单号。物品和单价从最近看过的挂单里按订单号查"},
		{Kind: KindRequest, Code: op.AuctionSellSpecificItemRequest, Name: "AuctionSellSpecificItemRequest",
			Keys: []byte{1, 4}, Why: "即时卖:key1 订单号、key4 件数"},

		// ── 事件 ──
		{Kind: KindEvent, Code: ev.UpdateMoney, Name: "UpdateMoney", Keys: []byte{0, 1},
			Why: "银币余额:key0 对象 id、key1 当前银币(×10000)"},
		{Kind: KindEvent, Code: ev.CraftingFocusUpdate, Name: "CraftingFocusUpdate", Keys: []byte{0, 3},
			Why: "专注余额:key0 对象 id、key3 余额(float)"},
	}
}

// EventCodes 是业务层处理器用到的事件码。
type EventCodes struct {
	UpdateMoney         int
	CraftingFocusUpdate int
}

// DefaultEventCodes 取自客户端 1.32.030.345319 的码表(docs/client-data/photon-codes.json)。
func DefaultEventCodes() EventCodes {
	return EventCodes{UpdateMoney: 81, CraftingFocusUpdate: 10}
}

type ruleKey struct {
	kind Kind
	code int
}

// allowIndex 是 Allowlist 按 (方向, 码) 建的索引,码表变了就重建。
// 每个包都要查一次,不能每次现建一遍切片
type allowIndex struct {
	mu     sync.Mutex
	built  bool
	codes  OpCodes
	events EventCodes
	rules  map[ruleKey]Rule
}

func (a *allowIndex) lookup(op OpCodes, ev EventCodes, kind Kind, code int) (Rule, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.built || a.codes != op || a.events != ev {
		a.rules = map[ruleKey]Rule{}
		for _, r := range Allowlist(op, ev) {
			a.rules[ruleKey{r.Kind, r.Code}] = r
		}
		a.codes, a.events, a.built = op, ev, true
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
