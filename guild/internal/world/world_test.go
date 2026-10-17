package world

import (
	"strings"
	"testing"
)

func TestCity_市场id收敛成AODP的地点名(t *testing.T) {
	cases := map[string]string{
		"0007": "Thetford", // AODP 内部存整数 7,"0007" 走 Atoi 就是它
		"7":    "Thetford",
		"1002": "Lymhurst",
		"2004": "Bridgewatch",
		"3008": "Martlock",
		"4002": "Fort Sterling",
		"3005": "Caerleon",
		"5003": "Brecilien",
	}
	for raw, want := range cases {
		if got := City(raw); got != want {
			t.Errorf("City(%q) = %q,想要 %q", raw, got, want)
		}
	}
}

// 黑市挂的是 NPC 买单(游戏里那个标签页就叫「需求」),价格逻辑和玩家市场
// 完全不同。它在 Caerleon 城本体那个 cluster(3003)里,**绝不能并进 Caerleon**
// ——并了的话整条跨城路线会拿 NPC 求购价当 Caerleon 的买单算。
//
// 依据:客户端 market-locations.json 里 3003 绑的 marketplace 实体类型是
// BLACKMARKET;AODP 的 location.rb 也把 3003 单列成 Black Market。
func TestCity_黑市是独立地点不并进Caerleon(t *testing.T) {
	if got := City("3003"); got != BlackMarket {
		t.Fatalf("City(\"3003\") = %q,想要 %q", got, BlackMarket)
	}
	if City("3003") == City("3005") {
		t.Fatal("黑市和 Caerleon 市场收敛成了同一个 key")
	}
}

// Caerleon 第二拍卖行。AODP 先去掉 -Auction2 后缀,再按 PORTAL_TO_CITY
// 把 3013 并进 3005。
func TestCity_Caerleon第二拍卖行并进主市场(t *testing.T) {
	if got := City("3013-Auction2"); got != "Caerleon" {
		t.Errorf("City(\"3013-Auction2\") = %q,想要 Caerleon", got)
	}
}

// 传送门城的单子算在对应主城上,照抄 AODP 的 PORTAL_TO_CITY。
// 我们融合的另一端就是它,它合我们也得合,不然同一批货两个 key。
func TestCity_传送门城并进主城(t *testing.T) {
	cases := map[string]string{
		"0301": "Thetford", "1301": "Lymhurst", "2301": "Bridgewatch",
		"3301": "Martlock", "4301": "Fort Sterling",
	}
	for raw, want := range cases {
		if got := City(raw); got != want {
			t.Errorf("City(%q) = %q,想要 %q", raw, got, want)
		}
	}
}

// docs/client-data.md 第二点九节第 3 条:客户端的 marketplacegroup 说 Thetford Portal、
// Antiquarian's Den、Thetford Market 是**同一个订单簿**。三个入口必须收敛成同一个 key:
// 收不拢的话同一张单(同一个 order_id)从两个入口看到,ingest 会把它当成串城
// (身份指纹里有地点),一边计冲突一边暂停那两个盘口的幽灵剔除。
//
// 能钉住的:市场本体(0007 / 7)、传送门(0301 / 301)、AODP 表里并进 Thetford 市场的
// 0000-HellDen。**Antiquarian's Den 在包里报的是哪个原始 id 本地核不了**
// (cache 里没有 world.txt,location.rb 的冻结副本不在这台机器上),要一条实抓:
// 站在那里时 Join 的 params[8]。如果它不在上面这几个里,City 会原样放行——
// 不会错并,但会和 Thetford 市场的同一批单互相算成串城,到时候在这里补一行
func TestCity_Thetford同一订单簿的入口收敛成一个key(t *testing.T) {
	for _, raw := range []string{"0007", "7", "0301", "301", "0000-HellDen"} {
		if got := City(raw); got != "Thetford" {
			t.Errorf("City(%q) = %q,同一个订单簿要收敛成 Thetford", raw, got)
		}
		if !Tradeable(raw) {
			t.Errorf("Tradeable(%q) = false", raw)
		}
	}
}

// 走私贩网络 AODP 当成**一个**共享盘口:请求其中任何一个据点,
// 它会把整个清单展开返回。所以分开记没有意义。
func TestCity_走私贩据点收敛成一个网络(t *testing.T) {
	cases := []string{
		"2310", "BLACKBANK-2310", "0321@BLACKBANK-2310", "1012", "4300", "0008",
	}
	for _, raw := range cases {
		if got := City(raw); got != SmugglersNetwork {
			t.Errorf("City(%q) = %q,想要 %q", raw, got, SmugglersNetwork)
		}
	}
}

// 地狱门里开的单子算在对应主城的市场上。注意 AODP 源码自己带了条警告:
// 3003-HellDen 并进 Caerleon 市场而**不是**黑市。
func TestCity_地狱门并进主城市场(t *testing.T) {
	cases := map[string]string{
		"0000-HellDen": "Thetford", "3003-HellDen": "Caerleon",
		"5000-HellDen": "Brecilien",
	}
	for raw, want := range cases {
		if got := City(raw); got != want {
			t.Errorf("City(%q) = %q,想要 %q", raw, got, want)
		}
	}
	if City("3003-HellDen") == BlackMarket {
		t.Error("3003-HellDen 不该并进黑市,AODP 源码在这里专门留了警告")
	}
}

