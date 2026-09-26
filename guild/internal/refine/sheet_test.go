package refine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"testing"
)

// 表格口径复算(设计 §4.7 第 5 条):用原始模板「制作计算器」钢条块的算法和作者手填的 J/K/N/O 值,
// 跑同一份 09-26 抓包价,应当得到 3,338,630 / 根。
//
// 这个测试**不是**说表格对:它是「为什么和表格不同」的证据(spreadsheet_diff.md)。
// 表格原样:8.4 矿全部按卖一 1,042,319 算(实际这个价只有 3 件)、每级强制自炼、
// 每级加尾常数 +(n−1)、返还率整条链 0.539。同一组输入本引擎全链专注自动比价是 2,900,775。
//
// J/K/N/O 从用户本机的 xlsx 导出(scratchpad rf3_sheet_export.py,openpyxl 只读),
// 放在 testdata/local/sheet_template.json,不进仓库;没有就跳过。
const sheetFile = "testdata/local/sheet_template.json"

type sheetRow struct {
	Tier    int      `json:"tier"`
	Enchant int      `json:"enchant"`
	J       *float64 `json:"J"` // 同 tier 升附魔的转化费(作者手抄)
	K       *float64 `json:"K"` // 同附魔升 tier 的转化费
	N       *float64 `json:"N"` // 每次专注(表格写死满专精)
	O       *float64 `json:"O"` // 每次站费
}

func loadSheet(t *testing.T) map[[2]int]sheetRow {
	t.Helper()
	raw, err := os.ReadFile(sheetFile)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("没有 %s(用户本机表格导出,不进仓库),跳过表格对照", sheetFile)
	}
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Rows []sheetRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	out := map[[2]int]sheetRow{}
	for _, r := range f.Rows {
		out[[2]int{r.Tier, r.Enchant}] = r
	}
	if len(out) != 25 {
		t.Fatalf("表格 %d 行,想要 T4~T8 × .0~.4 共 25 行", len(out))
	}
	return out
}

// sheetBlock 是表格钢条块的算法(照 rf_sheet.py):
//   - L 列:每档矿的单价 = min(卖一, L[同 tier 低一附魔] + J, L[低一 tier 同附魔] + K)
//   - 从 T8 往下每级做 T 件(T8 = Q,下一级 = 上一级 × (1 − rr)),用矿
//     Y = T × n × (1 − rr) + (n − 1)(尾常数),全部按 L 价
//   - 站费 T × O;链底 T3 条按 p3 买
func sheetBlock(rows map[[2]int]sheetRow, bestAsk map[[2]int]float64, q, rr float64, tail bool, p3 float64) float64 {
	n := map[int]float64{4: 2, 5: 3, 6: 4, 7: 5, 8: 5}
	L := map[[2]int]float64{}
	for tier := 4; tier <= 8; tier++ {
		for e := 0; e <= 4; e++ {
			k := [2]int{tier, e}
			best := bestAsk[k]
			if best <= 0 {
				best = math.Inf(1)
			}
			if r := rows[k]; e >= 1 && r.J != nil {
				best = math.Min(best, L[[2]int{tier, e - 1}]+*r.J)
			}
			if r := rows[k]; tier >= 5 && r.K != nil {
				best = math.Min(best, L[[2]int{tier - 1, e}]+*r.K)
			}
			L[k] = best
		}
	}
	cost, fee, prev := 0.0, 0.0, 0.0
	for tier := 8; tier >= 4; tier-- {
		T := q
		if tier != 8 {
			T = prev * (1 - rr)
		}
		Y := T * n[tier] * (1 - rr)
		if tail && T > 0 {
			Y += n[tier] - 1
		}
		k := [2]int{tier, 4}
		cost += Y * L[k]
		fee += T * *rows[k].O
		prev = T
	}
	return (cost + fee + prev*(1-rr)*p3) / q
}

func Test表格口径复算(t *testing.T) {
	d := loadCapture(t)
	rows := loadSheet(t)
	l := Lines[0]
	bestAsk := map[[2]int]float64{}
	for tier := 4; tier <= 8; tier++ {
		for e := 0; e <= 4; e++ {
			if b, ok := d.Books[l.RawID(tier, e)+"|Thetford"]; ok && len(b.Asks.Levels) > 0 {
				bestAsk[[2]int{tier, e}] = float64(b.Asks.Levels[0].Price)
			}
		}
	}
	// 表格的 T3 条价:rf_sheet.py 用的是 Martlock 的卖一 195(Thetford 没有)
	p3 := float64(d.Books["T3_METALBAR|Martlock"].Asks.Levels[0].Price)
	assertNear(t, "T3 条", p3, 195, 0)

	// 表格原样(含尾常数):Q=10 3,338,630;Q=1 8,551,760
	assertNear(t, "表格原样 Q=10", sheetBlock(rows, bestAsk, 10, 0.539, true, p3), 3_338_630, 1)
	assertNear(t, "表格原样 Q=1", sheetBlock(rows, bestAsk, 1, 0.539, true, p3), 8_551_760, 1)
	// 去掉尾常数:2,759,393
	assertNear(t, "表格去尾常数", sheetBlock(rows, bestAsk, 10, 0.539, false, p3), 2_759_393, 1)

	// 表格的挂卖到手是 卖一 × (1 − 2% − 4.5%),碰巧也是 0.935(spreadsheet_diff D7)
	sell := float64(d.Books[barT84+"|Thetford"].Asks.Levels[0].Price) * (1 - 0.02 - 0.045)
	assertNear(t, "表格挂卖利", sell-sheetBlock(rows, bestAsk, 10, 0.539, true, p3), -290_531, 1)

	// 表格的 J/K 和本引擎的 k=1.154 是同一口径:每步转化费相差不超过 6.4 银(规则表 B5)
	e := captureEngine(t, oreBooks(d), barT84, DefaultTransmuteFactor, nil)
	for tier := 4; tier <= 8; tier++ {
		for en := 0; en <= 4; en++ {
			to := l.RawID(tier, en)
			for _, te := range e.g.transIn[to] {
				_, ft, fe, _ := ParseItemID(te.from)
				r := rows[[2]int{tier, en}]
				sheet := r.K
				if ft == tier && fe == en-1 {
					sheet = r.J
				}
				if sheet == nil {
					continue
				}
				ours := TransmuteCost(te.silver, DefaultTransmuteFactor, te.toIV, DefaultTransmuteStationFeePer100)
				if math.Abs(ours-*sheet) > 6.5 && !(tier == 4 && en == 1) {
					t.Fatalf("%s ← %s:本引擎 %.1f,表格 %.0f", to, te.from, ours, *sheet)
				}
			}
		}
	}

	// 同一组输入,本引擎全链专注自动比价:2,900,775(差在 8.4 矿按阶梯吃、没有尾常数)
	ev := e.evaluate(scenario[idD])
	assertNear(t, "本引擎", e.unitCost(ev), 2_900_775, 1)
}
