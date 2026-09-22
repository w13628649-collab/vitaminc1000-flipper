//go:build !pcap

package capture

import (
	"context"
	"errors"
)

// ErrNotBuilt 说明这个二进制编译时没带抓包能力。
//
// 服务端不需要 libpcap/Npcap,所以默认不编译抓包那部分。
// 客户端构建时加 -tags pcap。
var ErrNotBuilt = errors.New("这个二进制编译时未启用抓包(需要 -tags pcap)")

func Devices() ([]string, error) { return nil, ErrNotBuilt }

func Listen(context.Context, Options, Handler) error { return ErrNotBuilt }
