package capture

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

// photonEvent 拼一个只有一条可靠命令的 Photon 包,里面是事件 code、空参数表。
// 布局同 photon 包测试里的 reliablePacket:12 字节包头 + 12 字节命令头 + 载荷
func photonEvent(code byte) []byte {
	hdr := []byte{0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0} // peerId=1,1 条命令
	payload := []byte{0xF3, 4, code, 0}               // 4 = 事件,空参数表
	cmd := make([]byte, 12)
	cmd[0] = 6 // SendReliable
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+len(payload)))
	return append(append(hdr, cmd...), payload...)
}

// udpFrame 把载荷包成以太网 / IPv4 / UDP 帧
func udpFrame(t *testing.T, src, dst uint16, payload []byte) []byte {
	t.Helper()
	eth := &layers.Ethernet{SrcMAC: net.HardwareAddr{0, 1, 2, 3, 4, 5},
		DstMAC: net.HardwareAddr{6, 7, 8, 9, 10, 11}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP,
		SrcIP: net.IP{10, 0, 0, 1}, DstIP: net.IP{10, 0, 0, 2}}
	udp := &layers.UDP{SrcPort: layers.UDPPort(src), DstPort: layers.UDPPort(dst)}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload(payload)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func recordFrames(t *testing.T, dir string, frames [][]byte, at []time.Time) {
	t.Helper()
	r, err := newRecorder(dir, `\Device\NPF_{12345678-ABCD}`, layers.LinkTypeEthernet, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames {
		r.write(gopacket.CaptureInfo{Timestamp: at[i], Length: len(f)}, f)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func onlyFile(t *testing.T, dir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.pcap"))
	if len(files) != 1 {
		t.Fatalf("应该录出 1 个文件,得到 %v", files)
	}
	return files[0]
}

// 录下来的文件回放时:只有 Albion 端口的包交给解析器,时间用包上的抓包时间
func TestRecordReplay_往返(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	recordFrames(t, dir, [][]byte{
		udpFrame(t, 5056, 50000, photonEvent(3)),
		udpFrame(t, 53, 50000, []byte("不是 Albion")),
		udpFrame(t, 50000, 5056, photonEvent(29)),
	}, []time.Time{t0, t0.Add(time.Second), t0.Add(2 * time.Second)})

	path := onlyFile(t, dir)
	if base := filepath.Base(path); !strings.HasPrefix(base, "albion-") || !strings.HasSuffix(base, "-5678ABCD-001.pcap") {
		t.Fatalf("文件名应该是 albion-<时间>-<网卡>-001.pcap,得到 %s", base)
	}
	var codes []byte
	var times []time.Time
	h := Handler{OnEvent: func(c byte, _ map[byte]any) { codes = append(codes, c) }}
	st, err := ReadFile(context.Background(), path, 0, h, func(at time.Time) { times = append(times, at) })
	if err != nil {
		t.Fatal(err)
	}
	if st.Packets != 3 || st.Fed != 2 {
		t.Fatalf("统计: %+v", st)
	}
	if len(codes) != 2 || codes[0] != 3 || codes[1] != 29 {
		t.Fatalf("事件: %v", codes)
	}
	if len(times) != 3 || !times[0].Equal(t0) || !st.Last.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("时间戳: %v,统计 %+v", times, st)
	}
}

// 文件到上限换下一个,每个文件都是完整能读的 pcap
func TestRecord_到上限换文件(t *testing.T) {
	dir := t.TempDir()
	r, err := newRecorder(dir, "eth0", layers.LinkTypeEthernet, 2048)
	if err != nil {
		t.Fatal(err)
	}
	frame := udpFrame(t, 5056, 50000, photonEvent(3))
	r.maxBytes = pcapHeaderBytes + 2*int64(pcapRecordHeaderBytes+len(frame)) // 每个文件装 2 个
	for range 5 {
		r.write(gopacket.CaptureInfo{Timestamp: time.Now(), Length: len(frame)}, frame)
	}
	_ = r.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "*.pcap"))
	if len(files) != 3 {
		t.Fatalf("5 个包、每个文件 2 个,应该 3 个文件,得到 %d", len(files))
	}
	total := 0
	for _, f := range files {
		st, err := ReadFile(context.Background(), f, 0, Handler{}, nil)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		total += st.Packets
	}
	if total != 5 {
		t.Fatalf("加起来应该 5 个包,得到 %d", total)
	}
}

// 录不了(目录是个文件)要报错,不能 panic;录到一半写失败只停录包
func TestRecord_出错不拖垮抓包(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "是个文件")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newRecorder(blocker, "eth0", layers.LinkTypeEthernet, 2048); err == nil {
		t.Fatal("目录建不了应该报错")
	}
	var r *recorder
	r.write(gopacket.CaptureInfo{}, []byte{1}) // nil 录包器(没开 -record)什么都不做
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

// Wireshark 默认存 pcapng,回放要认
func TestReplay_认pcapng(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws.pcapng")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := pcapgo.NewNgWriter(f, layers.LinkTypeEthernet)
	if err != nil {
		t.Fatal(err)
	}
	frame := udpFrame(t, 5056, 50000, photonEvent(7))
	if err := w.WritePacket(gopacket.CaptureInfo{Timestamp: time.Now(), CaptureLength: len(frame),
		Length: len(frame), InterfaceIndex: 0}, frame); err != nil {
		t.Fatal(err)
	}
	_ = w.Flush()
	_ = f.Close()

	var got []byte
	st, err := ReadFile(context.Background(), path, 0, Handler{OnEvent: func(c byte, _ map[byte]any) { got = append(got, c) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fed != 1 || len(got) != 1 || got[0] != 7 {
		t.Fatalf("统计 %+v,事件 %v", st, got)
	}
}

func TestReplay_不是pcap报错(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.pcap")
	_ = os.WriteFile(path, []byte("这不是 pcap 文件,随便写点什么凑够长度"), 0o644)
	if _, err := ReadFile(context.Background(), path, 0, Handler{}, nil); err == nil {
		t.Fatal("应该报错")
	}
}

func TestDeviceTag(t *testing.T) {
	for in, want := range map[string]string{
		`\Device\NPF_{4F1C2A9E-1234-5678-9ABC-DEF012345678}`: "12345678",
		"eth0": "eth0",
		"":     "dev",
		"以太网":  "dev",
	} {
		if got := deviceTag(in); got != want {
			t.Errorf("deviceTag(%q) = %q,想要 %q", in, got, want)
		}
	}
}
