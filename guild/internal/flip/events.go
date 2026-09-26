package flip

import (
	"encoding/json"
	"log/slog"
	"time"

	"albion-guild/internal/hub"
	"albion-guild/internal/scan"
)

// TopicPublisher 把一条消息推给订阅了某个主题的 WS 连接。hub.Hub 实现它。
type TopicPublisher interface {
	PublishTopic(topic string, payload []byte) int
}

// ScanEvent 是每次对外发布扫描结果后推给订阅了 scan 的 WS 连接的那条通知。
//
// 界面拿 Digest 和手上那份比,不同才重拉 /api/scan(同一个值也在 /api/scan 的
// digest 字段里)。摘要没变也照发:界面据此知道重算还活着,
// "evaluated_at" 一直在走。Full 为 true 表示这次是 AODP 全量,false 是
// 用缓存的 AODP 快照配最新抓包的快速重算。
type ScanEvent struct {
	Type          string    `json:"type"` // 恒为 "scan"
	EvaluatedAt   time.Time `json:"evaluated_at"`
	StartedAt     time.Time `json:"started_at"`
	Digest        string    `json:"digest"`
	Opportunities int       `json:"opportunities"`
	Routes        int       `json:"routes"`
	Full          bool      `json:"full"`
	// Ingest 是发布这一刻入库口径的计数(线上是 ingest.Stats:多开串城次数、最近一次现场等),
	// 形状和 /api/coverage 的 ingest 段一样。界面的串城横幅以前只读 /api/coverage,
	// 那个接口很贵、只在启动和全量之后读,运行中新出的串城最长 30 分钟才露出来;
	// 跟着每条通知带上,就和机会页同一个节奏(抓包快速重算默认每分钟)。没接 ingest 时不输出
	Ingest any `json:"ingest,omitempty"`
}

// publish 把一份新结果换成对外结果,再通知 WS 订阅者。调用方持有 evalMu:
// 结果替换和通知的先后一致,界面不会先收到新摘要、拉回来却还是旧结果。
//
// 摘要在替换之前填进结果:结果发布之后不再改,/api/scan 读到的一定带着它。
func (s *Service) publish(res *scan.Result, full bool) {
	res.Digest = scan.Digest(res)
	s.last.Store(res)

	ev := ScanEvent{
		Type:          hub.TopicScan,
		EvaluatedAt:   res.EvaluatedAt,
		StartedAt:     res.StartedAt,
		Digest:        res.Digest,
		Opportunities: len(res.Opportunities),
		Routes:        len(res.Routes),
		Full:          full,
	}
	if s.IngestStats != nil {
		ev.Ingest = s.IngestStats()
	}
	s.lastEvent.Store(&ev)
	if s.Events == nil {
		return
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("扫描通知编码失败", "err", err)
		return
	}
	// 非阻塞:连接积压就丢这一条,不会卡住发布
	s.Events.PublishTopic(hub.TopicScan, payload)
}

// LastScanEvent 是最近一次发布的那条通知,没发布过是 nil。
func (s *Service) LastScanEvent() *ScanEvent { return s.lastEvent.Load() }
