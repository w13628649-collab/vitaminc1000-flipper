// Package conf 放所有阈值。集中在一处,方便按实盘反馈调。
package conf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerBaseURL 是三个大区的 AODP 地址。
var ServerBaseURL = map[string]string{
	"east":   "https://east.albion-online-data.com",
	"west":   "https://west.albion-online-data.com",
	"europe": "https://europe.albion-online-data.com",
}

// Freshness 是数据新鲜度。AODP 是众包数据,亚服覆盖率差,
// 这是第一道也是最重要的闸门。
type Freshness struct {
	// MaxHours 超过这个时间的价格直接丢弃。
	MaxHours float64 `yaml:"max_hours"`
	// HighConfidenceHours 双边数据都在这个时间内 → confidence=high。
	HighConfidenceHours float64 `yaml:"high_confidence_hours"`
}

// Filters 是 troll 过滤阈值。
type Filters struct {
	// DeviationMin/Max 是 价格/7 日均价 的合理区间,区间外视为 troll 挂单。
	DeviationMin float64 `yaml:"deviation_min"`
	DeviationMax float64 `yaml:"deviation_max"`

	// MinDailyVolumeSilver 是日白银流水下限。低于这个量的物品吃不下也出不掉。
	MinDailyVolumeSilver float64 `yaml:"min_daily_volume_silver"`

	// RejectCrossedBook:买单价 >= 卖单价。真实市场不可能持续存在这种状态,
	// 出现说明两侧快照来自不同时间,是陈旧数据的强信号。
	RejectCrossedBook bool `yaml:"reject_crossed_book"`

	// MaxHistoryGapDays 成交历史最后一天距今超过这个天数 → 该物品历史不可用。
	MaxHistoryGapDays float64 `yaml:"max_history_gap_days"`

	// MinDaysWithData7d 7 日窗口里至少要有这么多天有成交数据。
	MinDaysWithData7d int `yaml:"min_days_with_data_7d"`

	// MaxMargin 是单笔毛利率上限。第五层过滤,SPEC 的四层盖不住这个缺口:
	// 买价偏离 0.45、卖价偏离 2.4 各自都"合规",组合起来却是 5 倍价差——
	// 两边同时是 troll 挂单。真实的同城价差极少超过 100%。
	MaxMargin float64 `yaml:"max_margin"`

	// ── 深度判据:只对来自自建抓包的那一边生效(AODP 给不了件数)──
	// 纯价格过滤挡不住盈亏平衡(ask/bid≈1.096)到 max_margin(≈2.19)之间那一整段,
	// 真机会和假机会都在里面,只有件数分得开。下面的件数阈值都没校准过,
	// 照 CLAUDE.md 里那几组实测拍的,等记账数据攒够再调。

	// NearPct 是"最优价附近"的窗口:卖一往上 / 买一往下这么多以内的档算近价。
	// 判据看近价件数而不是总件数:挂单簿底下永远躺着一堆 1 银的占位单
	NearPct float64 `yaml:"near_pct"`
	// MinBookQty:挂卖腿依托的卖方近价件数低于它 → thin_book 拒绝。0 = 关闭。
	// **口径和 Python 版不同**:Python 里是"卖一那一档的件数",这里是
	// "卖一往上 near_pct 以内的件数"。T5_METALBAR_LEVEL4@4 卖一只有 4 件、
	// 近价有 230 件,按旧口径会被误降级
	MinBookQty int64 `yaml:"min_book_qty"`
	// ThinBookEdgeQty:卖方近价件数低于它 → 降为 low 置信,不拒
	ThinBookEdgeQty int64 `yaml:"thin_book_edge_qty"`
	// MinBidDepth:挂买腿依托的买方近价件数低于它 → no_bid_side 拒绝,
	// 挂买单进去大概率收不到货,还白付创建费。0 = 关闭。
	// 口径同样从 Python 的"买方总件数"改成近价件数。默认 20 能拦下
	// T6_METALBAR_LEVEL4@4 的 11 件,放过 T4@4 的 60 件和 T5@4 的 40 件
	MinBidDepth int64 `yaml:"min_bid_depth"`
	// ThinBidEdgeQty:买方近价件数低于它 → 降为 low 置信
	ThinBidEdgeQty int64 `yaml:"thin_bid_edge_qty"`
	// BidCliffEdgePct:买方近价窗口外第一档的落差(depth.Support.GapAfterNear)
	// 达到它 → 降为 low。顶上一小撮吃完就断崖,说明没人在持续收。0 = 关闭
	BidCliffEdgePct float64 `yaml:"bid_cliff_edge_pct"`
	// MaxSpreadPct:卖一/买一 − 1 超过它 → wide_spread 拒绝,不分数据来源。0 = 关闭
	MaxSpreadPct float64 `yaml:"max_spread_pct"`
}

