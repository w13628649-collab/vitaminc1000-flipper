package conf

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 默认值就是文档里那张表。改默认值等于改实盘行为,得有测试拦着
func TestDefault_深度阈值与抓包段(t *testing.T) {
	c := Default()
	if c.Capture != (Capture{
		Enabled: false, MaxHours: 0, DepthMaxHours: 2.0,
		SnapshotSlackSeconds: 120, PreferSlackMinutes: 10, BookLevels: 128,
		MaxExtraItems: 300, ReevalSeconds: 60,
	}) {
		t.Fatalf("capture 默认值不对: %+v", c.Capture)
	}
	f := c.Filters
	if f.NearPct != 0.05 || f.MinBookQty != 3 || f.ThinBookEdgeQty != 10 ||
		f.MinBidDepth != 20 || f.ThinBidEdgeQty != 100 || f.BidCliffEdgePct != 0.20 || f.MaxSpreadPct != 0 {
		t.Fatalf("深度阈值默认值不对: %+v", f)
	}
	if got := c.DepthWindow(); got != 2*time.Hour {
		t.Fatalf("DepthWindow 应为 2h,得到 %v", got)
	}
	if got := c.SnapshotSlack(); got != 120*time.Second {
		t.Fatalf("SnapshotSlack 应为 120s,得到 %v", got)
	}
	if got := c.PreferSlack(); got != 10*time.Minute {
		t.Fatalf("PreferSlack 应为 10m,得到 %v", got)
	}
	if got := c.ReevalInterval(); got != time.Minute {
		t.Fatalf("ReevalInterval 应为 1m,得到 %v", got)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("默认值自己要能过校验: %v", err)
	}
}

// 配置文件只写要改的那几项,其余必须保持默认——不能因为写了 filters 段
// 就把同段没写的阈值清零(清零 = 关掉那条判据)
func TestLoad_只写两项阈值其余保持默认(t *testing.T) {
	c, err := Load(writeYAML(t, "filters: {min_book_qty: 5, min_bid_depth: 30}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Filters.MinBookQty != 5 || c.Filters.MinBidDepth != 30 {
		t.Fatalf("写了的两项没生效: %+v", c.Filters)
	}
	want := Default()
	want.Filters.MinBookQty, want.Filters.MinBidDepth = 5, 30
	if c.Filters != want.Filters || c.Capture != want.Capture {
		t.Fatalf("没写的项应保持默认\n得到 %+v %+v\n应为 %+v %+v", c.Filters, c.Capture, want.Filters, want.Capture)
	}
}

func TestCaptureWindow_为0时跟随新鲜度(t *testing.T) {
	c := Default()
	if got := c.CaptureWindow(); got != 6*time.Hour {
		t.Fatalf("capture.max_hours=0 时应等于 freshness.max_hours=6h,得到 %v", got)
	}
	c.Capture.MaxHours = 3
	if got := c.CaptureWindow(); got != 3*time.Hour {
		t.Fatalf("显式写 3 应为 3h,得到 %v", got)
	}
}

func TestValidate_深度与抓包段的边界(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		key  string // 报错里应点名的键
	}{
		// 截断在 128 档、全在近价窗口内时近价件数可能只有 128,阈值 200 会误拒
		{"件数阈值大于book_levels", func(c *Config) { c.Filters.MinBidDepth = 200 }, "min_bid_depth"},
		{"thin_bid_edge_qty也要受限", func(c *Config) { c.Capture.BookLevels = 50 }, "thin_bid_edge_qty"},
		{"near_pct为0", func(c *Config) { c.Filters.NearPct = 0 }, "near_pct"},
		{"near_pct过大", func(c *Config) { c.Filters.NearPct = 0.6 }, "near_pct"},
		{"book_levels为0", func(c *Config) { c.Capture.BookLevels = 0 }, "book_levels"},
		// 抓包窗口比 AODP 过期线还宽,就能拿更旧的价盖掉 AODP
		{"抓包窗口超过新鲜度", func(c *Config) { c.Capture.MaxHours = 8 }, "capture.max_hours"},
		{"抓包窗口为负", func(c *Config) { c.Capture.MaxHours = -1 }, "capture.max_hours"},
		{"深度窗口为0", func(c *Config) { c.Capture.DepthMaxHours = 0 }, "depth_max_hours"},
		{"深度窗口超过抓包窗口", func(c *Config) { c.Capture.MaxHours = 1 }, "depth_max_hours"},
		{"同一眼宽容度为负", func(c *Config) { c.Capture.SnapshotSlackSeconds = -1 }, "snapshot_slack_seconds"},
		{"优先宽容度为负", func(c *Config) { c.Capture.PreferSlackMinutes = -1 }, "prefer_slack_minutes"},
		{"并入物品上限为负", func(c *Config) { c.Capture.MaxExtraItems = -1 }, "max_extra_items"},
		{"重算间隔为负", func(c *Config) { c.Capture.ReevalSeconds = -1 }, "reeval_seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mut(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("应该报错")
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("报错应点名 %s,得到 %v", tc.key, err)
			}
		})
	}

	// 边界上的值要放行:阈值恰好等于 book_levels、slack 设成逃生口那么大
	c := Default()
	c.Filters.ThinBidEdgeQty = 128
	c.Capture.SnapshotSlackSeconds = 6 * 3600
	c.Capture.MaxHours = 6
	c.Filters.NearPct = 0.5
	if err := c.Validate(); err != nil {
		t.Fatalf("边界值应放行: %v", err)
	}
}

func TestLoad_抓包窗口超过新鲜度加载失败(t *testing.T) {
	if _, err := Load(writeYAML(t, "capture:\n  max_hours: 8\n")); err == nil {
		t.Fatal("capture.max_hours=8 大于 freshness.max_hours=6,应该加载失败")
	}
}

// 旧 Python 配置里有 Go 不认的键(data_dir 等),拿来就用是常见操作,
// 不能因此起不来;但也不能静默吞掉——键名写错时阈值会悄悄走默认值
func TestLoad_未知键只告警不失败(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	c, err := Load(writeYAML(t, "server: east\nfilters:\n  min_book_qty: 7\ndata_dir: data\n"))
	if err != nil {
		t.Fatalf("未知键不该导致加载失败: %v", err)
	}
	if c.Filters.MinBookQty != 7 {
		t.Fatalf("认识的键照常生效,得到 %d", c.Filters.MinBookQty)
	}
	log := buf.String()
	if !strings.Contains(log, "配置里有不认识的键") || !strings.Contains(log, "data_dir") {
		t.Fatalf("应告警并点名 data_dir,日志: %s", log)
	}

	// 全是认识的键时不告警
	buf.Reset()
	if _, err := Load(writeYAML(t, "filters:\n  min_book_qty: 7\n")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("没有未知键时不该告警,日志: %s", buf.String())
	}
}

// 仓库自带的 config.yaml 要能干净地加载:没有未知键、能过校验
func TestLoad_仓库自带配置(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	c, err := Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("自带配置加载失败: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("自带配置不该有未知键,日志: %s", buf.String())
	}
	d := Default()
	if c.Capture != d.Capture || c.Filters != d.Filters {
		t.Fatalf("自带配置里写的深度阈值应和内置默认值一致\n得到 %+v %+v", c.Capture, c.Filters)
	}
}
