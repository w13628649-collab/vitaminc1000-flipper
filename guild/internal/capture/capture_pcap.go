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

	"github.com/google/gopacket/pcap"

	"albion-guild/internal/photon"
)

// readTimeout 决定没有包时多久醒一次。
//
// 不能用 pcap.BlockForever:那样读操作一直卡在驱动里,退出时
// handle.Close() 拿不到锁,整个进程挂死在那儿不退。
// 醒得勤一点只是多几次空转,换来的是能干净地关掉。
const readTimeout = 500 * time.Millisecond

// Devices 枚举可抓的网卡,按名字归好类(categorize.go)。
func Devices() ([]Device, error) {
	ifs, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, d := range ifs {
		if len(d.Addresses) == 0 {
			continue // 没地址的接口抓不到东西
		}
		out = append(out, Device{Name: d.Name, Description: d.Description,
			Category: Categorize(d.Name, d.Description)})
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

	var rec *recorder
	if opt.RecordDir != "" {
		// 录不了不影响抓包:目录建不了之类的只记一条日志
		if rec, err = newRecorder(opt.RecordDir, opt.Device, linkType, uint32(handle.SnapLen())); err != nil {
			slog.Warn("录包开不起来,只抓不录", "device", opt.Device, "err", err)
			rec = nil
		}
		defer rec.Close()
	}

	// 自己读,不用 gopacket.PacketSource。PacketSource 会另起一个
	// goroutine 读包往 channel 里塞,ctx 取消后那个 goroutine 还卡在
	// 读操作上,Close() 就永远等不到——这是退出挂死的根因。
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		data, ci, err := handle.ReadPacketData()
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

		rec.write(ci, data)
		feed(parser, data, linkType, 0) // BPF 已经按端口滤过
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
