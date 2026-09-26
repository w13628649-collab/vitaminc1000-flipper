// Package refine 算精炼(原料 → 半成品)的成本、利润和最便宜的拿料路线。
//
// 纯函数:不连库、不看钟。价格、配方和参数都由调用方传进来,同一组输入
// 永远得到同一个结果,这样才能拿真实抓包做回归。
//
// 规则的出处和把握程度见 refine-design/design-rule_table.md。常量注释里的
// A1、C4 之类就是那张表的编号;✔ 是数据文件或官方原文直接读到的,
// ◐ 是推断或只有单一来源,? 是做成参数、等用户在游戏里核对的。
package refine

import (
	"fmt"
	"math"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
)

// ── C 回收率 ────────────────────────────────────────────────────────────

const (
	// BaseBonus 是皇家城(以及 Caerleon、Brecilien)的基础生产加成,百分点。
	// C2 ✔:craftingmodifiers.xml refiningbonus 0.18。个人岛上没有这一项(C7)
	BaseBonus = 18.0
	// SpecialtyBonus 是精炼专精城的加成,只加给那条线。
	// C3 ✔:craftingmodifiers.xml,Thetford 矿、Martlock 皮、Lymhurst 纤维、
	// Fort Sterling 木、Bridgewatch 石
	SpecialtyBonus = 40.0
	// FocusBonus 是开专注的加成。C4 ✔:gamedata.xml 1175 CraftingEfficiency 0.59
	FocusBonus = 59.0
)

// Bonus 是一次精炼能叠上的生产加成。几项加成直接相加(C6、C7),
// 不是连乘。
type Bonus struct {
	// Specialty:在这条线的精炼专精城,或绑定了专精城的个人岛
	Specialty bool
	// Focus:开专注
	Focus bool
	// Daily 是每日生产加成(C6 ◐),每天两个品类 +10 或 +20。没有接口能取,
	// 只能看游戏里的「活动」窗口手填,默认 0
	Daily float64
	// Island:个人岛。C7 ◐(高):岛上没有 18 的基础加成,但保留绑定城市的 +40
	// 专精加成(wiki RRR 表;craftingmodifiers 里只有 refiningbonus 带 islandvalue=0)。
	// 仓库 CLAUDE.md 写的「个人岛回收率为 0」和这个对不上,等用户同意再改那边
	Island bool
}

// Total 是加成合计,百分点。
func (b Bonus) Total() float64 {
	pb := b.Daily
	if !b.Island {
		pb += BaseBonus
	}
	if b.Specialty {
		pb += SpecialtyBonus
	}
	if b.Focus {
		pb += FocusBonus
	}
	return pb
}

// ReturnRate 是资源返还率(RRR)= PB / (100 + PB),PB 是加成合计(百分点)。
//
// C1 ✔:wiki Resource_return_rate(2026-06-01 版)原文写作 1 − 1/(1 + PB/100),
// 化简就是这个。C8 ✔:按期望值算,不模拟逐次的随机取整——wiki 原文也说
// 游戏是取整到期望的平均返还率。
//
// 由此:专精城 36.71%,专精城 + 专注 53.92%,再 +10 是 55.95%(表格的 0.558 是算错了),
// 非专精城 15.25% / 43.50%(C5)。
func ReturnRate(pb float64) float64 {
	if pb <= 0 {
		return 0
	}
	return pb / (100 + pb)
}

// ── D 专注 ──────────────────────────────────────────────────────────────

const (
	// FocusHalvingFCE:专注消耗效率(FCE)每多 10000 点,消耗减半。
	// D2 ✔:gamedata.xml 1174 ActionFocus costreductionconstant=1.00695555005672,
	// 这个数的 100 次方正好是 2,即每 100 点 FCE 除一次它、每 10000 点减半
	FocusHalvingFCE = 10000.0
	// MaxFCE 是单个 tier 的满专精:D3 ✔ achievements.xml 23179–23275,每个精炼节点
	// 每级给整条线 +30、给本 tier 另加 +250,每节点 100 级,
	// 本 tier 满级 25000 + 五个节点满级 15000 = 40000,消耗降到 1/16
	MaxFCE = 40000.0
)

// FocusCost 是精炼一次实际扣的专注 = 基础 × 0.5^(FCE/10000)。
//
// 基础值来自配方数据(D1,T8.4 是 4714),FCE 由用户录入。专注按次扣,
// 和返还无关(D5)。游戏界面上显示的是整数,取整方式没核对,这里保留小数。
func FocusCost(base, fce float64) float64 {
	return base * math.Pow(0.5, fce/FocusHalvingFCE)
}