// Capture 是自建抓包盘口怎么参与扫描。键名沿用 Python 版能对上的那几个。
//
// 上级目录旧 Python 版的 config.yaml 里也写着 capture.enabled: true,和这里的
// 默认值一致;它的 capture.max_age_hours、freshness.capture_max_hours 这里不认,
// 加载时会告警并忽略。
type Capture struct {
	// Enabled 是融合总开关。false 时扫描就是纯 AODP,抓到的数据照样入库,
	// 只是不参与机会板
	Enabled bool `yaml:"enabled"`
	// MaxHours 是扫描读抓包的窗口,0 表示跟 freshness.max_hours 一致。
	// 和服务端的 -fresh(默认 30m)是两回事:那个只管 WS、/api/book、/api/quotes
	MaxHours float64 `yaml:"max_hours"`
	// DepthMaxHours 是深度的可信窗口。幽灵单规则只能验证"最近一眼"覆盖到的
	// 价段,更旧的档可能早就没了,不拿来做深度判据
	DepthMaxHours float64 `yaml:"depth_max_hours"`
	// SnapshotSlackSeconds 是多宽的时间范围算"同一眼"(book.Build 的最近一轮)。
	// 扫描、WS 报价和查价页共用这一个数。设成 ≥ 窗口就等于关掉残单剔除,
	// 是规则前提不成立时的逃生口。
	//
	// 默认 120s,不是查价页以前的 5 分钟:
	//   - 要盖住的只是一次响应里的时间差:客户端 3s 攒批、服务端 2s flush,加上同一页
	//     几秒后被详情页又看一眼。实测 293 个盘口里 283 个所有单都在 1 分钟内,120s 留一倍余量
	//   - 翻页翻得慢(第 2 页比第 1 页晚到超过 slack)已经由续页识别处理(第 1 页是满页、
	//     第 2 页接在它整页最差价之后,就整页保留),不用再靠放宽 slack 兜。
	//     以前扫描的 SQL 规则认不得续页,才会在这里出错
	//   - 放到 5 分钟的代价:两眼隔 2~5 分钟时被并成一轮,中间被买走的最优单剔不掉,
	//     机会页和 WS 报价会把它多报几分钟(审查 E 轮就是 125s 后的第二眼)
	SnapshotSlackSeconds float64 `yaml:"snapshot_slack_seconds"`
	// PreferSlackMinutes:抓包比 AODP 旧不超过这么多,仍然用抓包
	PreferSlackMinutes float64 `yaml:"prefer_slack_minutes"`
	// BookLevels 是每个盘口最多读多少档
	BookLevels int `yaml:"book_levels"`
	// MaxExtraItems 是扫描最多并入多少个"配置清单外、但抓包窗口里有挂单"的物品。
	// 成员翻市场翻的是自己关心的货(T7/T8 精炼材料、附魔资源、良好~不凡品质的装备……),
	// 配置清单和 qualities 盖不全;不并进来的话抓得再多机会板也看不见。这些也要向 AODP
	// 拉价和历史(troll 过滤和成交量离不开),所以要有上限:超出时按抓包件数从多到少截断。
	//
	// **名额按物品计,不按 (物品, 品质)**:并进来的物品带上它抓到的全部品质,清单物品抓到的
	// 别的品质不占名额。成本在 id 上——多一个物品就多一个 id 塞进 AODP 的 URL,多一档品质
	// 只是 qualities 参数里多个数;一个物品最多五档,组合数封顶在 5 × (清单 + 上限)。
	// 0 = 抓包不扩扫描集合(物品和品质都不扩),扫描就是配置清单 × qualities
	MaxExtraItems int `yaml:"max_extra_items"`
	// ReevalSeconds 是两次 AODP 全量扫描之间,多久用缓存的 AODP 快照 + 最新抓包
	// 重算一次机会板。全量扫描 30 分钟一次(要打 AODP、受配额限制),成员刚翻完
	// 市场却要等半小时才看得到,抓包"实时"这个优势就没了。重算只读库、不打 AODP。
	// 0 = 关闭,只随全量扫描刷新
	ReevalSeconds float64 `yaml:"reeval_seconds"`
}

