package capture

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"albion-guild/internal/photon"
)

// ReplayStats 是一次离线回放的统计。
type ReplayStats struct {
	Packets     int // 文件里一共多少个包
	Fed         int // 过了端口过滤、交给 Photon 解析器的
	First, Last time.Time
}

// ReadFile 把一个 .pcap / .pcapng 文件按抓包顺序喂进解析器(离线回放)。
//
// 纯 Go 读文件(pcapgo),不需要 libpcap,所以不受 pcap 编译标签限制,测试在哪都能跑。
// onPacket 在每个包交给解析器之前回调它的抓包时间:回放时让上层用包上的时间
// 而不是墙钟,不然一个月前录的挂单会被当成刚看到的
func ReadFile(ctx context.Context, path string, port int, h Handler, onPacket func(time.Time)) (ReplayStats, error) {
	var st ReplayStats
	if port == 0 {
		port = DefaultPort
	}
	f, err := os.Open(path)
	if err != nil {
		return st, err
	}
	defer f.Close()

	src, linkType, err := packetReader(bufio.NewReader(f))
	if err != nil {
		return st, fmt.Errorf("%s: %w", path, err)
	}
	parser := photon.NewPhotonParser(h.OnRequest, h.OnResponse, h.OnEvent)
	if h.OnEncrypted != nil {
		parser.OnEncrypted = h.OnEncrypted
	}
	for {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		data, ci, err := src.ReadPacketData()
		if errors.Is(err, io.EOF) {
			return st, nil
		}
		if err != nil {
			return st, fmt.Errorf("%s 第 %d 个包: %w", path, st.Packets+1, err)
		}
		st.Packets++
		if st.First.IsZero() {
			st.First = ci.Timestamp
		}
		st.Last = ci.Timestamp
		if onPacket != nil {
			onPacket(ci.Timestamp)
		}
		if feed(parser, data, linkType, port) {
			st.Fed++
		}
	}
}

// packetReader 按魔数认 pcap 还是 pcapng。Wireshark 默认存 pcapng,我们录的是 pcap
func packetReader(r *bufio.Reader) (gopacket.PacketDataSource, layers.LinkType, error) {
	magic, err := r.Peek(4)
	if err != nil {
		return nil, 0, fmt.Errorf("读文件头: %w", err)
	}
	if magic[0] == 0x0A && magic[1] == 0x0D && magic[2] == 0x0D && magic[3] == 0x0A {
		ng, err := pcapgo.NewNgReader(r, pcapgo.DefaultNgReaderOptions)
		if err != nil {
			return nil, 0, fmt.Errorf("不是合法的 pcapng: %w", err)
		}
		return ng, ng.LinkType(), nil
	}
	pr, err := pcapgo.NewReader(r)
	if err != nil {
		return nil, 0, fmt.Errorf("不是合法的 pcap: %w", err)
	}
	return pr, pr.LinkType(), nil
}
