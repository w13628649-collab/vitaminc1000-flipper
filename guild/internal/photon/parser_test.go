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