// Economics 是交易经济学。默认值对应亚服 + 高级会员。
type Economics struct {
	// MarketTax 成交时的市场税。premium 4%,非会员 8%。
	MarketTax float64 `yaml:"market_tax"`
	// SetupFee 挂单手续费,不退。
	SetupFee float64 `yaml:"setup_fee"`
	// BuyOrderSetupFee 挂买单是否也收 SetupFee。**已确认收**:游戏本地化文本里
	// 买单和卖单都有 MARKETPLACE_*ORDER_LABEL_SETUP_COST,中文叫"创建费"。
	BuyOrderSetupFee bool `yaml:"buy_order_setup_fee"`
	// OutbidSilver 挂买单要压过现有最高买单才排得到队首。
	OutbidSilver int64 `yaml:"outbid_silver"`
	// UndercutSilver 挂卖单要低于现有最低卖单才排得到队首。
	UndercutSilver int64 `yaml:"undercut_silver"`
}

// RoundTripFriction 是一轮买入 + 卖出的总摩擦比例(不含 outbid/undercut)。
func (e Economics) RoundTripFriction() float64 {
	buySide := 0.0
	if e.BuyOrderSetupFee {
		buySide = e.SetupFee
	}
	return e.MarketTax + e.SetupFee + buySide
}

// Sizing 是吃单量估算。倒爷的真正约束是"能吃下市场多大比例而不砸价"。
type Sizing struct {
	// AbsorbRatio 日成交量里我敢吃的比例。
	// 第二阶段用实盘成交率反过来校准这个系数。
	AbsorbRatio float64 `yaml:"absorb_ratio"`
	// BaselineDays troll 过滤的基准均价取最近几天。
	BaselineDays int `yaml:"baseline_days"`
	// HistoryDays 拉多少天历史。同时给出 7 日和 30 日口径。
	HistoryDays int `yaml:"history_days"`

	// FillHours 是一条**挂单**腿平均等多久才成交。吃单腿不用等。
	// 同城跨城共用这一个数,否则两边周转口径不一致,组合排名就没意义了
	FillHours float64 `yaml:"fill_hours"`
	// TravelHours 是跨城单程路上的时间,同城为 0。
	TravelHours float64 `yaml:"travel_hours"`
	// BrecilienTravelHours 是一端是 Brecilien 的路线单程要多久,0 = 和 travel_hours 一样。
	//
	// Brecilien 在迷雾里,进出都要穿过迷雾,按比两个皇家城市之间走得久算,所以单列。
	// **默认的 1.0 小时是估的,没实测过**(具体路线和耗时也没核实):按"皇家城市
	// 之间 0.5 小时的两倍"拍,跑过几趟之后按实际改
	BrecilienTravelHours float64 `yaml:"brecilien_travel_hours"`
}

// Brecilien 是迷雾里那座城在 AODP 和扫描器里的名字。抓包的 5003 已经在
// world.City 收敛成这个名字,AODP 的 locations 参数也认它。
const Brecilien = "Brecilien"

// ViaMists 说明这两座城之间要不要穿过迷雾:任何一端是 Brecilien 都要。
// 迷雾里能被劫,工具不把被劫概率建模成数字,只打风险标记、限置信度。
func ViaMists(a, b string) bool { return a == Brecilien || b == Brecilien }

