package diag

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// err 序列化成 {} 是最容易漏的一个坑:排查时最想看的就是它。
func TestJSONSafeKeepsErrorText(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"error", fmt.Errorf("打开 eth0 失败: %w", errors.New("No Such Device")),
			`"打开 eth0 失败: No Such Device"`},
		{"string", "abc", `"abc"`},
		{"int", 42, `42`},
		{"duration", 3 * time.Second, `3000000000`},
		{"struct", struct{ A int }{1}, `"{1}"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := json.Marshal(jsonSafe(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != c.want {
				t.Fatalf("得到 %s,想要 %s", b, c.want)
			}
		})
	}
}
