// 本文件来自 ao-data/albiondata-client,MIT License,
// Copyright (c) 2017 The Albion Data Project。
// 许可证全文见仓库根目录 licenses/albiondata-client-LICENSE。
package photon

import (
	"encoding/binary"
	"testing"
)

// buildFragmentPacket wraps a single SendFragment command into a minimal
// valid Photon UDP packet (12-byte header + 12-byte command header +
// 20-byte fragment header + data).
func buildFragmentPacket(startSeq, fragCount, fragNum, totalLen, fragOffset uint32, data []byte) []byte {
	hdr := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}

	fragHdr := make([]byte, fragmentHeaderLength)
	binary.BigEndian.PutUint32(fragHdr[0:], startSeq)
	binary.BigEndian.PutUint32(fragHdr[4:], fragCount)
	binary.BigEndian.PutUint32(fragHdr[8:], fragNum)
	binary.BigEndian.PutUint32(fragHdr[12:], totalLen)
	binary.BigEndian.PutUint32(fragHdr[16:], fragOffset)

	cmdData := append(fragHdr, data...)

	cmdHdr := make([]byte, commandHeaderLength)
	cmdHdr[0] = cmdSendFragment
	binary.BigEndian.PutUint32(cmdHdr[4:], uint32(commandHeaderLength+len(cmdData)))

	pkt := append(append([]byte{}, hdr...), cmdHdr...)
	pkt = append(pkt, cmdData...)
	return pkt
}

// TestPendingSegments_BoundedByCap verifies that incomplete fragment
// reassembly (e.g. because a fragment was lost to packet loss during a
// network hiccup) doesn't grow pendingSegments without bound - each
// abandoned start-of-fragment allocates a totalLen-sized buffer that is
// never released otherwise.
func TestPendingSegments_BoundedByCap(t *testing.T) {
	parser := NewPhotonParser(nil, nil, nil)

	for i := 0; i < maxPendingSegments*4; i++ {
		// Send only the first fragment of a 2-fragment message, so it never
		// completes and stays in pendingSegments.
		pkt := buildFragmentPacket(uint32(i), 2, 0, 20, 0, make([]byte, 10))
		parser.ReceivePacket(pkt)
	}

	if len(parser.pendingSegments) > maxPendingSegments {
		t.Fatalf("pendingSegments grew to %d entries, want capped at %d", len(parser.pendingSegments), maxPendingSegments)
	}
}

// TestPendingSegments_RejectsOversizedTotalLength verifies a corrupt or
// hostile totalLen field (read straight off the wire) can't force a huge
// allocation.
func TestPendingSegments_RejectsOversizedTotalLength(t *testing.T) {
	parser := NewPhotonParser(nil, nil, nil)

	pkt := buildFragmentPacket(1, 2, 0, maxSegmentTotalLength+1, 0, make([]byte, 10))
	parser.ReceivePacket(pkt)

	if len(parser.pendingSegments) != 0 {
		t.Fatalf("pendingSegments has %d entries, want 0 for an oversized totalLen", len(parser.pendingSegments))
	}
}

// reliablePacket 拼一个只含一条 SendReliable 命令的 Photon 包:
// 12 字节包头 + 12 字节命令头 + signal 字节 + msgType + body。
func reliablePacket(msgType byte, body []byte) []byte {
	hdr := []byte{0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0} // peerId=1, flags=0, 1 条命令
	cmd := make([]byte, commandHeaderLength)
	cmd[0] = cmdSendReliable
	payload := append([]byte{0xF3, msgType}, body...)
	binary.BigEndian.PutUint32(cmd[4:], uint32(commandHeaderLength+len(payload)))
	pkt := append(append(hdr, cmd...), payload...)
	return pkt
}

// 131 = 0x80|3 是加密的操作响应,要带着类型报上去
func TestEncrypted_131带类型回调(t *testing.T) {
	var got []byte
	p := NewPhotonParser(nil, nil, nil)
	p.OnEncrypted = func(mt byte) { got = append(got, mt) }
	if !p.ReceivePacket(reliablePacket(131, []byte{1, 2, 3, 4})) {
		t.Fatal("加密命令本身是合法的包,应该返回 true")
	}
	if len(got) != 1 || got[0] != 131 {
		t.Fatalf("应回调一次 131,得到 %v", got)
	}
}

// 以前只认 131,132(加密事件)之类既不触发 OnEncrypted、switch 又没有
// default,静默消失。现在高位置位的一律当密文
func TestEncrypted_别的0x80类型也要报(t *testing.T) {
	var got []byte
	p := NewPhotonParser(nil, nil, nil)
	p.OnEncrypted = func(mt byte) { got = append(got, mt) }
	for _, mt := range []byte{0x80 | MsgEvent, 0x80 | MsgRequest, 0x80 | msgResponseAlt} {
		p.ReceivePacket(reliablePacket(mt, []byte{9, 9}))
	}
	if string(got) != string([]byte{0x84, 0x82, 0x87}) {
		t.Fatalf("得到 %x", got)
	}
	// 密文不能被当成明文消息派发出去
	p.OnEvent = func(byte, map[byte]any) { t.Fatal("密文事件被当明文派发了") }
	p.ReceivePacket(reliablePacket(0x80|MsgEvent, []byte{1, 0}))
}

