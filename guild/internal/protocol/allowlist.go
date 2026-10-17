package protocol

import "sync"

// 抓包白名单:哪条消息的哪几个参数键可以进业务层。**全仓库只有这一处定义。**
//
// 仓库 CLAUDE.md「不变的核心要求」:抓包只解操作响应(params[253]),一条事件
// (params[252])都不解;白名单按 (码, 参数键) 显式枚举,默认丢弃,不许"全抓后过滤"。
// 判准是"这条数据换个人换个时间看是不是同一个值":是 → 可以收(费率/税率/所有者/
// 建筑布局);否 → 不能收(资源节点/怪/人物位置)。理由见 docs/client-data.md 第二点十节。
//
// 落到代码上是两条硬规矩,测试钉着:
//   - 不在表里的 (方向, 码),业务层拿不到任何参数——Handle* 读完码就返回
//   - 在表里的,表外的键在进业务层之前剥掉(Rule.filter 拷一份新 map,原 map 不再往下传)
//
// 加一行之前先过上面那条判准,并在 Why 里写清楚为什么能收。

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

// Rule 是白名单里的一行。
type Rule struct {
	Kind Kind
	Code int
	Name string // 客户端码表里的名字,只给人看
	// Keys 是允许进业务层的参数键。空 = 业务层只知道"这条消息发生了",一个参数都拿不到
	Keys []byte
	// Sample 为 true 时,原始样本记录器(sample.go)把这条消息带类型标签原样记下来,
	// 用来核对字段布局。默认只记 Keys 里的键——样本不许比业务层多拿一个键。
	// 每个码每次运行最多 20 条
	Sample bool
	// SampleAllKeys 为 true 时样本记**全部键**。这是"全抓",只给用户明确批准过的
	// 消息开(目前只有三条专精事件,批准记录见下面的注释和仓库 CLAUDE.md)
	SampleAllKeys bool
	Why           string
}

// Allowlist 是当前这套码下的完整白名单。码随游戏版本漂移,所以按名字从 OpCodes /
// EventCodes 取,不在这里写死数字;默认码下各行的数值见 allowlist_test.go。
func Allowlist(op OpCodes, ev EventCodes) []Rule {
	return []Rule{
		// ── 操作响应 ──
		{Kind: KindResponse, Code: op.AuctionGetOffers, Name: "AuctionGetOffers", Keys: []byte{0},
			Why: "卖单列表。params[0] 是挂单 JSON 字符串数组;挂单价谁去看都一样"},
		{Kind: KindResponse, Code: op.AuctionGetRequests, Name: "AuctionGetRequests", Keys: []byte{0},
			Why: "买单列表,同上"},
		{Kind: KindResponse, Code: op.Join, Name: "Join", Keys: []byte{1, 2, 8},
			Why: "自己的角色 id / 角色名 / 所在区。城市挂单的 LocationId 在包里是空的,靠所在区补"},
		{Kind: KindResponse, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo",
			Keys: []byte{17, 18, 20, 22, 23, 24, 25, 31, 33, 34},
			// 样本只记上面这几个键,用来核对线上类型(比如 key18 float[][] 每行是不是 2 个)。
			// 92 个键里其余 82 个没人看过,按 AddBuilding / AddRealEstate 的签名很可能有
			// canUse / guildAccess / ownerCharacter Guid 这类"换个人看值就不同"的东西,
			// 全键样本没有用户批准,不开。要找建筑 id、地皮价,先点名键再加进 Keys
			Sample: true,
			Why: "区域地图里的建筑:类型、坐标、耐久、营养、银、所有者、两档使用费(23/31 含义未知,原样存)。" +
				"费率和所有者换个人看是同一个值;坐标只当「哪个建筑」的连接键,不建地图层"},

		// ── 操作请求 ──
		{Kind: KindRequest, Code: op.GetGameServerByCluster, Name: "GetGameServerByCluster", Keys: []byte{0},
			Why: "切区:key0 是要去的区 id"},
		{Kind: KindRequest, Code: op.GetClusterMapInfo, Name: "GetClusterMapInfo", Keys: []byte{0},
			Why: "key0 是区 id。响应里不一定带区 id,靠它关联"},
		{Kind: KindRequest, Code: op.AuctionGetOffers, Name: "AuctionGetOffers",
			Why: "只要知道「发了市场请求」,给加密判定做门控,不读参数"},
		{Kind: KindRequest, Code: op.AuctionGetRequests, Name: "AuctionGetRequests",
			Why: "同上"},

		// ── 事件:默认全丢。下面三行是「一条事件都不解」的唯一例外 ──
		//
		// 批准:用户 2026-10-17 明确批准(会进游戏打开专精界面让我们抓样本)。
		// 判准(仓库 CLAUDE.md):专精是玩家自己的进度,换个时间看是同一个值,
		// 不是"服务端因为你站在那里才推给你的"东西。
		// 范围:**只记原始样本**(Keys 为空,业务层一个键都拿不到),用来解字段布局;
		// 解码器写好之前不进任何业务逻辑。码见 docs/client-data.md 第四节
		//
		// 拿不准的一条:这三条什么时候下发没验证过,很可能是登录或进区时推,
		// 不一定是打开专精界面时推。样本里的 observed_at 能看出来
		{Kind: KindEvent, Code: ev.FullAchievementInfo, Name: "FullAchievementInfo", Sample: true, SampleAllKeys: true,
			Why: "专精全量。用户 2026-10-17 批准的例外,只记样本"},
		{Kind: KindEvent, Code: ev.AchievementProgressInfo, Name: "AchievementProgressInfo", Sample: true, SampleAllKeys: true,
			Why: "专精增量。用户 2026-10-17 批准的例外,只记样本"},
		{Kind: KindEvent, Code: ev.FullAchievementProgressInfo, Name: "FullAchievementProgressInfo", Sample: true, SampleAllKeys: true,
			Why: "专精等级。用户 2026-10-17 批准的例外,只记样本"},
	}
}

// EventCodes 是白名单例外里用到的事件码(只有这三个,见 Allowlist)。
type EventCodes struct {
	FullAchievementInfo         int
	AchievementProgressInfo     int
	FullAchievementProgressInfo int
}

// DefaultEventCodes 取自客户端 1.32.030.345319 的码表(docs/client-data/photon-codes.json)。
func DefaultEventCodes() EventCodes {
	return EventCodes{
		FullAchievementInfo:         150,
		AchievementProgressInfo:     152,
		FullAchievementProgressInfo: 153,
	}
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

// SampleRule 按样本上的方向字符串(Kind.String())和码,查允许记样本的那一行。
// 服务端收上传时用:「只收这几条」不能只靠客户端执行——老版本、开发版、
// 改过的客户端都能往 /api/upload 里塞任意码的样本
func SampleRule(op OpCodes, ev EventCodes, kind string, code int) (Rule, bool) {
	for _, r := range Allowlist(op, ev) {
		if r.Sample && r.Kind.String() == kind && r.Code == code {
			return r, true
		}
	}
	return Rule{}, false
}

// SampleKeyAllowed 判断样本里出现参数键 k 合不合规:全键样本什么键都行,
// 否则只能是 Keys 里的
func (r Rule) SampleKeyAllowed(k int) bool {
	if r.SampleAllKeys {
		return true
	}
	for _, x := range r.Keys {
		if int(x) == k {
			return true
		}
	}
	return false
}

// filter 拷出只含白名单键的新 map。原 map 不再往业务层传:
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