// 城市本体、银行这些不是交易地点,AODP 不认,原样放行。
// 合不上 AODP 只是少一路融合;错并到别的地方才是真的坏数据。
func TestCity_不是交易地点的原样放行(t *testing.T) {
	for _, raw := range []string{
		"0000", "1000", "5000", "5001", "0006", // 城市本体和银行
		"", "个人岛", "0901", "T5_CLOTH", "ISLAND-GUILD-0001b",
	} {
		if got := City(raw); got != raw {
			t.Errorf("City(%q) = %q,应该原样放行", raw, got)
		}
		if Tradeable(raw) {
			t.Errorf("Tradeable(%q) = true,它不是 AODP 认的交易地点", raw)
		}
	}
}

func TestTradeable_认得出交易地点(t *testing.T) {
	for _, raw := range []string{"0007", "3003", "3005", "5003", "0301",
		"3013-Auction2", "BLACKBANK-2310", "0000-HellDen"} {
		if !Tradeable(raw) {
			t.Errorf("Tradeable(%q) = false,它是交易地点", raw)
		}
	}
}

// 重复收敛要安全:新客户端将来自己做了收敛,服务端再过一遍也不能坏。
func TestCity_重复收敛不会坏(t *testing.T) {
	for _, c := range []string{"Thetford", "Martlock", "Fort Sterling",
		"Caerleon", "Brecilien", BlackMarket, SmugglersNetwork} {
		if got := City(City(c)); got != c {
			t.Errorf("City(City(%q)) = %q", c, got)
		}
	}
}

func TestDisplayName_各类地点都认(t *testing.T) {
	cases := map[string]string{
		"0000": "Thetford", "0007": "Thetford · 市场", "0301": "Thetford · 传送门",
		"3003": "Caerleon · 城内(黑市)", "3005": "Caerleon · 市场",
		"5000": "Brecilien", "5003": "Brecilien · 市场",
		"1012": "梅林的安息地",
	}
	for raw, want := range cases {
		if got := DisplayName(raw); got != want {
			t.Errorf("DisplayName(%q) = %q,想要 %q", raw, got, want)
		}
	}
	// 走私贩据点几十个,不逐个列名
	if got := DisplayName("BLACKBANK-2310"); !strings.HasPrefix(got, "走私贩网络") {
		t.Errorf("DisplayName(\"BLACKBANK-2310\") = %q", got)
	}
}

func TestDisplayName_认不出的原样返回(t *testing.T) {
	for _, raw := range []string{"", "0901", "个人岛", "T5_CLOTH"} {
		if got := DisplayName(raw); got != raw {
			t.Errorf("DisplayName(%q) = %q,认不出的应该原样返回", raw, got)
		}
	}
}

// op 197 点名的是城区本体(0000),不是市场 id(0007);黑市那个 cluster 的工作站归 Caerleon
func TestClusterCity_城区本体翻成城市(t *testing.T) {
	for zone, want := range map[string]string{
		"0000": "Thetford", "1000": "Lymhurst", "2000": "Bridgewatch", "3004": "Martlock",
		"4000": "Fort Sterling", "3003": "Caerleon", "5000": "Brecilien",
	} {
		if got, ok := ClusterCity(zone); !ok || got != want {
			t.Errorf("ClusterCity(%q) = %q, %v,想要 %q", zone, got, ok, want)
		}
	}
	// 传送门城是另一座镇子、另一批建筑,不并;野外原样
	for _, zone := range []string{"0301", "0007", "1205", ""} {
		if got, ok := ClusterCity(zone); ok || got != zone {
			t.Errorf("ClusterCity(%q) = %q, %v,应原样返回", zone, got, ok)
		}
	}
	// 和显示名口径一致:城区本体的显示名就是城市名(Caerleon 那格带了黑市注释,以它开头)
	for zone, city := range cityClusters {
		if got := DisplayName(zone); !strings.HasPrefix(got, city) {
			t.Errorf("DisplayName(%q) = %q,应以 %q 开头", zone, got, city)
		}
	}
}

// DisplayName 收了城市本体 id,City 不能因此跟着变。
// 两者对 0000 的回答故意不同:一个是给人看的,一个是做 key 的。
func TestDisplayName_不影响City(t *testing.T) {
	for _, raw := range []string{"0000", "5000", "0006"} {
		if DisplayName(raw) == raw {
			t.Fatalf("前提不成立:DisplayName(%q) 应该认得", raw)
		}
		if got := City(raw); got != raw {
			t.Errorf("City(%q) = %q,非交易地点必须原样放行", raw, got)
		}
	}
}

// 每个能收敛成城市名的 id,显示名都要以那个名字开头:
// 顶栏写的城市和扫描里用的城市对不上,成员会以为抓包串城了。
// 黑市是故意的例外——顶栏按"你在哪"写 Caerleon,key 是 Black Market。
func TestDisplayName_和City口径一致(t *testing.T) {
	for raw := range places {
		city := City(raw)
		if city == raw || city == BlackMarket || city == SmugglersNetwork {
			continue // 非交易地点,或故意不一致的那两个
		}
		if got := DisplayName(raw); !strings.HasPrefix(got, city) {
			t.Errorf("DisplayName(%q) = %q,应以 City 的结果 %q 开头", raw, got, city)
		}
	}
}
