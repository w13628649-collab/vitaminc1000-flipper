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
