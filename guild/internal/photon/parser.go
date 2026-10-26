// 本文件来自 ao-data/albiondata-client,MIT License,
// Copyright (c) 2017 The Albion Data Project。
// 许可证全文见仓库根目录 licenses/albiondata-client-LICENSE。
package photon

import (
	"bytes"
	"encoding/binary"
	"log/slog"
)

const (
	photonHeaderLength   = 12
	commandHeaderLength  = 12
	fragmentHeaderLength = 20

	// maxPendingSegments bounds how many in-progress fragment reassemblies
	// can be tracked at once. Without this cap, a fragment lost to packet
	// loss (e.g. during a network hiccup) leaves its entry - and the
	// buffer it allocated - in pendingSegments forever, growing without
	// bound as more fragmented messages arrive. When full, the oldest
	// incomplete reassembly is evicted to make room for a new one.
	maxPendingSegments = 64

	// maxSegmentTotalLength caps the totalLength read off the wire for a
	// fragmented message, which otherwise sizes a `make([]byte, totalLen)`
	// allocation directly from unvalidated packet data.
	maxSegmentTotalLength = 8 << 20 // 8 MiB
)

// Photon command type constants
const (
	cmdDisconnect     = byte(4)
	cmdSendReliable   = byte(6)
	cmdSendUnreliable = byte(7)
	cmdSendFragment   = byte(8)
)

// Photon reliable message type constants (exported for tests)
const (
	MsgRequest  = byte(2)
	MsgResponse = byte(3)
	MsgEvent    = byte(4)
	// Some Albion builds send OperationResponse as type 7
	msgResponseAlt = byte(7)

	// msgEncryptedBit 置位表示这条消息是密文:Photon 的惯例是 0x80|kind,
	// 131 = 0x80|3 是加密的操作响应,132 就是加密的事件。
	//
	// 以前只认 msgType==131、后面的 switch 又没有 default,别的加密类型连
	// OnEncrypted 都不触发,静默消失——"有没有被加密"这个问题因此答不全
	msgEncryptedBit = byte(0x80)
)

// EncryptedPacket 是 OnEncrypted 收到的"整包加密"(包头 flags=1)。
// 那种情况下连命令都解不开,没有消息类型可报,用 0 占位(真实消息类型不会是 0)。
const EncryptedPacket = byte(0)

// Photon packet header flag values.
const (
	flagEncrypted  = byte(1)
	flagCrcEnabled = byte(0xCC)
	crcFieldLength = 4
)

type segmentedPackage struct {
	totalLength  int
	bytesWritten int
	payload      []byte

	// fragmentCount 是这条消息一共几片,received 记已经收到哪几片。
	//
	// 完整性必须按分片号判,不能靠累加字节数:重传的分片、或者
	// 越界压根没拷进去的分片,累加法照样计数,于是 bytesWritten 提前
	// 达标,把还带空洞(全零)的 payload 当成完整消息交上去解析。
	// 这两个字段本来就在包头里,以前读出来直接丢了。
	fragmentCount int
	received      map[int]struct{}
}

// complete 判断这条消息是不是收齐了。
//
// 首选按分片号数:它是协议自己的判据,不受分片长度和重传影响。
// 包头里的 fragmentCount 不可信(为 0 或离谱)时才退回按字节数判,
// 那时 received 里的去重仍然保证不会被重传骗过去。
func (s *segmentedPackage) complete() bool {
	if s.fragmentCount > 0 {
		return len(s.received) >= s.fragmentCount
	}
	return s.bytesWritten >= s.totalLength
}

// RawPacket holds the raw UDP/TCP payload of one Photon packet.
// Used for offline recording and replay.
type RawPacket struct {
	Payload []byte
}