// CheckFCE 校验用户录入的 FCE。超出 [0, 40000] 说明录错了,不静默截断。
func CheckFCE(fce float64) error {
	if !(fce >= 0 && fce <= MaxFCE) {
		return fmt.Errorf("专注消耗效率(FCE)要在 [0, %g] 内,得到 %g", MaxFCE, fce)
	}
	return nil
}

// ── E 工作站使用费 ──────────────────────────────────────────────────────

const (
	// ItemValueToNutrition:每件产物消耗的营养 = 物品价值(IV)× 0.1125。
	// E1 ✔:gamedata.xml 1166;wiki Building;论坛 157229(2021-11-09,官方)
	ItemValueToNutrition = 0.1125
	// FreeCraftingMaxTier:产物 T2 及以下不收使用费。E3 ✔:gamedata.xml 1171 FreeCrafting maxTier=2
	FreeCraftingMaxTier = 2
	// MaxStationFeePer100 是站主能设的使用费上限(每 100 营养)。
	// E3 ✔:gamedata.xml 1172 maxuseagefee=1000;Foundations(2024-03)从 10000 降下来(论坛 194320)
	MaxStationFeePer100 = 1000.0
)

// usageFee 是按营养计的使用费:IV × 0.1125 × 费率/100。精炼站费和转化的
// 主人费用是同一个公式,只是基数取的物品不同。
func usageFee(itemValue, feePer100 float64) float64 {
	return itemValue * ItemValueToNutrition * feePer100 / 100
}

// StationFee 是精炼一次的工作站使用费 = 产物 IV × 0.1125 × 费率/100(E1)。
//
// 按次收,返还不减免(E4 ◐)。IV 取产物的,来自配方数据(E2:T4 条 16,每 +1 tier
// 或 +1 附魔 ×2,T8.4 条 4096)。费率是站主自定的(E5),各城各站不同,
// 默认的 500 是表格作者用的数,没有核实。
func StationFee(productIV float64, productTier int, feePer100 float64) float64 {
	if productTier <= FreeCraftingMaxTier {
		return 0
	}
	return usageFee(productIV, feePer100)
}

// CheckStationFee 校验使用费率:游戏里设不出负数,也设不过 1000(E3)。
func CheckStationFee(feePer100 float64) error {
	if !(feePer100 >= 0 && feePer100 <= MaxStationFeePer100) {
		return fmt.Errorf("工作站使用费要在 [0, %g] 银/100 营养内,得到 %g", MaxStationFeePer100, feePer100)
	}
	return nil
}

// ── B 转化 ──────────────────────────────────────────────────────────────

// TransmuteCost 是转化一次的实付 = 基础银币 × k + 产物 IV × 0.1125 × 费率/100。
//
//   - 基础银币来自配方数据(B4 ✔),不在这里推算:数不规整(16,500、51,975、155,925……),
//     而且随版本改过
//   - k(B5 ?)是游戏在基础价之上乘的系数,来源不明、随时间变过:2023-10 约 1.00
//     (论坛 187902),2025-11 西服约 1.156(论坛 208640),亚服表格 2026-07~09 的
//     J/K 列反推 1.154。只能做成参数,由用户看游戏里的转化明细核对
//   - 后一项是「主人费用」(B6 ◐):按**产物**的 IV 算,公式和精炼站费一样。
//     论坛 208640 两次石头实测差 4 银,正好是 T5_ROCK IV 5.34 × 0.1125 × 6.18
//   - 全局折扣(B7 ◐,金价低于 5000 才生效,亚服现在 13,554)不单独建模,归进 k
//   - 转化不用专注、没有返还(B3 ✔)
func TransmuteCost(baseSilver int64, k, productIV, feePer100 float64) float64 {
	return float64(baseSilver)*k + usageFee(productIV, feePer100)
}

// CheckTransmuteFactor 校验 k。k 是乘在基础价上的系数,见过的实测都在 1.0~1.2,
// 小于 1 或者离谱地大多半是录错(比如录成了百分数)。
func CheckTransmuteFactor(k float64) error {
	if !(k >= 0.5 && k <= 3) {
		return fmt.Errorf("转化实付系数 k 要在 [0.5, 3] 内(见过的实测 1.00~1.16),得到 %g", k)
	}
	return nil
}

