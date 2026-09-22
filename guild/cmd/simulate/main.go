// simulate 模拟公会成员在游戏里翻市场,往服务端灌数据。
//
// 没有 Windows + Npcap + 游戏客户端的时候,用它验证整条链路:
// 去重是否生效、行情是否推得出去、前端会不会跳。
// 它刻意制造大量"什么都没变"的重复观测,因为真实场景里那才是大头。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/ludy/albion-guild/internal/model"
)

var (
	items  = []string{"T4_METALBAR", "T5_METALBAR", "T6_METALBAR", "T5_CLOTH", "T5_PLANKS"}
	cities = []string{"Martlock", "Lymhurst", "Bridgewatch", "Thetford"}
	names  = []string{"Ludy", "Bruno313", "Mitch77", "Apolo540"}
)

// order 是模拟市场里的一张挂单
type order struct {
	id     int64
	item   string
	city   string
	side   model.Side
	price  int64
	amount int32
}

func main() {
	var (
		server   = flag.String("server", "http://127.0.0.1:8080", "服务端地址")
		interval = flag.Duration("interval", 700*time.Millisecond, "上传间隔")
		orders   = flag.Int("orders", 240, "模拟市场里有多少张活跃挂单")
		churn    = flag.Float64("churn", 0.06, "每轮有多大比例的挂单发生变化")
		rounds   = flag.Int("rounds", 0, "跑多少轮,0 = 一直跑")
	)
	flag.Parse()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	book := seed(rng, *orders, time.Now().Unix()*1000)
	// 起点带时间戳:重启模拟器时不会跟上一轮的 id 撞上
	// (真实游戏里 order_id 由服务器发,不存在这个问题)
	base := time.Now().Unix() * 1000
	nextID := base + int64(*orders)

	client := &http.Client{Timeout: 10 * time.Second}
	var totalSent, totalChanged, totalTouched int

	for round := 1; *rounds == 0 || round <= *rounds; round++ {
		// 模拟市场变动:一小部分挂单被吃掉一些、改价,或者整张消失换新的
		for _, o := range book {
			if rng.Float64() >= *churn {
				continue
			}
			switch rng.Intn(3) {
			case 0: // 被买走一部分
				if o.amount > 10 {
					o.amount -= int32(rng.Intn(int(o.amount / 3)))
				}
			case 1: // 挂单人改价
				delta := float64(o.price) * (rng.Float64()*0.06 - 0.03)
				o.price = max64(1, o.price+int64(delta))
			case 2: // 撤单/成交后换一张新的。
				// 必须换新 order_id —— 游戏里订单 ID 是订单的身份,
				// 撤单再挂是两张不同的单,不会顶着同一个 ID 换物品
				nextID++
				*o = *newOrder(rng, nextID)
			}
		}

		// 模拟"有人翻了一页市场":看到的是当前状态的一个快照,
		// 其中绝大多数挂单跟上次看到时一模一样
		batch := model.UploadBatch{Reporter: names[rng.Intn(len(names))]}
		now := time.Now()
		for _, o := range book {
			if rng.Float64() > 0.55 { // 不是每张单每轮都会被看到
				continue
			}
			batch.Orders = append(batch.Orders, model.MarketOrder{
				OrderID: o.id, ItemID: o.item, LocationID: o.city,
				Quality: 1, Side: o.side, UnitPrice: o.price, Amount: o.amount,
				ObservedAt: now,
			})
		}
		if len(batch.Orders) == 0 {
			continue
		}

		changed, touched, err := upload(client, *server, batch)
		if err != nil {
			log.Printf("上传失败: %v", err)
			time.Sleep(time.Second)
			continue
		}
		totalSent += len(batch.Orders)
		totalChanged += changed
		totalTouched += touched

		if round%10 == 0 || round <= 3 {
			saved := 0.0
			if totalSent > 0 {
				saved = float64(totalTouched) / float64(totalSent) * 100
			}
			log.Printf("第 %d 轮: 本次上传 %d 条 → 入库 %d / 去重 %d;累计去重率 %.0f%%",
				round, len(batch.Orders), changed, touched, saved)
		}
		time.Sleep(*interval)
	}
}

func seed(rng *rand.Rand, n int, base int64) []*order {
	book := make([]*order, 0, n)
	for i := range n {
		book = append(book, newOrder(rng, base+int64(i)))
	}
	return book
}

func newOrder(rng *rand.Rand, id int64) *order {
	item := items[rng.Intn(len(items))]
	base := map[string]int64{
		"T4_METALBAR": 300, "T5_METALBAR": 1120, "T6_METALBAR": 2890,
		"T5_CLOTH": 1180, "T5_PLANKS": 1040,
	}[item]
	side := model.SideOffer
	if rng.Intn(2) == 1 {
		side = model.SideRequest
	}
	// 卖单挂在基准价之上,买单挂在之下,这样盘口看起来像那么回事
	spread := 1.0 + rng.Float64()*0.12
	if side == model.SideRequest {
		spread = 1.0 - rng.Float64()*0.12
	}
	return &order{
		id: id, item: item, city: cities[rng.Intn(len(cities))], side: side,
		price:  int64(float64(base) * spread),
		amount: int32(5 + rng.Intn(400)),
	}
}

func upload(c *http.Client, server string, b model.UploadBatch) (changed, touched int, err error) {
	body, err := json.Marshal(b)
	if err != nil {
		return 0, 0, err
	}
	resp, err := c.Post(server+"/api/upload", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct{ Changed, Touched int }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, err
	}
	return out.Changed, out.Touched, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
