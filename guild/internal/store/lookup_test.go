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

	"albion-guild/internal/model"
)

// openLookupTestDB 给查价这组集成测试一个全新的临时库。没设 FLIPPER_TEST_DSN 就跳过。
//
// 名字故意不叫 openTestStore:另一条后端线在同一个包里加了同名 helper,
// 合并后两份可以收成一份,在那之前别撞名。
func openLookupTestDB(t *testing.T) *Store {
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
	name := fmt.Sprintf("flipper_lookup_it_%d", time.Now().UnixNano())
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

	target := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("解析 FLIPPER_TEST_DSN: %v", err)
		}
		u.Path = "/" + name
		target = u.String()
	}
	st, err := Open(ctx, target)
	if err != nil {
		t.Fatalf("打开临时库: %v", err)
	}
	t.Cleanup(st.Close) // 后进先出:池先关、库后删
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("迁移临时库: %v", err)
	}
	return st
}

func TestItemOrdersIntegration(t *testing.T) {
	st := openLookupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	insert := func(id int64, item, loc string, q, side int16, price int64, amount int32,
		first, last time.Time) {
		t.Helper()
		if _, err := st.pool.Exec(ctx, `INSERT INTO market_order_live
			(order_id, item_id, location_id, quality, side, unit_price, amount, first_seen, last_seen)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			id, item, loc, q, side, price, amount, first, last); err != nil {
			t.Fatal(err)
		}
	}
	insert(1, "T4_LEATHER", "Martlock", 1, 0, 310, 15736, now.Add(-3*time.Hour), now.Add(-time.Minute))
	insert(2, "T4_LEATHER", "Martlock", 1, 1, 301, 1775, now.Add(-time.Hour), now.Add(-2*time.Minute))
	insert(3, "T4_LEATHER", "Thetford", 2, 0, 400, 5, now.Add(-time.Hour), now.Add(-5*time.Hour))
	insert(4, "T4_LEATHER", "Martlock", 1, 0, 305, 0, now, now)                                     // 0 件不算
	insert(5, "T5_LEATHER", "Martlock", 1, 0, 700, 3, now, now)                                     // 别的物品
	insert(6, "T4_LEATHER", "Lymhurst", 1, 0, 450, 1, now.Add(-9*time.Hour), now.Add(-8*time.Hour)) // 窗口外

	got, err := st.ItemOrders(ctx, "T4_LEATHER", now.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("应读到 3 张单,得到 %d: %+v", len(got), got)
	}
	// ORDER BY location_id, quality, side, unit_price
	o := got[0]
	if o.City != "Martlock" || o.Side != model.SideOffer || o.Price != 310 || o.Amount != 15736 ||
		!o.FirstSeen.Equal(now.Add(-3*time.Hour)) || !o.LastSeen.Equal(now.Add(-time.Minute)) {
		t.Fatalf("第一张: %+v", o)
	}
	if got[1].Side != model.SideRequest || got[2].City != "Thetford" || got[2].Quality != 2 {
		t.Fatalf("排序或字段不对: %+v", got)
	}
	if got[0].LastSeen.Location() != time.UTC {
		t.Fatal("时间应统一成 UTC")
	}
}

func TestItemHistoryIntegration(t *testing.T) {
	st := openLookupTestDB(t)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	exec := func(item, loc string, q int16, bucket time.Time, count, avg int64, source string) {
		t.Helper()
		if _, err := st.pool.Exec(ctx, `INSERT INTO market_history
			(item_id, location_id, quality, timescale, bucket, item_count, avg_price, source, observed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())`,
			item, loc, q, int16(TimescaleDaily), bucket, count, avg, source); err != nil {
			t.Fatal(err)
		}
	}
	exec("T4_LEATHER", "Martlock", 1, day.AddDate(0, 0, -1), 1000, 300, SourceAODP)
	exec("T4_LEATHER", "Martlock", 1, day.AddDate(0, 0, -1), 20, 310, SourceCapture) // 同桶两个来源都回
	exec("T4_LEATHER", "Martlock", 1, day.AddDate(0, 0, -2), 900, 295, SourceAODP)
	exec("T4_LEATHER", "Martlock", 1, day.AddDate(0, 0, -40), 1, 1, SourceAODP) // 窗口外
	exec("T5_LEATHER", "Martlock", 1, day.AddDate(0, 0, -1), 5, 5, SourceAODP)  // 别的物品
	if _, err := st.pool.Exec(ctx, `INSERT INTO market_history
		(item_id, location_id, quality, timescale, bucket, item_count, avg_price, source, observed_at)
		VALUES ('T4_LEATHER','Martlock',1,0,$1,1,1,'aodp',now())`, day.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err) // 不是日线
	}

	got, err := st.ItemHistory(ctx, "T4_LEATHER", day.AddDate(0, 0, -31))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("应读到 3 根日线,得到 %d: %+v", len(got), got)
	}
	if !got[0].Day.Equal(day.AddDate(0, 0, -2)) || got[0].ItemCount != 900 || got[0].Source != SourceAODP {
		t.Fatalf("按 bucket 升序: %+v", got[0])
	}
	sources := map[string]bool{got[1].Source: true, got[2].Source: true}
	if !sources[SourceAODP] || !sources[SourceCapture] {
		t.Fatalf("同一天两个来源都要返回,由调用方整条选: %+v", got[1:])
	}
}

// ItemOrders 读回来的形状要能直接喂给 buildSide:这里只确认
// WriteOrders 写进去的单(first_seen = last_seen = ObservedAt)读得回来。
func TestItemOrdersSeesWriteOrders(t *testing.T) {
	st := openLookupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := st.WriteOrders(ctx, "tester", []model.MarketOrder{{
		OrderID: 77, ItemID: "T6_METALBAR", LocationID: "Thetford", Quality: 1,
		Side: model.SideRequest, UnitPrice: 2000, Amount: 12, ObservedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ItemOrders(ctx, "T6_METALBAR", now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Amount != 12 || got[0].Side != model.SideRequest ||
		!got[0].LastSeen.Equal(now) {
		t.Fatalf("got %+v", got)
	}
}
