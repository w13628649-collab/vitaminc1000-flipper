//go:build pcap

// 抓包循环基于 ao-data/albiondata-client 的 client/listener.go 改写(MIT)。
package capture

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"github.com/ludy/albion-guild/internal/photon"
)

// Devices 枚举可抓的网卡。
func Devices() ([]string, error) {
	ifs, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range ifs {
		if len(d.Addresses) == 0 {
			continue // 没地址的接口抓不到东西
		}
		out = append(out, d.Name)
	}
	return out, nil
}

// Listen 在一张网卡上抓到 ctx 取消为止。
//
// 有些接口枚举得到却打不开(Windows 上基于 Wintun 的 VPN 适配器是常见例子,
// Npcap 的驱动绑在以太网/NDIS 过滤层,而 Wintun 是纯三层隧道)。
// 这种情况返回错误而不是 panic —— 调用方通常是每张网卡起一个 goroutine,
// 一张打不开不该把其他网卡上的抓包也带崩。
func Listen(ctx context.Context, opt Options, h Handler) error {
	port := opt.Port
	if port == 0 {
		port = DefaultPort
	}

	handle, err := pcap.OpenLive(opt.Device, 2048, false, pcap.BlockForever)
	if err != nil {
		return fmt.Errorf("打开 %s 抓包失败: %w", opt.Device, err)
	}
	defer handle.Close()

	// TCP 也要,因为 Photon 在 UDP 不通时会回退到 TCP
	if err := handle.SetBPFFilter(fmt.Sprintf("tcp port %d || udp port %d", port, port)); err != nil {
		return fmt.Errorf("设置过滤器失败: %w", err)
	}

	parser := photon.NewPhotonParser(h.OnRequest, h.OnResponse, h.OnEvent)
	if h.OnEncrypted != nil {
		parser.OnEncrypted = h.OnEncrypted
	}

	src := gopacket.NewPacketSource(handle, handle.LinkType())
	packets := src.Packets()
	slog.Info("开始抓包", "device", opt.Device, "port", port)

	for {
		select {
		case <-ctx.Done():
			return nil
		case pkt, ok := <-packets:
			if !ok {
				return nil // 离线 pcap 读完了
			}
			feed(parser, pkt)
		}
	}
}

func feed(parser *photon.PhotonParser, pkt gopacket.Packet) {
	// 只认 IPv4 上的 UDP/TCP 载荷,其余直接跳过
	if pkt.NetworkLayer() == nil || pkt.TransportLayer() == nil {
		return
	}
	switch t := pkt.TransportLayer().(type) {
	case *layers.UDP:
		parser.ReceivePacket(t.Payload)
	case *layers.TCP:
		parser.ReceivePacket(t.Payload)
	}
}
