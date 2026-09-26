package econ

import (
	"testing"

	"albion-guild/internal/conf"
)

// 拆出来的单腿系数必须和 Mode 里用的是同一份:四种模式的盈亏平衡
// 都能由两条腿的系数复算出来。
func TestLegMultipliersAgreeWithBreakeven(t *testing.T) {
	cfg := conf.Default().Economics
	for _, m := range Modes {
		want := m.Breakeven(cfg)
		got := BuyMul(m.Buy, cfg)/SellMul(m.Sell, cfg) - 1
		if !near(got, want) {
			t.Fatalf("%s:单腿复算 %.6f,Breakeven %.6f", m.Label(), got, want)
		}
	}
}

// 默认配置(premium 4% 税、2.5% 创建费、挂买单也收创建费)下的四个数。
func TestLegMultipliersDefaults(t *testing.T) {
	cfg := conf.Default().Economics
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"吃卖单买入", BuyMul(Taker, cfg), 1.0},
		{"挂买单收料", BuyMul(Maker, cfg), 1.025},
		{"吃买单卖出", SellMul(Taker, cfg), 0.96},
		{"挂卖单卖出", SellMul(Maker, cfg), 0.935},
	}
	for _, c := range cases {
		if !near(c.got, c.want) {
			t.Fatalf("%s 系数 %v,想要 %v", c.name, c.got, c.want)
		}
	}

	// 开关关掉时挂买单不收创建费——和 Mode 的行为一致
	cfg.BuyOrderSetupFee = false
	if got := BuyMul(Maker, cfg); !near(got, 1.0) {
		t.Fatalf("buy_order_setup_fee=false 时挂买单系数 %v,想要 1", got)
	}
}