// TravelHoursBetween 是 a、b 两城之间单程路上的时间。
func (s Sizing) TravelHoursBetween(a, b string) float64 {
	if ViaMists(a, b) && s.BrecilienTravelHours > 0 {
		return s.BrecilienTravelHours
	}
	return s.TravelHours
}

// API 是 AODP 的调用约束。
type API struct {
	// MaxURLLength AODP 上限 4096,留余量。超长自动分批。
	MaxURLLength int `yaml:"max_url_length"`
	// RatePerMinute 官方 180/分钟,留余量。
	RatePerMinute int `yaml:"rate_per_minute"`
	// RatePer5Min 官方 300/5 分钟。这条比每分钟那条更紧(等效 56/分钟)。
	RatePer5Min int `yaml:"rate_per_5min"`

	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	MaxRetries     int     `yaml:"max_retries"`
}

// Items 是物品清单。用模式展开,不手写 ID。
type Items struct {
	// Patterns 支持 brace 展开:T{4,5,6}_{CLOTH,METALBAR} → 6 个 ID。
	Patterns []string `yaml:"patterns"`
	// Exclude 展开后要剔除的具体 ID。
	Exclude []string `yaml:"exclude"`
}

type Config struct {
	Server    string    `yaml:"server"`
	Cities    []string  `yaml:"cities"`
	Capital   int64     `yaml:"capital"`
	Qualities []int     `yaml:"qualities"`
	Freshness Freshness `yaml:"freshness"`
	Capture   Capture   `yaml:"capture"`
	Filters   Filters   `yaml:"filters"`
	Economics Economics `yaml:"economics"`
	Sizing    Sizing    `yaml:"sizing"`
	API       API       `yaml:"api"`
	Items     Items     `yaml:"items"`
}

// DefaultCities 是五个皇家城市、凯尔利恩和 Brecilien。黑区市场不进来——
// 那里的价格好看,但货运不出来。
//
// Brecilien 同城机会照常算;跨城路线也出,但一端是它的路线要穿迷雾:
// 运输时间用 sizing.brecilien_travel_hours,路线带 risk_tags=["mists"]、
// 置信度最高 medium(见 arb)
var DefaultCities = []string{
	"Thetford", "Fort Sterling", "Lymhurst", "Martlock", "Bridgewatch", "Caerleon", Brecilien,
}

// DefaultPatterns 是起步的物品清单:只碰高流动性、低单价的品类。
// 单价高 = 占用本金多 = 周转慢。
var DefaultPatterns = []string{
	// 精炼材料:全游戏成交量最大的品类
	"T{4,5,6}_{CLOTH,METALBAR,PLANKS,LEATHER,STONEBLOCK}",
	// 原料:用于对比"卖原料 vs 卖半成品"
	"T{4,5,6}_{FIBER,ORE,WOOD,HIDE,ROCK}",
	// 布甲三件套(学者/法师/神秘)
	"T{4,5,6}_{HEAD,ARMOR,SHOES}_CLOTH_{SET1,SET2,SET3}",
	// 火法杖系
	"T{4,5,6}_{MAIN_FIRESTAFF,2H_FIRESTAFF,2H_INFERNOSTAFF}",
}

