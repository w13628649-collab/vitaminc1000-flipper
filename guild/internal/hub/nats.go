package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"albion-guild/internal/model"
)

// DefaultSubject 是行情广播用的主题。
//
// 先用单主题、各实例收全量再按本地订阅过滤:200 人量级下每秒也就几十条
// 批量消息,过滤成本可以忽略。实例数上去、跨实例带宽开始显眼时,
// 再换成 quote.{item_id} 这种细粒度主题。
const DefaultSubject = "quotes"

// NatsBroadcaster 把行情发到 NATS,让所有实例都能扇给自己的连接。
//
// 解决的是 WS 的连接粘性:客户端 A 连实例 1、B 连实例 2,
// 数据从 A 这边进来时,实例 1 的内存里压根没有 B。
type NatsBroadcaster struct {
	conn    *nats.Conn
	hub     *Hub
	subject string
}

// DialNats 连 NATS 并配好重连。断线期间要发的东西堆在缓冲里,恢复后自动补发。
func DialNats(url string, h *Hub, subject string) (*NatsBroadcaster, error) {
	if subject == "" {
		subject = DefaultSubject
	}
	conn, err := nats.Connect(url,
		nats.MaxReconnects(-1), // 一直重连,别放弃
		nats.ReconnectWait(2*time.Second),
		nats.ReconnectBufSize(8*1024*1024),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("NATS 断开", "err", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			slog.Info("NATS 已重连", "url", c.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("连接 NATS: %w", err)
	}
	return &NatsBroadcaster{conn: conn, hub: h, subject: subject}, nil
}

// Publish 只发布,**不在本地直接 Fanout**。
//
// NATS 会把消息回显给发布者,自己再扇一次的话本实例的客户端会收到两遍。
// 统一走"发出去 → 订阅回来 → Fanout"一条路径,代价是同可用区内多一个
// 不到 1ms 的 RTT,换来单实例和多实例行为完全一致。
func (b *NatsBroadcaster) Publish(_ context.Context, quotes []model.Quote) error {
	if len(quotes) == 0 {
		return nil
	}
	payload, err := json.Marshal(quotes)
	if err != nil {
		return err
	}
	// Publish 是异步的(写进本地发送缓冲),对行情正合适。
	// 不要每批都 Flush,那会把吞吐打回同步 RTT。
	return b.conn.Publish(b.subject, payload)
}

// Run 订阅并把收到的行情扇给本实例的连接,阻塞到 ctx 取消。
func (b *NatsBroadcaster) Run(ctx context.Context) error {
	sub, err := b.conn.Subscribe(b.subject, func(m *nats.Msg) {
		var quotes []model.Quote
		if err := json.Unmarshal(m.Data, &quotes); err != nil {
			slog.Warn("广播内容解析失败", "err", err)
			return
		}
		b.hub.Fanout(quotes) // 和单实例版本走的是同一个函数
	})
	if err != nil {
		return fmt.Errorf("订阅 %s: %w", b.subject, err)
	}
	slog.Info("NATS 广播已就绪", "subject", b.subject, "url", b.conn.ConnectedUrl())

	<-ctx.Done()
	_ = sub.Unsubscribe()
	b.conn.Close()
	return nil
}
