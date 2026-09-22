// checkconfig 读一遍配置文件把解析结果打出来,改完 YAML 先跑这个确认没写错。

package main

import (
	"fmt"
	"os"

	"albion-guild/internal/conf"
)

func main() {
	c, err := conf.Load(os.Args[1])
	if err != nil {
		fmt.Println("错误:", err)
		os.Exit(1)
	}
	fmt.Printf("server=%s cities=%d capital=%d qualities=%v\n", c.Server, len(c.Cities), c.Capital, c.Qualities)
	fmt.Printf("filters: dev=[%g,%g] minVol=%g maxMargin=%g crossed=%v gap=%g days=%d\n",
		c.Filters.DeviationMin, c.Filters.DeviationMax, c.Filters.MinDailyVolumeSilver,
		c.Filters.MaxMargin, c.Filters.RejectCrossedBook, c.Filters.MaxHistoryGapDays, c.Filters.MinDaysWithData7d)
	fmt.Printf("econ: tax=%g fee=%g buyFee=%v 摩擦=%.1f%%\n",
		c.Economics.MarketTax, c.Economics.SetupFee, c.Economics.BuyOrderSetupFee, c.Economics.RoundTripFriction()*100)
	fmt.Printf("api: url=%d rate=%d/%d  sizing: absorb=%g base=%d hist=%d\n",
		c.API.MaxURLLength, c.API.RatePerMinute, c.API.RatePer5Min,
		c.Sizing.AbsorbRatio, c.Sizing.BaselineDays, c.Sizing.HistoryDays)
	fmt.Printf("patterns=%d\n", len(c.Items.Patterns))
}