// Default 是全套默认值。YAML 里没写的字段用这里的。
// 不给配置文件也能直接跑起来。
func Default() Config {
	return Config{
		Server:    "east",
		Cities:    append([]string(nil), DefaultCities...),
		Capital:   10_000_000,
		Qualities: []int{1},
		Freshness: Freshness{MaxHours: 6.0, HighConfidenceHours: 2.0},
		Capture: Capture{
			// 默认开:深度闸门(screen.LegGate)已经接上,抓包价上榜之前要先过件数判据。
			// 要退回纯 AODP,在配置里写 capture.enabled: false
			Enabled:              true,
			MaxHours:             0,
			DepthMaxHours:        2.0,
			SnapshotSlackSeconds: 120,
			PreferSlackMinutes:   10,
			BookLevels:           128,
			// id 批量塞进 URL:300 个 id 约 6.6k 字符,按 3500 的 URL 预算每个端点
			// 多两三次请求,prices + history 合计多 4~6 次;抓到别的品质的物品另成一组
			// (scan.fetchAODP),至多再多两次。280 次/5 分钟的配额里不算什么
			MaxExtraItems: 300,
			ReevalSeconds: 60,
		},
		Filters: Filters{
			DeviationMin:         0.4,
			DeviationMax:         2.5,
			MinDailyVolumeSilver: 500_000,
			RejectCrossedBook:    true,
			MaxHistoryGapDays:    3,
			MinDaysWithData7d:    3,
			MaxMargin:            1.0,
			NearPct:              0.05,
			MinBookQty:           3,
			ThinBookEdgeQty:      10,
			MinBidDepth:          20,
			ThinBidEdgeQty:       100,
			BidCliffEdgePct:      0.20,
			MaxSpreadPct:         0,
		},
		Economics: Economics{
			MarketTax:        0.04,
			SetupFee:         0.025,
			BuyOrderSetupFee: true,
			OutbidSilver:     1,
			UndercutSilver:   1,
		},
		Sizing: Sizing{
			AbsorbRatio: 0.20, BaselineDays: 7, HistoryDays: 30,
			FillHours: 4.0, TravelHours: 0.5,
			// 估的,没实测:经迷雾按皇家城市之间的两倍算,见 BrecilienTravelHours
			BrecilienTravelHours: 1.0,
		},
		Items: Items{Patterns: append([]string(nil), DefaultPatterns...)},
		API: API{
			MaxURLLength:   3500,
			RatePerMinute:  170,
			RatePer5Min:    280,
			TimeoutSeconds: 30,
			MaxRetries:     4,
		},
	}
}

func (c Config) BaseURL() string { return ServerBaseURL[c.Server] }

// CaptureWindow 是扫描读抓包的时间窗口。capture.max_hours 为 0 时
// 跟 freshness.max_hours 走:抓包和 AODP 用同一条过期线,两边才好比
func (c Config) CaptureWindow() time.Duration {
	h := c.Capture.MaxHours
	if h == 0 {
		h = c.Freshness.MaxHours
	}
	return hours(h)
}

// DepthWindow 是深度判据只信多近的档。
func (c Config) DepthWindow() time.Duration { return hours(c.Capture.DepthMaxHours) }

// SnapshotSlack 是"同一眼"的时间宽容度。
func (c Config) SnapshotSlack() time.Duration {
	return time.Duration(c.Capture.SnapshotSlackSeconds * float64(time.Second))
}

// PreferSlack 是抓包比 AODP 旧多少以内仍优先用抓包。
func (c Config) PreferSlack() time.Duration {
	return time.Duration(c.Capture.PreferSlackMinutes * float64(time.Minute))
}

// ReevalInterval 是用缓存快照重算机会板的间隔,0 表示不重算。
func (c Config) ReevalInterval() time.Duration {
	return time.Duration(c.Capture.ReevalSeconds * float64(time.Second))
}

func hours(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }

