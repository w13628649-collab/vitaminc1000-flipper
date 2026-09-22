// Package conf 放所有阈值。集中在一处,方便按实盘反馈调。
package conf

import (
	"fmt"
	"os"

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
	Filters   Filters   `yaml:"filters"`
	Economics Economics `yaml:"economics"`
	Sizing    Sizing    `yaml:"sizing"`
	API       API       `yaml:"api"`
	Items     Items     `yaml:"items"`
}

// DefaultCities 是五个皇家城市加凯尔利恩。黑区市场不进来——
// 那里的价格好看,但货运不出来。
var DefaultCities = []string{
	"Thetford", "Fort Sterling", "Lymhurst", "Martlock", "Bridgewatch", "Caerleon",
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
		Filters: Filters{
			DeviationMin:         0.4,
			DeviationMax:         2.5,
			MinDailyVolumeSilver: 500_000,
			RejectCrossedBook:    true,
			MaxHistoryGapDays:    3,
			MinDaysWithData7d:    3,
			MaxMargin:            1.0,
		},
		Economics: Economics{
			MarketTax:        0.04,
			SetupFee:         0.025,
			BuyOrderSetupFee: true,
			OutbidSilver:     1,
			UndercutSilver:   1,
		},
		Sizing: Sizing{AbsorbRatio: 0.20, BaselineDays: 7, HistoryDays: 30},
		Items:  Items{Patterns: append([]string(nil), DefaultPatterns...)},
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

func (c Config) Validate() error {
	if _, ok := ServerBaseURL[c.Server]; !ok {
		return fmt.Errorf("未知 server: %s", c.Server)
	}
	if len(c.Cities) == 0 {
		return fmt.Errorf("cities 不能为空")
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
	return cfg, cfg.Validate()
}
