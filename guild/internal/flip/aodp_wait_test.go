package flip

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAODPWait_没有客户端或没挂时照常等(t *testing.T) {
	s := &Service{}
	if w, d := s.aodpWait(time.Now()); w != lookupAODPWait || d != nil {
		t.Fatalf("没有 AODP 客户端时应照常等 %v,得到 %v / %v", lookupAODPWait, w, d)
	}
}

// 等待到期才换成"它连不上";4xx、解析失败这类对面回话了的错误原样留着
func TestAsDown_只换等待到期(t *testing.T) {
	d := &aodpDown{since: time.Date(2026, 10, 2, 11, 53, 29, 0, time.UTC), why: "请求超时"}
	if got := asDown(context.DeadlineExceeded, d); got != d {
		t.Fatalf("等待到期应换成 aodpDown,得到 %v", got)
	}
	other := errors.New("HTTP 404")
	if got := asDown(other, d); got != other {
		t.Fatalf("对面回话了的错误应原样留着,得到 %v", got)
	}
	if got := asDown(context.DeadlineExceeded, nil); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("没挂时到期就是普通的到期,得到 %v", got)
	}
	if asDown(nil, d) != nil {
		t.Fatal("没错误就没错误")
	}
	// 换过之后,老代码里 errors.Is(err, DeadlineExceeded) 的判断照样成立
	if !errors.Is(asDown(context.DeadlineExceeded, d), context.DeadlineExceeded) {
		t.Fatal("aodpDown 应该包着 DeadlineExceeded")
	}
}

// 界面上的提示要说"它连不上、先只用抓包",不能再说"排队限流、稍后再点"——
// 用户照着再点只会再等一次
func TestAODPErrText_连不上时说清楚(t *testing.T) {
	d := &aodpDown{since: time.Now(), why: "请求超时"}
	msg := aodpErrText(d)
	for _, want := range []string{"连不上", "请求超时", "只用抓包"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("提示里应有 %q,得到 %q", want, msg)
		}
	}
	if strings.Contains(msg, "排队限流") {
		t.Fatalf("连不上时不该再说排队限流,得到 %q", msg)
	}
	if !strings.Contains(aodpErrText(context.DeadlineExceeded), "排队限流") {
		t.Fatal("没挂时的到期提示应保持原样")
	}
}
