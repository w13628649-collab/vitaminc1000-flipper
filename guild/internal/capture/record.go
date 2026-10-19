package capture

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

// 录包:把过了端口过滤的原始包原样写成 .pcap。
//
// 用途有两个:一是回家录一段游戏,另一边用 ReadFile / cmd/pcapdump 完整重放、照着真实数据
// 写解码器;二是切成小段放进测试目录做回归测试——游戏一打补丁,码和参数位置会漂,
// 有真实录包的测试一跑就知道哪里漂了。
//
// 写的是标准 pcap,Wireshark 能直接打开。只有 Albion 端口的包(BPF 已经滤过),
// 一小时大约一两百 MB。

// maxRecordBytes 是单个文件的上限,到了就换下一个文件。太大的 pcap 拷来拷去不方便
const maxRecordBytes = 256 << 20

// pcapHeaderBytes 和 pcapRecordHeaderBytes 是 pcap 文件头和每条记录头的长度,算文件大小用
const (
	pcapHeaderBytes       = 24
	pcapRecordHeaderBytes = 16
)

// recorder 是一张网卡的录包器。一张网卡一个 Listen goroutine 一个 recorder,不共享,不加锁
type recorder struct {
	dir, tag string
	linkType layers.LinkType
	snaplen  uint32
	maxBytes int64
	now      func() time.Time

	f       *os.File
	w       *pcapgo.Writer
	written int64
	seq     int
	// failed 之后不再写:磁盘满了之类的错误不该拖垮抓包本身,记一条日志就停
	failed bool
}

func newRecorder(dir, device string, linkType layers.LinkType, snaplen uint32) (*recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("建录包目录 %s: %w", dir, err)
	}
	r := &recorder{dir: dir, tag: deviceTag(device), linkType: linkType, snaplen: snaplen,
		maxBytes: maxRecordBytes, now: time.Now}
	if err := r.rotate(); err != nil {
		return nil, err
	}
	return r, nil
}

// deviceTag 把网卡名压成能进文件名的一小段。Windows 上是 \Device\NPF_{GUID},
// 取 GUID 末尾几位就够区分
func deviceTag(device string) string {
	var b strings.Builder
	for _, c := range device {
		if c < unicode.MaxASCII && (unicode.IsLetter(c) || unicode.IsDigit(c)) {
			b.WriteRune(c)
		}
	}
	s := b.String()
	if len(s) > 8 {
		s = s[len(s)-8:]
	}
	if s == "" {
		s = "dev"
	}
	return s
}

func (r *recorder) rotate() error {
	if r.f != nil {
		_ = r.f.Close()
	}
	r.seq++
	name := fmt.Sprintf("albion-%s-%s-%03d.pcap", r.now().Format("20060102-150405"), r.tag, r.seq)
	f, err := os.Create(filepath.Join(r.dir, name))
	if err != nil {
		return fmt.Errorf("建录包文件: %w", err)
	}
	w := pcapgo.NewWriter(f)
	if err := w.WriteFileHeader(r.snaplen, r.linkType); err != nil {
		_ = f.Close()
		return fmt.Errorf("写 pcap 文件头: %w", err)
	}
	r.f, r.w, r.written = f, w, pcapHeaderBytes
	slog.Info("开始录包", "file", f.Name())
	return nil
}

func (r *recorder) write(ci gopacket.CaptureInfo, data []byte) {
	if r == nil || r.failed {
		return
	}
	size := int64(pcapRecordHeaderBytes + len(data))
	if r.written+size > r.maxBytes {
		if err := r.rotate(); err != nil {
			r.fail(err)
			return
		}
	}
	// pcapgo 要求 CaptureLength == len(data)
	ci.CaptureLength = len(data)
	if ci.Length < len(data) {
		ci.Length = len(data)
	}
	if err := r.w.WritePacket(ci, data); err != nil {
		r.fail(err)
		return
	}
	r.written += size
}

func (r *recorder) fail(err error) {
	r.failed = true
	slog.Warn("录包出错,这张网卡停止录包(抓包照常)", "err", err, "dir", r.dir)
	if r.f != nil {
		_ = r.f.Close()
		r.f = nil
	}
}

func (r *recorder) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
