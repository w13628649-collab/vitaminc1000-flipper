package hub

// 主题订阅:和按盘口 key 订阅并行的第二种订阅方式。
//
// 报价是"某个盘口变了",按 key 订;主题是"某类状态变了"(目前只有扫描结果
// 重新发布了),按名字订。两种消息走同一条 Client.Out() 通道、同一套背压:
// 发不进去就丢、计进 Dropped,绝不阻塞发布方。主题消息一分钟才一条,
// 和报价同样的丢弃策略下它不会比报价更容易被丢;丢了也只是晚一轮——
// 下一次发布照样会推,而且摘要不变时也照发。
//
// 协议(界面按这个写):
//   - 客户端 {"op":"sub","topics":["scan"]} 订阅,{"op":"unsub","topics":["scan"]} 取消,
//     可以和 keys 写在同一条指令里
//   - 主题消息是带 "type" 字段的 JSON 对象;报价消息({k,p,d,n,t})没有 type,
//     前端按有没有 type 区分
//   - 订阅时如果这个主题已经发布过,立刻补发最近那一条(retained)。断线重连的
//     界面不用等下一轮发布,就能拿摘要判断自己手上那份结果是不是最新的
//   - 连接积压丢过消息(报价或主题消息)时,积压排空后服务端补发一条 {"type":"resync"}
//     (ResyncMessage,不用订阅)。界面收到后对自己订的全部 key 回拉 /api/quotes,
//     再重订一次 scan 拿补发的最近一条通知。老界面按不认识的 type 忽略
//
// 主题消息只扇给本实例的连接,不走 Broadcaster/NATS:扫描是每个实例各跑各的,
// 通知说的是"本实例的 /api/scan 变了",发给连在别的实例上的界面反而是错的。

// TopicScan 是"扫描结果重新发布了"的主题:全量扫描或抓包快速重算完成后各推一条。
const TopicScan = "scan"

// knownTopics 之外的主题名一律忽略。不做这层的话,一条 64KB 的 sub 指令能往
// 每个连接的订阅表里塞几千个垃圾名字
var knownTopics = map[string]struct{}{TopicScan: {}}

// KnownTopic 说明这个主题名服务端认不认。
func KnownTopic(topic string) bool {
	_, ok := knownTopics[topic]
	return ok
}

// SubscribeTopics 给连接订上这些主题(不认识的跳过),返回实际订上的。
// 已经发布过的主题立刻补发最近一条——重复订阅也补发,当作"把当前状态再给我一次"。
func (h *Hub) SubscribeTopics(c *Client, topics []string) []string {
	var added []string
	c.mu.Lock()
	for _, t := range topics {
		if KnownTopic(t) {
			c.topics[t] = struct{}{}
			added = append(added, t)
		}
	}
	c.mu.Unlock()
	if len(added) == 0 {
		return nil
	}

	// 补发和 PublishTopic 都在 h.mu 里做:订阅和发布对同一个连接是全序的,
	// 不会出现"先补发了新的、又被一条慢了一拍的旧消息盖回去"
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range added {
		if payload, ok := h.retained[t]; ok {
			h.offerLocked(c, payload)
		}
	}
	return added
}

// UnsubscribeTopics 取消这些主题的订阅。没订过的忽略。
func (c *Client) UnsubscribeTopics(topics []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range topics {
		delete(c.topics, t)
	}
}

func (c *Client) subscribedTopic(topic string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.topics[topic]
	return ok
}

// PublishTopic 把 payload 推给订阅了 topic 的本地连接,并留作这个主题的最近一条。
// 返回推进去了几个连接。不认识的主题直接忽略。
//
// 发送是非阻塞的(和 Fanout 同一套):连接积压时丢掉这一条、计进 Dropped。
// 所以可以在调用方的锁里调——flip 就是在"发布结果"那把锁里调的,
// 通知的先后和结果替换的先后一致。
func (h *Hub) PublishTopic(topic string, payload []byte) int {
	if !KnownTopic(topic) {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.retained[topic] = payload
	sent := 0
	for c := range h.clients {
		if c.subscribedTopic(topic) && h.offerLocked(c, payload) {
			sent++
		}
	}
	return sent
}

// TopicSubscribers 是订阅了这个主题的连接数,/api/stats 排查"界面收不到推送"用。
func (h *Hub) TopicSubscribers(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for c := range h.clients {
		if c.subscribedTopic(topic) {
			n++
		}
	}
	return n
}

// offerLocked 非阻塞地往连接里塞一条,调用方持有 h.mu。
// 连接已经走了或积压满了返回 false;积压满算一次丢弃。
func (h *Hub) offerLocked(c *Client, payload []byte) bool {
	select {
	case <-c.closed:
		return false
	case c.send <- payload:
		return true
	default:
		h.Dropped++
		c.markLagged() // 丢的是扫描通知也一样:resync 之后界面重订 scan,拿到补发的最近一条
		return false
	}
}