// 整包加密(包头 flags=1)没有消息类型可报,用 EncryptedPacket 占位
func TestEncrypted_整包加密报EncryptedPacket(t *testing.T) {
	var got []byte
	p := NewPhotonParser(nil, nil, nil)
	p.OnEncrypted = func(mt byte) { got = append(got, mt) }
	pkt := reliablePacket(MsgEvent, []byte{1, 0})
	pkt[2] = flagEncrypted
	p.ReceivePacket(pkt)
	if len(got) != 1 || got[0] != EncryptedPacket {
		t.Fatalf("得到 %v", got)
	}
}

// 没处理的明文消息类型要数得出来,不能静默消失
func TestUnknownMsgType_计数(t *testing.T) {
	p := NewPhotonParser(nil, nil, nil)
	p.OnEncrypted = func(byte) { t.Fatal("明文的未知类型不是密文") }
	p.ReceivePacket(reliablePacket(8, []byte{1}))
	p.ReceivePacket(reliablePacket(8, []byte{1}))
	p.ReceivePacket(reliablePacket(6, []byte{1}))
	got := p.UnknownMsgTypes()
	if got[8] != 2 || got[6] != 1 || len(got) != 2 {
		t.Fatalf("得到 %v", got)
	}
}

// 明文事件照常派发:加了 default 不能把正常路径改坏
func TestReliable_明文事件照常派发(t *testing.T) {
	var code byte = 0xFF
	p := NewPhotonParser(nil, nil, func(c byte, _ map[byte]any) { code = c })
	p.ReceivePacket(reliablePacket(MsgEvent, []byte{3, 0})) // 事件码 3,空参数表
	if code != 3 {
		t.Fatalf("事件没派发,code=%d", code)
	}
}

// frag 拼一条分片命令:20 字节头 + 载荷。
// 头是 startSeq / fragmentCount / fragmentNumber / totalLength / fragmentOffset。
func frag(startSeq, count, number, totalLen, fragOffset uint32, body []byte) []byte {
	b := make([]byte, 20, 20+len(body))
	binary.BigEndian.PutUint32(b[0:], startSeq)
	binary.BigEndian.PutUint32(b[4:], count)
	binary.BigEndian.PutUint32(b[8:], number)
	binary.BigEndian.PutUint32(b[12:], totalLen)
	binary.BigEndian.PutUint32(b[16:], fragOffset)
	return append(b, body...)
}

// 分片完整性必须按分片号判,不能靠累加字节数。
//
// 重传在有丢包的网络上很常见。累加法会让重复的那一片也计数,
// bytesWritten 提前达标,于是把还带空洞(全零)的 payload 当成
// 完整消息交上去解析——解出来的挂单价格是错的,而且一条日志都不会打。
func TestFragment_重传的分片不能让它提前判完整(t *testing.T) {
	p := NewPhotonParser(nil, nil, nil)

	first := frag(1, 2, 0, 8, 0, []byte("AAAA"))
	p.handleSendFragment(first, 0, len(first))

	seg, ok := p.pendingSegments[1]
	if !ok {
		t.Fatal("收到第一片之后应该还在等第二片")
	}
	if seg.bytesWritten != 4 {
		t.Fatalf("已写入 %d 字节,应该是 4", seg.bytesWritten)
	}

	// 同一片再来一次(重传)
	p.handleSendFragment(first, 0, len(first))

	if _, still := p.pendingSegments[1]; !still {
		t.Fatal("重传的分片让它以为收齐了——带空洞的 payload 被当成完整消息交出去了")
	}
	if seg.bytesWritten != 4 {
		t.Fatalf("重传之后已写入 %d 字节,重复的那片不该计数", seg.bytesWritten)
	}

	// 真正的第二片
	second := frag(1, 2, 1, 8, 4, []byte("BBBB"))
	p.handleSendFragment(second, 0, len(second))

	if _, still := p.pendingSegments[1]; still {
		t.Fatal("两片都到了,应该判完整并交出去")
	}
}

// 越界的分片压根没拷进 payload,不能冒充"已写入"。
func TestFragment_越界的分片不计数(t *testing.T) {
	p := NewPhotonParser(nil, nil, nil)

	good := frag(2, 3, 0, 12, 0, []byte("AAAA"))
	p.handleSendFragment(good, 0, len(good))

	// fragmentOffset 指到 payload 之外,copy 会被跳过
	bad := frag(2, 3, 1, 12, 999, []byte("BBBB"))
	p.handleSendFragment(bad, 0, len(bad))

	seg := p.pendingSegments[2]
	if seg == nil {
		t.Fatal("越界的分片不该让它判完整")
	}
	if seg.bytesWritten != 4 {
		t.Fatalf("已写入 %d 字节,越界那片没拷进去就不该算", seg.bytesWritten)
	}
}

// 包头里的 fragmentCount 不可信时(为 0),退回按字节数判,
// 但去重仍然有效,不会被重传骗过去。
func TestFragment_分片数为零时退回按字节判(t *testing.T) {
	p := NewPhotonParser(nil, nil, nil)

	a := frag(3, 0, 0, 8, 0, []byte("AAAA"))
	p.handleSendFragment(a, 0, len(a))
	p.handleSendFragment(a, 0, len(a)) // 重传
	if _, still := p.pendingSegments[3]; !still {
		t.Fatal("即使按字节数判,重传也不该让它提前完整")
	}
	b := frag(3, 0, 1, 8, 4, []byte("BBBB"))
	p.handleSendFragment(b, 0, len(b))
	if _, still := p.pendingSegments[3]; still {
		t.Fatal("两片齐了应该判完整")
	}
}
