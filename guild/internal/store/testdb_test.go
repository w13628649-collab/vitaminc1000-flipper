package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// openTestStore 给每个集成测试一个全新的临时库。
//
// 没设 FLIPPER_TEST_DSN 就跳过:单元测试不该要求本机有数据库。
// 设了就在那个实例上 CREATE DATABASE flipper_it_<unixnano>,跑完整套迁移,
// 测完 DROP ... WITH (FORCE)。**只动自己建的临时库**,DSN 指向的那个库
// (通常是 postgres)只用来建删库,用户的 flipper 库碰都不碰。
// 依赖 template1 里已经装好 timescaledb 扩展,迁移里的 create_hypertable 才跑得通
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("FLIPPER_TEST_DSN")
	if dsn == "" {
		t.Skip("没设 FLIPPER_TEST_DSN,跳过数据库集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("连接测试实例: %v", err)
	}
	name := fmt.Sprintf("flipper_it_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("建临时库: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("删临时库 %s: %v", name, err)
		}
		_ = admin.Close(ctx)
	})

	st, err := Open(ctx, dsnWithDatabase(t, dsn, name))
	if err != nil {
		t.Fatalf("打开临时库: %v", err)
	}
	// 删库先登记、关池后登记:Cleanup 后进先出,池先关、库后删
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("迁移临时库: %v", err)
	}
	return st
}

// dsnWithDatabase 把 DSN 里的库名换成 name。URL 和 key=value 两种写法都认。
func dsnWithDatabase(t *testing.T, dsn, name string) string {
	t.Helper()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("解析 FLIPPER_TEST_DSN: %v", err)
		}
		u.Path = "/" + name
		return u.String()
	}
	// key=value 写法里同名键后者生效
	return dsn + " dbname=" + name
}
