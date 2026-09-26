package screen

import (
	"math"
	"strconv"
)

// Thousands 把整数按千分位写成 "2,022,222"。
//
// 拒绝原因、警告这些文案原样显示在界面上。界面自己的数字都带千分位,
// 文案里的价和件数不带的话,"买一 2022222" 得一位一位数,和旁边格子里的
// 2,022,222 对不上也不好认
func Thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	out := make([]byte, 0, len(s)+len(s)/3+1)
	if neg {
		out = append(out, '-')
	}
	head := len(s) % 3
	if head == 0 {
		head = 3
	}
	out = append(out, s[:head]...)
	for i := head; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

// thousandsF 是浮点版:先四舍五入到整数再分组,给日流水这种银币总额用。
func thousandsF(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return Thousands(int64(math.Round(v)))
}

// thousands1 保留一位小数、整数部分分组:单件利润几银到几万银都有,
// 便宜货的零点几银不能四舍五入没了
func thousands1(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	s := strconv.FormatFloat(math.Abs(v), 'f', 1, 64)
	dot := len(s) - 2 // 'f', 1 的输出恒为 "整数.一位"
	ip, err := strconv.ParseInt(s[:dot], 10, 64)
	if err != nil {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	out := Thousands(ip) + s[dot:]
	if v < 0 && s != "0.0" {
		out = "-" + out
	}
	return out
}
