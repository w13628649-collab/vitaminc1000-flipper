package capture

import (
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"albion-guild/internal/photon"
)

// feed 把一个链路层包里的 UDP/TCP 载荷交给 Photon 解析器。
//
// port > 0 时只认源或目的端口是它的包:离线回放没有 BPF,得自己过滤;
// 实时抓包时 BPF 已经滤过,传 0 跳过检查。返回这个包有没有交给解析器
func feed(parser *photon.PhotonParser, data []byte, linkType layers.LinkType, port int) bool {
	// ReadPacketData / pcapgo 每次返回新切片,NoCopy 是安全的
	pkt := gopacket.NewPacket(data, linkType, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
	// 只认 IP 上的 UDP/TCP 载荷,其余直接跳过
	if pkt.NetworkLayer() == nil || pkt.TransportLayer() == nil {
		return false
	}
	switch t := pkt.TransportLayer().(type) {
	case *layers.UDP:
		if port > 0 && int(t.SrcPort) != port && int(t.DstPort) != port {
			return false
		}
		parser.ReceivePacket(t.Payload)
	case *layers.TCP:
		if port > 0 && int(t.SrcPort) != port && int(t.DstPort) != port {
			return false
		}
		parser.ReceivePacket(t.Payload)
	default:
		return false
	}
	return true
}
