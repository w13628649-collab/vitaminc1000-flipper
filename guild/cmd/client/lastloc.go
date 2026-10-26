package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// lastLocation 是最后一次抓到的位置,存在配置目录里。
//
// 城市挂单的地点在包里是空的,靠「进了哪个区」补;客户端开在游戏中途、角色一直没切区的话,
// 解析器不知道在哪,挂单只能先扣着(protocol.Parser.AssumeLocation)。2026-10-26 用户重开客户端后
// 在市场里点了一晚上、一条都没传。存下上次的位置,界面就能问一句「还在 X 吗」,点一下补发。
// 只当建议给人确认,不自动套用:客户端关着的时候人可能已经换了城
type lastLocation struct {
	Character string    `json:"character"`
	Location  string    `json:"location"`
	At        time.Time `json:"at"`
}

const lastLocationFile = "last_location.json"

var lastLocMu sync.Mutex

func loadLastLocation() *lastLocation {
	b, err := os.ReadFile(filepath.Join(configDir(), lastLocationFile))
	if err != nil {
		return nil
	}
	var l lastLocation
	if json.Unmarshal(b, &l) != nil || l.Location == "" {
		return nil
	}
	return &l
}

// saveLastLocation 先写临时文件再改名:几张网卡的抓包 goroutine 都可能触发,写到一半被读到也不会是半截
func saveLastLocation(l lastLocation) {
	b, err := json.Marshal(l)
	if err != nil {
		return
	}
	lastLocMu.Lock()
	defer lastLocMu.Unlock()
	dir := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp := filepath.Join(dir, lastLocationFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		slog.Debug("记位置失败", "err", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, lastLocationFile)); err != nil {
		slog.Debug("记位置失败", "err", err)
	}
}