// PhotonParser parses raw Photon UDP/TCP payloads and fires callbacks for each
// decoded operation or event. It is a port of PhotonParser.cs from
// https://github.com/JPCodeCraft/AlbionDataAvalonia.
type PhotonParser struct {
	pendingSegments map[int]*segmentedPackage
	// segmentOrder tracks insertion order of pendingSegments keys so the
	// oldest incomplete reassembly can be evicted once maxPendingSegments
	// is reached. Entries for keys already removed from pendingSegments
	// (completed or previously evicted) are skipped lazily.
	segmentOrder []int

	// OnRequest is called for every decoded OperationRequest.
	OnRequest func(operationCode byte, params map[byte]interface{})

	// OnResponse is called for every decoded OperationResponse.
	OnResponse func(operationCode byte, returnCode int16, debugMessage string, params map[byte]interface{})

	// OnEvent is called for every decoded EventData.
	OnEvent func(code byte, params map[byte]interface{})

	// OnEncrypted 遇到密文时回调。msgType 是那条消息的类型字节(0x80|kind),
	// 整包加密时是 EncryptedPacket。**带上类型是为了不误诊**:加密是按消息粒度的,
	// 一条加密事件不说明市场挂单被加密了(docs/client-data.md 第二点七节)
	OnEncrypted func(msgType byte)

	// unknown 按消息类型数没认出来的明文消息。一个 PhotonParser 只在一个抓包
	// goroutine 里用,不用加锁
	unknown map[byte]int64

	// seen 是最近处理过的可靠命令,去掉 Photon 的重发(reliableSeen)
	seen reliableSeen
}

// reliableSeenMax 是去重窗口:记最近多少条可靠命令。重发间隔几秒、最多持续几十秒,
// 这段时间里的可靠命令远少于这个数
const reliableSeenMax = 4096

// seenKey 认一条可靠命令:通道、可靠序号、命令体的哈希。
//
// 重发是逐字节一样的同一条命令,所以带上内容哈希:序号在两个方向上、换游戏服务器重连后
// 都会撞,只有序号和内容都一样才是重发 —— 那样的话丢掉它也不会少任何信息
type seenKey struct {
	channel byte
	seq     uint32
	sum     uint64
}

// reliableSeen 是可靠命令的去重窗口(FIFO 淘汰)。
//
// Photon 的可靠命令在对端没及时确认时会原样重发,抓包把每一遍都看到了。以前不去重,
// 每一遍都当新消息处理:2026-10-26 实况,进银行那条「切到 1001」的响应每 3.2 秒重发一遍,
// 人已经进了市场(1002),迟到的重发又把位置改回银行,之后翻的挂单全记到了 1001 名下,
// 精炼页只读 Lymhurst,看到的反而是前一天的老单
type reliableSeen struct {
	set  map[seenKey]struct{}
	ring []seenKey
	next int
}

// dup 报告 k 是不是窗口里见过的;没见过就记下
func (r *reliableSeen) dup(k seenKey) bool {
	if _, ok := r.set[k]; ok {
		return true
	}
	if r.set == nil {
		r.set = make(map[seenKey]struct{}, reliableSeenMax)
		r.ring = make([]seenKey, 0, reliableSeenMax)
	}
	if len(r.ring) < reliableSeenMax {
		r.ring = append(r.ring, k)
	} else {
		delete(r.set, r.ring[r.next])
		r.ring[r.next] = k
		r.next = (r.next + 1) % reliableSeenMax
	}
	r.set[k] = struct{}{}
	return false
}

