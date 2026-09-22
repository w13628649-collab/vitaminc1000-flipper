//go:build pcap

// 抓包循环基于 ao-data/albiondata-client 的 client/listener.go 改写(MIT)。
package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"albion-guild/internal/photon"
)

// readTimeout 决定没有包时多久醒一次。
//
// 不能用 pcap.BlockForever:那样读操作一直卡在驱动里,退出时
// handle.Close() 拿不到锁,整个进程挂死在那儿不退。
// 醒得勤一点只是多几次空转,换来的是能干净地关掉。
const readTimeout = 500 * time.Millisecond

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

	handle, err := open(opt.Device)
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

	linkType := handle.LinkType()
	slog.Info("开始抓包", "device", opt.Device, "port", port)

	// 自己读,不用 gopacket.PacketSource。PacketSource 会另起一个
	// goroutine 读包往 channel 里塞,ctx 取消后那个 goroutine 还卡在
	// 读操作上,Close() 就永远等不到——这是退出挂死的根因。
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		data, _, err := handle.ReadPacketData()
		switch {
		case err == nil:
		case errors.Is(err, pcap.NextErrorTimeoutExpired):
			continue // 这段时间没包,正常
		case errors.Is(err, io.EOF), errors.Is(err, pcap.NextErrorNoMorePackets):
			return nil // 离线 pcap 读完了
		default:
			// 网卡被拔掉、驱动被卸载之类。报上去,别静默死掉
			return fmt.Errorf("%s 读包失败: %w", opt.Device, err)
		}

		// ReadPacketData 每次返回新切片,NoCopy 是安全的
		feed(parser, gopacket.NewPacket(data, linkType,
			gopacket.DecodeOptions{Lazy: true, NoCopy: true}))
	}
}

// open 打开网卡。
//
// 不用 pcap.OpenLive,因为它没法开 immediate mode。不开的话
// libpcap 会攒够一缓冲区才把包交上来,读超时在没流量时根本不触发
// (Linux 上尤其明显),退出就卡在读操作里出不来。
func open(device string) (*pcap.Handle, error) {
	inactive, err := pcap.NewInactiveHandle(device)
	if err != nil {
		return nil, err
	}
	defer inactive.CleanUp()

	if err := inactive.SetSnapLen(2048); err != nil {
		return nil, err
	}
	if err := inactive.SetPromisc(false); err != nil {
		return nil, err
	}
	if err := inactive.SetTimeout(readTimeout); err != nil {
		return nil, err
	}
	// 来一个包交一个包。行情要的就是这个即时性,顺带让读超时真的生效
	if err := inactive.SetImmediateMode(true); err != nil {
		return nil, err
	}
	return inactive.Activate()
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
