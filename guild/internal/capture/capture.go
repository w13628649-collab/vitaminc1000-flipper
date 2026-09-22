// Package capture 从网卡抓 Albion 的 Photon 流量并解析。
//
// 抓包循环基于 ao-data/albiondata-client 的 client/listener.go 改写
// (MIT,见仓库根目录 NOTICE),去掉了它的 dashboard 和自有 log 封装。
//
// 需要 libpcap(Linux/macOS)或 Npcap(Windows),所以用 build tag 隔离:
// 不带 -tags pcap 编译时走 stub,服务端因此不需要这些系统库。
package capture

// Handler 收 Photon 解析出来的三类消息。
//
// Protocol18 下真实的 opcode 不在这个 code 参数里:
// event 的真码在 params[252],operation 的在 params[253]。
// 这里原样透传,由上层去认。
type Handler struct {
	OnRequest   func(code byte, params map[byte]any)
	OnResponse  func(code byte, returnCode int16, debug string, params map[byte]any)
	OnEvent     func(code byte, params map[byte]any)
	OnEncrypted func()
}

// Options 控制一次抓包。
type Options struct {
	Device string // 网卡名;空表示自动枚举全部
	Port   int    // Albion 的 Photon 端口,默认 5056
}

const DefaultPort = 5056