// fnv64a 是命令体的哈希(FNV-1a 64 位)。只用来认重发,不用防碰撞攻击
func fnv64a(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

// UnknownMsgTypes 返回到目前为止见过、但没有处理的消息类型及条数。
func (p *PhotonParser) UnknownMsgTypes() map[byte]int64 {
	out := make(map[byte]int64, len(p.unknown))
	for k, v := range p.unknown {
		out[k] = v
	}
	return out
}

// NewPhotonParser creates a PhotonParser wired to the given callbacks.
func NewPhotonParser(
	onRequest func(byte, map[byte]interface{}),
	onResponse func(byte, int16, string, map[byte]interface{}),
	onEvent func(byte, map[byte]interface{}),
) *PhotonParser {
	return &PhotonParser{
		pendingSegments: make(map[int]*segmentedPackage),
		OnRequest:       onRequest,
		OnResponse:      onResponse,
		OnEvent:         onEvent,
	}
}

// ReceivePacket processes a raw Photon UDP/TCP payload and fires the appropriate
// callbacks. Returns true if every Photon packet found in the payload was valid.
//
// A single UDP datagram can carry more than one Photon packet back-to-back
// (e.g. due to NIC/driver receive coalescing on some users' machines), so this
// scans for packet boundaries using the peerId/challenge identity shared by
// every packet in the payload and processes each one in turn.
func (p *PhotonParser) ReceivePacket(payload []byte) bool {
	if len(payload) < photonHeaderLength {
		return false
	}

	peerID, challenge, ok := getPacketIdentity(payload, 0)
	if !ok {
		return false
	}

	firstLen, isTerminalEncrypted, ok := getPacketLength(payload, 0)
	if !ok {
		return false
	}

	if isTerminalEncrypted || firstLen == len(payload) {
		return p.receiveSinglePacket(payload)
	}

	type packetRange struct{ offset, length int }
	ranges := []packetRange{{0, firstLen}}
	off := firstLen
	framingOK := true

	for off < len(payload) {
		if !matchesIdentity(payload, off, peerID, challenge) {
			framingOK = false
			break
		}
		length, isTerminal, valid := getPacketLength(payload, off)
		if !valid {
			framingOK = false
			break
		}
		ranges = append(ranges, packetRange{off, length})
		off += length
		if isTerminal {
			break
		}
	}

	success := framingOK
	for _, r := range ranges {
		if !p.receiveSinglePacket(payload[r.offset : r.offset+r.length]) {
			success = false
		}
	}
	return success
}

// receiveSinglePacket parses exactly one Photon packet (header + commands).
func (p *PhotonParser) receiveSinglePacket(payload []byte) bool {
	if len(payload) < photonHeaderLength {
		return false
	}

	offset := 2 // skip peerId (2 bytes)
	flags := payload[offset]
	offset++
	commandCount := int(payload[offset])
	offset++
	offset += 8 // skip timestamp (4) + challenge (4)

	if flags == flagEncrypted {
		if p.OnEncrypted != nil {
			p.OnEncrypted(EncryptedPacket)
		}
		return false
	}

	if flags == flagCrcEnabled {
		if !available(payload, offset, crcFieldLength) {
			return false
		}
		crc := binary.BigEndian.Uint32(payload[offset:])
		offset += crcFieldLength

		crcPayload := append([]byte(nil), payload...)
		for i := photonHeaderLength; i < photonHeaderLength+crcFieldLength; i++ {
			crcPayload[i] = 0
		}
		if crc != calculateCrc(crcPayload) {
			return false
		}
	}

	for i := 0; i < commandCount; i++ {
		newOffset, ok := p.handleCommand(payload, offset)
		if !ok {
			return false
		}
		offset = newOffset
	}

	return offset == len(payload)
}

// getPacketIdentity reads the peerId/challenge pair that is common to every
// Photon packet coalesced into the same UDP payload.
func getPacketIdentity(payload []byte, offset int) (peerID int16, challenge int32, ok bool) {
	if !available(payload, offset, photonHeaderLength) {
		return 0, 0, false
	}
	peerID = int16(binary.BigEndian.Uint16(payload[offset:]))
	challenge = int32(binary.BigEndian.Uint32(payload[offset+8:]))
	return peerID, challenge, true
}

func matchesIdentity(payload []byte, offset int, expectedPeerID int16, expectedChallenge int32) bool {
	peerID, challenge, ok := getPacketIdentity(payload, offset)
	return ok && peerID == expectedPeerID && challenge == expectedChallenge
}

// getPacketLength scans the packet starting at offset (header + its declared
// commands) to determine where it ends, without decoding command contents.
// isTerminalEncrypted reports an encrypted packet, which has no discoverable
// length beyond "the rest of the payload" and must be the last packet.
func getPacketLength(payload []byte, offset int) (length int, isTerminalEncrypted bool, ok bool) {
	if !available(payload, offset, photonHeaderLength) {
		return 0, false, false
	}

	flags := payload[offset+2]
	if flags == flagEncrypted {
		return len(payload) - offset, true, true
	}

	headerLen := photonHeaderLength
	if flags == flagCrcEnabled {
		headerLen += crcFieldLength
	}
	if !available(payload, offset, headerLen) {
		return 0, false, false
	}

	commandCount := int(payload[offset+3])
	if commandCount == 0 {
		return 0, false, false
	}

	cmdOffset := offset + headerLen
	for i := 0; i < commandCount; i++ {
		if !available(payload, cmdOffset, commandHeaderLength) {
			return 0, false, false
		}
		cmdLen := int(binary.BigEndian.Uint32(payload[cmdOffset+4:]))
		if cmdLen < commandHeaderLength || !available(payload, cmdOffset, cmdLen) {
			return 0, false, false
		}
		cmdOffset += cmdLen
	}

	length = cmdOffset - offset
	if length < headerLen {
		return 0, false, false
	}
	return length, false, true
}

// calculateCrc reproduces Photon's CRC32 variant (standard IEEE polynomial,
// no final XOR) used to validate CRC-enabled packets (flags == 0xCC).
func calculateCrc(data []byte) uint32 {
	const key = uint32(3988292384)
	result := uint32(0xFFFFFFFF)
	for _, b := range data {
		result ^= uint32(b)
		for j := 0; j < 8; j++ {
			if result&1 != 0 {
				result = result>>1 ^ key
			} else {
				result >>= 1
			}
		}
	}
	return result
}

func (p *PhotonParser) handleCommand(src []byte, offset int) (int, bool) {
	if !available(src, offset, commandHeaderLength) {
		return offset, false
	}

	cmdType := src[offset]
	offset++
	channel := src[offset]
	offset++
	offset++ // commandFlags
	offset++ // reserved byte
	cmdLen := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	seq := binary.BigEndian.Uint32(src[offset:])
	offset += 4
	cmdLen -= commandHeaderLength

	if cmdLen < 0 || !available(src, offset, cmdLen) {
		return offset, false
	}

	// 可靠命令(含分片)的重发原样丢掉,见 reliableSeen。不可靠命令不会重发
	if cmdType == cmdSendReliable || cmdType == cmdSendFragment {
		if p.seen.dup(seenKey{channel, seq, fnv64a(src[offset : offset+cmdLen])}) {
			return offset + cmdLen, true
		}
	}

	switch cmdType {
	case cmdDisconnect:
		return offset + cmdLen, true

	case cmdSendUnreliable:
		if cmdLen < 4 {
			return offset + cmdLen, false
		}
		offset += 4
		cmdLen -= 4
		newOffset, _ := p.handleSendReliable(src, offset, cmdLen)
		return newOffset, true

	case cmdSendReliable:
		newOffset, _ := p.handleSendReliable(src, offset, cmdLen)
		return newOffset, true

	case cmdSendFragment:
		return p.handleSendFragment(src, offset, cmdLen), true

	default:
		return offset + cmdLen, true
	}
}

func (p *PhotonParser) handleSendReliable(src []byte, offset, cmdLen int) (int, bool) {
	if cmdLen < 2 || !available(src, offset, cmdLen) {
		return offset + cmdLen, false
	}

	/* signalByte := src[offset] */
	offset++
	msgType := src[offset]
	offset++
	cmdLen -= 2

	if !available(src, offset, cmdLen) {
		return offset + cmdLen, false
	}

	if msgType&msgEncryptedBit != 0 {
		if p.OnEncrypted != nil {
			p.OnEncrypted(msgType)
		}
		return offset + cmdLen, true
	}

	data := src[offset : offset+cmdLen]
	offset += cmdLen

	switch msgType {
	case MsgRequest:
		p.dispatchRequest(data)
	case MsgResponse, msgResponseAlt:
		p.dispatchResponse(data)
	case MsgEvent:
		p.dispatchEvent(data)
	default:
		// 内部操作(6)、纯消息(8/9)之类。不处理,但要数得出来:
		// 哪天市场数据换了个消息类型,这里是唯一能看出来的地方
		if p.unknown == nil {
			p.unknown = map[byte]int64{}
		}
		if p.unknown[msgType] == 0 {
			slog.Debug("没处理的 Photon 消息类型", "msg_type", msgType, "len", len(data))
		}
		p.unknown[msgType]++
	}

	return offset, true
}

func (p *PhotonParser) dispatchRequest(data []byte) {
	if len(data) < 1 {
		return
	}
	opCode := data[0]
	params := deserializeParameterTable(data[1:])
	if p.OnRequest != nil {
		p.OnRequest(opCode, params)
	}
}

func (p *PhotonParser) dispatchResponse(data []byte) {
	if len(data) < 3 {
		return
	}
	opCode := data[0]
	returnCode := int16(binary.LittleEndian.Uint16(data[1:3]))

	// Per Protocol18Deserializer.DeserializeOperationResponse: after returnCode,
	// if bytes remain read one type byte + its value (the debug message), then
	// the parameter table.
	buf := bytes.NewBuffer(data[3:])
	debugMsg := ""

	if buf.Len() > 0 {
		tc, _ := buf.ReadByte()
		val := deserialize(buf, tc)
		switch v := val.(type) {
		case string:
			debugMsg = v
		case []string:
			// Albion embeds market-order data as a typed string array in the
			// position where the debug message would normally be.
			// Surface it as params[0] so the listener can identify and route it.
			params := map[byte]interface{}{0: v}
			if p.OnResponse != nil {
				p.OnResponse(opCode, returnCode, "", params)
			}
			return
		}
	}

	params := readParameterTable(buf)
	if p.OnResponse != nil {
		p.OnResponse(opCode, returnCode, debugMsg, params)
	}
}

func (p *PhotonParser) dispatchEvent(data []byte) {
	if len(data) < 1 {
		return
	}
	code := data[0]
	params := deserializeParameterTable(data[1:])
	if p.OnEvent != nil {
		p.OnEvent(code, params)
	}
}

func (p *PhotonParser) handleSendFragment(src []byte, offset, cmdLen int) int {
	if cmdLen < fragmentHeaderLength || !available(src, offset, fragmentHeaderLength) {
		return offset + cmdLen
	}

	startSeq := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragCount := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragNumber := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	totalLen := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragOffset := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4

	fragLen := cmdLen
	if fragLen < 0 || !available(src, offset, fragLen) {
		return offset + fragLen
	}

	seg, ok := p.pendingSegments[startSeq]
	if !ok {
		if totalLen <= 0 || totalLen > maxSegmentTotalLength {
			// Corrupt or hostile totalLength - refuse to allocate for it.
			return offset + fragLen
		}

		if len(p.pendingSegments) >= maxPendingSegments {
			p.evictOldestSegment()
		}

		seg = &segmentedPackage{
			totalLength:   totalLen,
			payload:       make([]byte, totalLen),
			fragmentCount: fragCount,
			received:      make(map[int]struct{}),
		}
		p.pendingSegments[startSeq] = seg
		p.segmentOrder = append(p.segmentOrder, startSeq)
	}

	end := fragOffset + fragLen
	fitted := fragOffset >= 0 && end <= len(seg.payload)
	if fitted {
		copy(seg.payload[fragOffset:end], src[offset:offset+fragLen])
	}
	offset += fragLen

	// 只有"这一片是新的,而且确实拷进去了"才算数。
	// 重传的片不重复计数,越界没拷进去的片不冒充已写入
	if _, dup := seg.received[fragNumber]; !dup && fitted {
		seg.received[fragNumber] = struct{}{}
		seg.bytesWritten += fragLen
	}

	if seg.complete() {
		delete(p.pendingSegments, startSeq)
		p.handleSendReliable(seg.payload, 0, len(seg.payload))
	}

	return offset
}

// evictOldestSegment drops the oldest incomplete fragment reassembly to
// make room for a new one once maxPendingSegments is reached.
func (p *PhotonParser) evictOldestSegment() {
	for len(p.segmentOrder) > 0 {
		oldest := p.segmentOrder[0]
		p.segmentOrder = p.segmentOrder[1:]
		if _, exists := p.pendingSegments[oldest]; exists {
			delete(p.pendingSegments, oldest)
			return
		}
	}
}

// available reports whether src[offset:offset+count] is in bounds.
func available(src []byte, offset, count int) bool {
	return count >= 0 && offset >= 0 && len(src)-offset >= count
}