// ── F 市场 ──────────────────────────────────────────────────────────────

// Market 是四种成交方式的费用系数。全部取自 econ,税费逻辑只有那一份(F1–F3 ✔)。
//
// 创建费 2.5% 在创建或修改买单、卖单时当场扣,撤单不退(F1);交易税 8%,
// premium 减半为 4%,只由卖方付(F2)。所以吃卖单的买方一分钱不付——表格按
// 卖一价买料不乘 1.025 并没有错(F3′),只有挂买单收料才乘。
type Market struct {
	TakeAsk float64 // 吃卖单买入:成本 = 价 × 1.000
	PostBid float64 // 挂买单收料:成本 = 价 × 1.025(不成交也白付)
	TakeBid float64 // 吃买单卖出:到手 = 价 × 0.96
	PostAsk float64 // 挂卖单卖出:到手 = 价 × 0.935
}

// MarketFees 按经济学配置算出四个系数。精炼页可以让用户改税率(F7 ◐:
// 有装备能减拍卖税),改的是传进来的 cfg,算法不另写一份。
func MarketFees(cfg conf.Economics) Market {
	return Market{
		TakeAsk: econ.BuyMul(econ.Taker, cfg),
		PostBid: econ.BuyMul(econ.Maker, cfg),
		TakeBid: econ.SellMul(econ.Taker, cfg),
		PostAsk: econ.SellMul(econ.Maker, cfg),
	}
}

// ── 待用户核对的参数 ────────────────────────────────────────────────────
//
// 默认值照设计(design-questions.md),计算结果里要把它们列进
// params_used.unverified,不能让人以为是核实过的数。

const (
	// DefaultTransmuteFactor:k,B5 ?。亚服表格反推 1.154(160 格里 156 格残差不超过 6.4 银)
	DefaultTransmuteFactor = 1.154
	// DefaultStationFeePer100:精炼站使用费,E5 ?。表格作者用的数
	DefaultStationFeePer100 = 500.0
	// DefaultTransmuteStationFeePer100:转化的主人费率,B6 ◐。同上
	DefaultTransmuteStationFeePer100 = 500.0
	// DefaultFCE:精炼专精,D3。用户是火法 + 纤维采集,矿/皮专精多半是 0
	DefaultFCE = 0.0
	// DefaultUnlockedTier:已解锁精炼到 T 几,A10 ◐。T8 节点第 1 级要 2,795,000 声望,
	// 默认按已解锁算;没解锁的 tier 只能买、不能自炼。转化不用解锁(原矿带 unlockedtocraft)
	DefaultUnlockedTier = 8
	// DefaultDailyBonus:每日生产加成,C6 ◐
	DefaultDailyBonus = 0.0
	// DefaultHeartReturns:精炼用的心是否也返还,A7 ?。只能从「没标 maxreturnamount」推断。
	// 对 .4 链没有影响(.4 没有心配方)
	DefaultHeartReturns = true
)

// Unverified 是一条没核实、做成了参数的规则。
type Unverified struct {
	Key   string `json:"key"`   // 参数名,和请求体里的字段对得上
	Rule  string `json:"rule"`  // 规则表编号
	Check string `json:"check"` // 怎么在游戏里核对
}

// UnverifiedParams 是全部待核对的参数,顺序按对结论的影响从大到小。
var UnverifiedParams = []Unverified{
	{"fees.transmute_factor", "B5",
		"T8 冶炼厂「转化」分页选 8.3→8.4、数量 1,记下明细里的基本花费 / 全局折扣 / 主人费用 / 地区税费,实付 ÷ 基础价就是 k"},
	{"focus.fce", "D3",
		"命运之轮里 T4~T8 五个精炼节点的等级,或在站里选中产物看显示的专注消耗"},
	{"focus.unlocked_tier", "A10", "命运之轮 T8 精炼节点是否已解锁"},
	{"fees.station_per100", "E5", "城内按 N 打开地图,悬停在精炼站上看每 100 营养多少银"},
	{"fees.transmute_station_per100", "B6", "同上,转化和精炼在同一个站"},
	{"rrr.daily_bonus", "C6", "游戏内「活动」窗口里今天的每日生产加成落在哪些品类、+10 还是 +20"},
	{"heart_returns", "A7", "用心、开专注精炼一小批,看有没有返还心"},
}
