package ingest

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/model"
)

// Normalize 把一批上传的观测时间统一到服务端时钟。
//
// 为什么要统一:WriteOrders 写进 last_seen 的是客户端给的 ObservedAt,
// 以前 TouchOrders 写的却是服务端 flush 的时刻,两套钟混在一张表里。
// 读簿时要按 last_seen 判断几张单是不是"同一眼"看到的(幽灵单剔除靠它),
// 时钟不统一这个判断就做不准。
//
// 做法:
//  1. SentAt 非零时,skew = now − SentAt,所有非零 ObservedAt 整体加上 skew。
//     整批平移而不是逐条改成 now:批内先后顺序不变,断网重传的旧单
//     也不会被"刷新"成刚看到的
//  2. 零值补成 now(老客户端可能不填)
//  3. 平移后仍超前于 now 的钳到 now 并计数:没有 SentAt 的老客户端钟快时,
//     只能靠这一步兜住,否则 last_seen 在未来,永远不会被判过期
//
// skew 里含网络延迟(正向、通常几十毫秒),会让观测时间略微偏新,
// 量级远小于"同一眼"的宽容度,不单独处理。
func Normalize(b *model.UploadBatch, now time.Time) (skew time.Duration, clamped int) {
	if !b.SentAt.IsZero() {
		skew = now.Sub(b.SentAt)
	}
	for i := range b.Orders {
		o := &b.Orders[i]
		if o.ObservedAt.IsZero() {
			o.ObservedAt = now
			continue
		}
		o.ObservedAt = o.ObservedAt.Add(skew)
		if o.ObservedAt.After(now) {
			o.ObservedAt = now
			clamped++
		}
	}
	return skew, clamped
}

// canonicalItemID 在附魔等级大于 0、而 id 里没有 @ 时补上 @N。
//
// AODP 和扫描器都用 T5_2H_FIRESTAFF@2 这种带后缀的写法做 key。实抓样本
// 本来就带后缀(protocol_test 里的真包是 T6_METALBAR_LEVEL4@4),这里只是防御:
// 哪天游戏或解析改了口径,不补的话 .2 的挂单会并进 .0 的盘口,价格全错
func canonicalItemID(id string, enchant int16) string {
	if enchant > 0 && !strings.Contains(id, "@") {
		return id + "@" + strconv.Itoa(int(enchant))
	}
	return id
}

// identOf 是一张单"挂在哪个盘口"的指纹:物品 | 城市 | 品质 | 方向。
//
// 游戏里一张单的这几项不会变(改了就是新 order_id),所以同一个 order_id
// 指纹变了,几乎只可能是多开时客户端把城市记串了,或者跨服 id 撞车、
// 解析出错。存 64 位哈希而不是原字符串,是为了 50 万条的 LRU 不多吃内存
func identOf(o model.MarketOrder) uint64 { return identOfKey(o.Key()) }

// identOfKey 和 identOf 同一个指纹,从盘口键算。查冲突记录时用
func identOfKey(k model.QuoteKey) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(k.ItemID))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(k.LocationID))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(strconv.Itoa(int(k.Quality))))
	_, _ = h.Write([]byte{'|'})
	_, _ = h.Write([]byte(strconv.Itoa(int(k.Side))))
	return h.Sum64()
}
