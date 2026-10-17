// Package world 把抓包拿到的地点 id 收敛成 AODP 用的城市名。
//
// 抓包补进来的是**市场**那个区域 id(0007 = Thetford Market),而 AODP、
// 扫描器、界面全都按城市名(Thetford)做 key。不收敛的话两路数据永远是两个
// key,融合一次都不会发生,抓到的东西在界面上一条都对不上。
//
// 这张表和归一化规则照抄 AODP 服务端自己的 lib/location.rb(冻结副本的
// SHA-256 见 docs/client-data.md),再用客户端 1.32.030.345319 解出来的
// market-locations.json 交叉核对过。照抄而不是自己推,是因为融合的另一端
// 就是 AODP:它认什么我们就得认什么,推得再合理只要和它不一致就合不上。
package world

import (
	"strconv"
	"strings"
)

// Black Market 和 Smugglers Network 是两个要单独对待的地点。
//
// 它们**不是城市**,但 AODP 把它们当成独立 location 收价,所以我们也当成
// 独立的 key。尤其黑市:它挂的是 NPC 买单(游戏里那个标签页就叫「需求」),
// 价格逻辑和玩家市场完全不同,混进 Caerleon 会把整条跨城路线算歪。
const (
	BlackMarket      = "Black Market"
	SmugglersNetwork = "Smugglers Network"
)

// locations 是地点 id → AODP 认的名字。
//
// 注意 Thetford 是 7 不是 0007:AODP 内部存的是整数,"0007" 走 strconv 之后
// 就是 7。我们这张表按整数存,字符串形态在 normalize 里统一。
var locations = map[int]string{
	7:    "Thetford",
	1002: "Lymhurst",
	2004: "Bridgewatch",
	3008: "Martlock",
	4002: "Fort Sterling",
	3005: "Caerleon",
	5003: "Brecilien",
	// 黑市在 Caerleon 城本体那个 cluster 里(3003),**和 Caerleon 市场 3005
	// 是两个地方**。客户端 market-locations.json 里 3003 绑的 marketplace
	// 实体类型就是 BLACKMARKET;AODP 的表也把 3003 单列成 Black Market
	3003: BlackMarket,
}

// portals 是传送门城和 Caerleon 第二拍卖行,成交数据并进对应主城。
// 照抄 location.rb 的 PORTAL_TO_CITY。
var portals = map[int]int{
	301:  7,    // Thetford Portal
	1301: 1002, // Lymhurst Portal
	2301: 2004, // Bridgewatch Portal
	3301: 3008, // Martlock Portal
	4301: 4002, // Fort Sterling Portal
	3013: 3005, // Caerleon2(-Auction2 去掉后缀就是它)
}

// smugglers 是走私贩网络的全部据点。
//
// **AODP 把它们当成一个共享盘口**:请求其中任何一个,它会把整个清单展开
// 返回(location.rb 的 get_locations)。所以这里全部收敛成同一个名字 ——
// 分开记没有意义,AODP 那边本来就不分。
var smugglers = map[int]bool{
	8: true, 307: true, 320: true, 321: true, 341: true, 344: true, 349: true,
	353: true, 1012: true, 1312: true, 1323: true, 1339: true, 1342: true,
	1343: true, 1348: true, 1359: true, 2308: true, 2310: true, 2311: true,
	2333: true, 2336: true, 2342: true, 2344: true, 2347: true, 2348: true,
	3306: true, 3344: true, 3345: true, 3351: true, 3355: true, 3357: true,
	4300: true, 4313: true, 4318: true, 4322: true, 4345: true, 4351: true,
	4357: true,
}

// hellDens 是地狱门里开的单子,算在对应主城的市场上。
//
// 照抄 location.rb 的 HELLDEN_TO_MARKET,**包括它那条警告**:
// 3003-HellDen 并进 Caerleon 市场(3005)而不是黑市。
var hellDens = map[string]int{
	"0000-HellDen": 7, "1000-HellDen": 1002, "2000-HellDen": 2004,
	"3004-HellDen": 3008, "3003-HellDen": 3005, "4000-HellDen": 4002,
	"5000-HellDen": 5003,
}

// normalize 把包里的原始地点串解析成 AODP 的整数 location id。
// 第二个返回值是 false 表示这不是一个 AODP 认的交易地点。
//
// 规则逐条对应 location.rb 的 parse_location_integer,顺序也一样 ——
// 顺序要紧:`@` 分支里还要再剥一次 BLACKBANK- 前缀。
func normalize(raw string) (int, bool) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return 0, false
	}

	switch {
	case strings.HasSuffix(id, "-Auction2"):
		id = strings.TrimSuffix(id, "-Auction2")
	case strings.HasSuffix(id, "-HellDen"):
		city, ok := hellDens[id]
		return city, ok
	case strings.Contains(id, "@"):
		// 走私贩据点报上来的形如 xxxx@BLACKBANK-2310,取 @ 之后那段
		_, after, _ := strings.Cut(id, "@")
		id = strings.TrimPrefix(after, "BLACKBANK-")
	case strings.HasPrefix(id, "BLACKBANK-"):
		id = strings.TrimPrefix(id, "BLACKBANK-")
	}

	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, false
	}
	if city, ok := portals[n]; ok {
		return city, true
	}
	if _, ok := locations[n]; ok {
		return n, true
	}
	if smugglers[n] {
		return n, true
	}
	return 0, false
}

