package flip

import (
	"context"
	"errors"
	"time"
)

// 查价页和精炼页是交互式的:成员点一下就在等。AODP 正常时最多等 lookupAODPWait
// (12 秒,主要是给"和定时扫描排队限流"留余地);可 AODP 整个不响应的时候
// (2026-10-02 实测:TCP 连得上、HTTP 20 秒不回),每点一次都要白等满 12 秒,
// 失败又不进缓存,下一次照样再等 —— 页面就卡在"正在查"。
//
// 所以先问 AODP 客户端"最近是不是一直连不上":是的话只等 lookupAODPWaitDown,
// 抓包数据马上出来;请求照样在后台跑(见 lookup_cache.go),AODP 一恢复就进缓存。
const (
	lookupAODPWaitDown = 1500 * time.Millisecond
	// aodpDownWindow:最近一次失败超过这么久就不再当它"还挂着",回到正常的等法。
	// 定时扫描、补拉每分钟左右都会碰一次 AODP,这个窗口内总会有新的成败记录
	aodpDownWindow = 3 * time.Minute
)

// aodpDown 是"AODP 已知连不上、这次没等它"的错误。包着 context.DeadlineExceeded,
// 已有的 errors.Is(err, context.DeadlineExceeded) 判断照样成立
type aodpDown struct {
	since time.Time
	why   string
}

func (d *aodpDown) Error() string {
	s := "AODP 从 " + d.since.Local().Format("15:04:05") + " 起连不上"
	if d.why != "" {
		s += "(" + d.why + ")"
	}
	return s
}

func (d *aodpDown) Unwrap() error { return context.DeadlineExceeded }

// aodpWait 返回这一次最多等 AODP 多久;AODP 已知连不上时一并返回原因,
// 等不到时用它换掉笼统的"超时"
func (s *Service) aodpWait(now time.Time) (time.Duration, *aodpDown) {
	if s.aodp == nil {
		return lookupAODPWait, nil
	}
	since, why, down := s.aodp.Down(now, aodpDownWindow)
	if !down {
		return lookupAODPWait, nil
	}
	return lookupAODPWaitDown, &aodpDown{since: since, why: why}
}

// asDown 把"没等到"换成"它连不上":只换等待到期,别的错误(4xx、解析失败)原样留着
func asDown(err error, d *aodpDown) error {
	if err == nil || d == nil {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return d
	}
	return err
}