func (c Config) Validate() error {
	if _, ok := ServerBaseURL[c.Server]; !ok {
		return fmt.Errorf("未知 server: %s", c.Server)
	}
	if len(c.Cities) == 0 {
		return fmt.Errorf("cities 不能为空")
	}
	// qualities 是清单物品评估的品质,也是扫描拉 AODP 时分组的基准(scan.fetchAODP):
	// 空着的话清单物品一档都不评估;0、6 这种 AODP 不认,只会白占请求
	if len(c.Qualities) == 0 {
		return fmt.Errorf("qualities 不能为空(1 普通 / 2 良好 / 3 优秀 / 4 杰出 / 5 不凡)")
	}
	for _, q := range c.Qualities {
		if q < 1 || q > 5 {
			return fmt.Errorf("qualities 只能是 1..5,得到 %v", c.Qualities)
		}
	}

	f, cp := c.Filters, c.Capture
	if !(f.NearPct > 0 && f.NearPct <= 0.5) {
		return fmt.Errorf("filters.near_pct 要在 (0, 0.5] 内,得到 %g", f.NearPct)
	}
	if cp.BookLevels < 1 {
		return fmt.Errorf("capture.book_levels 至少为 1,得到 %d", cp.BookLevels)
	}
	// 件数阈值不能超过 book_levels。阶梯在 book_levels 档处被截断时,
	// 如果读到的档全在近价窗口内,近价件数至少是 book_levels(每档至少 1 件);
	// 阈值不超过它,截断就永远不会造成误拒
	for _, q := range []struct {
		key string
		v   int64
	}{
		{"min_book_qty", f.MinBookQty},
		{"thin_book_edge_qty", f.ThinBookEdgeQty},
		{"min_bid_depth", f.MinBidDepth},
		{"thin_bid_edge_qty", f.ThinBidEdgeQty},
	} {
		if q.v > int64(cp.BookLevels) {
			return fmt.Errorf("filters.%s=%d 大于 capture.book_levels=%d:阶梯截断后可能误拒,"+
				"要么调低阈值,要么调高 book_levels", q.key, q.v, cp.BookLevels)
		}
	}
	if cp.MaxHours < 0 || cp.MaxHours > c.Freshness.MaxHours {
		return fmt.Errorf("capture.max_hours 要在 [0, freshness.max_hours=%g] 内,得到 %g:"+
			"比 AODP 的过期线还宽,抓包就能拿更旧的价盖掉 AODP", c.Freshness.MaxHours, cp.MaxHours)
	}
	if !(cp.DepthMaxHours > 0) || hours(cp.DepthMaxHours) > c.CaptureWindow() {
		return fmt.Errorf("capture.depth_max_hours 要在 (0, 抓包窗口 %v] 内,得到 %g",
			c.CaptureWindow(), cp.DepthMaxHours)
	}
	if cp.SnapshotSlackSeconds < 0 {
		return fmt.Errorf("capture.snapshot_slack_seconds 不能为负,得到 %g", cp.SnapshotSlackSeconds)
	}
	if cp.PreferSlackMinutes < 0 {
		return fmt.Errorf("capture.prefer_slack_minutes 不能为负,得到 %g", cp.PreferSlackMinutes)
	}
	if cp.MaxExtraItems < 0 {
		return fmt.Errorf("capture.max_extra_items 不能为负(0 = 不并入抓包物品),得到 %d", cp.MaxExtraItems)
	}
	if cp.ReevalSeconds < 0 {
		return fmt.Errorf("capture.reeval_seconds 不能为负(0 = 不重算),得到 %g", cp.ReevalSeconds)
	}
	// 路上时间为负会让一趟来回比挂单等待还短,周转次数凭空变多
	if c.Sizing.TravelHours < 0 {
		return fmt.Errorf("sizing.travel_hours 不能为负,得到 %g", c.Sizing.TravelHours)
	}
	if c.Sizing.BrecilienTravelHours < 0 {
		return fmt.Errorf("sizing.brecilien_travel_hours 不能为负(0 = 和 travel_hours 一样),得到 %g",
			c.Sizing.BrecilienTravelHours)
	}
	return nil
}

// Load 读 YAML。先铺默认值再覆盖,所以配置文件只写要改的那几项就行。
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("解析 %s: %w", path, err)
	}
	warnUnknownKeys(path, raw)
	return cfg, cfg.Validate()
}

// warnUnknownKeys 用严格模式再解一遍,只为把写错/不认识的键报出来。
//
// 宽松解码会静默吞掉未知键:min_bid_dpeth 这种笔误,阈值就悄悄用了默认值,
// 实盘里根本察觉不到。但也不能直接拒绝加载——旧 Python 版的配置里有一堆
// Go 不认的键(data_dir、capture.max_age_hours……),拿来就用是常见操作。
// 所以只告警,加载成败的语义不变。解到临时副本里,不影响真正的结果
func warnUnknownKeys(path string, raw []byte) {
	probe := Default()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&probe); err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("配置里有不认识的键", "path", path, "err", err)
	}
}