// City 返回 raw 对应的 AODP 地点名,认不出的原样返回。
//
// 已经是地点名的也原样返回,所以重复调用是安全的——新客户端将来自己
// 做了收敛,服务端再过一遍也不会坏。
//
// 认不出时**原样放行而不是丢掉**:野外地图、个人岛、还没收录的新地点
// 都会走到这里。合不上 AODP 只是少一路融合,错并到别的城市才是真的坏数据。
func City(raw string) string {
	n, ok := normalize(raw)
	if !ok {
		return raw
	}
	if name, ok := locations[n]; ok {
		return name
	}
	if smugglers[n] {
		return SmugglersNetwork
	}
	return raw
}

// Tradeable 报告这个地点是不是 AODP 认的交易地点。
// 扫描和读簿用它决定要不要把一条挂单纳进来。
func Tradeable(raw string) bool {
	_, ok := normalize(raw)
	return ok
}

// places 是顶栏显示"客户端认为你现在在哪"用的对照表,**只用于显示**。
//
// 和 locations 刻意分开:City 管数据收敛,收的是 AODP 认的交易地点;
// 这里没有那个顾虑——认错了只是顶栏上写错一个字,不会有一条数据被并到
// 别的地方去。所以城市本体、银行、传送门、休息区都收。
// 对照取自客户端的 market-locations.json 和 world.xml。
var places = map[string]string{
	"0000": "Thetford", "0006": "Thetford · 银行", "0007": "Thetford · 市场", "0301": "Thetford · 传送门",
	"1000": "Lymhurst", "1002": "Lymhurst · 市场", "1301": "Lymhurst · 传送门",
	"2000": "Bridgewatch", "2004": "Bridgewatch · 市场", "2301": "Bridgewatch · 传送门",
	"3004": "Martlock", "3008": "Martlock · 市场", "3301": "Martlock · 传送门",
	"4000": "Fort Sterling", "4002": "Fort Sterling · 市场", "4301": "Fort Sterling · 传送门",
	// 3003 是 Caerleon 城本体,黑市 NPC 就在里面。顶栏按"在哪"显示,
	// 所以写城市名;做 key 时 City("3003") 给的是 Black Market,两者故意不同
	"3003": "Caerleon · 城内(黑市)", "3005": "Caerleon · 市场", "3013-Auction2": "Caerleon · 市场",
	"5000": "Brecilien", "5001": "Brecilien", "5003": "Brecilien · 市场",
	"0008": "摩根娜的安息地", "1012": "梅林的安息地", "4300": "亚瑟的安息地",
	"0004": "沼泽十字路口", "1006": "森林十字路口", "2002": "草原十字路口",
	"3002": "高地十字路口", "4006": "山地十字路口",
}

// cityClusters 是城区本体的 cluster id → 城市名,给 op 197(区域地图、制造站)用。
//
// 197 请求点名的是"哪个区",城里的建筑挂在城区本体上(0000),不是市场那个
// 地点 id(0007)——所以不能拿 City 收敛:City("0000") 故意原样放行。
// 对照取自 places(客户端 market-locations.json / world.xml)。
//
// 3003 在这里是 Caerleon,不是黑市:黑市只是那座城里的一个 NPC,城里的工作站
// 归 Caerleon。传送门城(0301…)刻意不并进主城:那是另一座镇子、另一批建筑,
// 费率挂错城就是错的数据
var cityClusters = map[string]string{
	"0000": "Thetford", "1000": "Lymhurst", "2000": "Bridgewatch",
	"3004": "Martlock", "4000": "Fort Sterling", "3003": "Caerleon",
	"5000": "Brecilien", "5001": "Brecilien",
}

// ClusterCity 把 op 197 的区 id 翻成城市名。不是城区本体的原样返回、ok=false
// (野外、传送门城、还没收录的地方),调用方按原始 id 存,不丢数据也不错并。
func ClusterCity(zone string) (string, bool) {
	if c, ok := cityClusters[strings.TrimSpace(zone)]; ok {
		return c, true
	}
	return zone, false
}

// DisplayName 把包里的原始地点 id 翻成给人看的地名:市场 id、城市本体 id、
// 走私贩据点都认,认不出的(个人岛、野外地图……)原样返回。
//
// **只用于显示,不要拿它做 key。** 做 key 用 City:两者对城市本体 id 的回答
// 故意不同(City("0000") 原样返回 "0000",这里给 "Thetford")。
func DisplayName(raw string) string {
	if n, ok := places[raw]; ok {
		return n
	}
	// 走私贩据点有几十个,不逐个列名字,统一显示成网络 + 原始编号
	if n, ok := normalize(raw); ok && smugglers[n] {
		return "走私贩网络 · " + strconv.Itoa(n)
	}
	return raw
}
