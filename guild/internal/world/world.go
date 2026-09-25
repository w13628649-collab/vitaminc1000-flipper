// Package world 把抓包拿到的地点 id 收敛成 AODP 用的城市名。
//
// 抓包补进来的是**市场**那个区域 id(0007 = Thetford Market),而 AODP、
// 扫描器、界面全都按城市名(Thetford)做 key。不收敛的话两路数据永远是两个
// key,融合一次都不会发生,抓到的东西在界面上一条都对不上。
package world

// markets 只收有依据的条目。对照取自 ao-bin-dumps 的 formatted/world.json,
// 前五个另有实抓数据核对过(docs/packet-capture.md)。
//
// **城市本体的 id(0000 Thetford、3003 Caerleon 这些)刻意不收。**
// 挂单是在市场里看的,实抓数据里从没出现过城市本体 id;而 3003 很可能就是
// 黑市开单子时报上来的位置(黑市 NPC 在 Caerleon 城里,不在 Caerleon Market
// 里),把它并进 Caerleon 会让黑市求购冒充 Caerleon 的买单。
// 拿不准的宁可原样放行:只是合不上 AODP,不会被错并到别的城市去
var markets = map[string]string{
	"0007":          "Thetford",
	"1002":          "Lymhurst",
	"2004":          "Bridgewatch",
	"3008":          "Martlock",
	"4002":          "Fort Sterling",
	"3005":          "Caerleon",
	"3013-Auction2": "Caerleon",
	"5003":          "Brecilien",
}

// City 返回 raw 对应的城市名,认不出的原样返回。
//
// 已经是城市名的也原样返回,所以重复调用是安全的——新客户端将来自己
// 做了收敛,服务端再过一遍也不会坏。
func City(raw string) string {
	if c, ok := markets[raw]; ok {
		return c
	}
	return raw
}

// places 是顶栏显示"客户端认为你现在在哪"用的对照表,**只用于显示**。
//
// 和 markets 刻意分开:City 管数据收敛,只收有依据的市场 id,城市本体 id 收进去
// 会让黑市求购冒充 Caerleon 的买单(见 markets 的注释)。这里没有这个顾虑——
// 认错了只是顶栏上写错一个字,不会有一条数据被并到别的城市去。所以城市本体、
// 银行、传送门都收。对照同样取自 ao-bin-dumps 的 formatted/world.json
// (docs/packet-capture.md 列出的那几条),拿不准的一律不收、原样显示。
var places = map[string]string{
	"0000": "Thetford", "0006": "Thetford · 银行", "0007": "Thetford · 市场", "0301": "Thetford · 传送门",
	"1000": "Lymhurst", "1002": "Lymhurst · 市场",
	"2000": "Bridgewatch", "2004": "Bridgewatch · 市场",
	"3004": "Martlock", "3008": "Martlock · 市场",
	"4000": "Fort Sterling", "4002": "Fort Sterling · 市场",
	"3003": "Caerleon", "3005": "Caerleon · 市场", "3013-Auction2": "Caerleon · 市场",
	"5000": "Brecilien", "5001": "Brecilien", "5003": "Brecilien · 市场",
}

// DisplayName 把包里的原始地点 id 翻成给人看的地名:市场 id 和城市本体 id 都认,
// 认不出的(走私窝点 xxxx@yyyy、个人岛、野外地图……)原样返回。
//
// **只用于显示,不要拿它做 key。** 做 key 用 City:两者对城市本体 id 的回答
// 故意不同(City("0000") 原样返回 "0000")。
func DisplayName(raw string) string {
	if n, ok := places[raw]; ok {
		return n
	}
	return raw
}
