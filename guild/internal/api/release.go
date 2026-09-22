package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 能下载的文件白名单。
//
// 不做白名单、直接 FileServer 一个目录的话,会白送一个目录列表出去,
// 而且路径拼错就变成任意文件读取。发布目录里将来还会有别的东西,
// 明确列出来最省心。
var downloadable = map[string]string{
	"flipper-client.exe": "application/octet-stream",
	"flipper-client.zip": "application/zip",
	"SHA256.txt":         "text/plain; charset=utf-8",
	"使用说明.txt":           "text/plain; charset=utf-8",
}

// Release 是更新 manifest。客户端启动时拉一次,比对版本。
type Release struct {
	Version string `json:"version"`
	// URL 是相对路径,客户端拼上自己配的服务端地址。
	// 写死绝对地址的话,换个部署环境(内网/公网/端口变了)就全错
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// ServerMinVersion 是服务端能接受的最低客户端版本。
	// 协议改了之后老客户端传上来的数据可能是错的,宁可让它更新
	ServerMinVersion string `json:"server_min_version"`
	ReleasedAt       string `json:"released_at"`
	Notes            string `json:"notes,omitempty"`
}

// releaseCache 缓存算出来的 sha256。11MB 每次请求都重算太浪费,
// 而文件只在发版时才换,按 mtime+size 判断是否失效就够了。
type releaseCache struct {
	mu      sync.Mutex
	key     string
	sha     string
	size    int64
	modTime time.Time
}

func (c *releaseCache) get(path string) (sha string, size int64, mod time.Time, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	key := st.ModTime().UTC().Format(time.RFC3339Nano) + "/" + itoa(st.Size())

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key {
		return c.sha, c.size, c.modTime, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, time.Time{}, err
	}
	c.key, c.sha, c.size, c.modTime = key, hex.EncodeToString(h.Sum(nil)), st.Size(), st.ModTime()
	return c.sha, c.size, c.modTime, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// registerRelease 挂发布相关的路由。ReleaseDir 没配就整组不注册——
// 开发时不想管发布目录,服务端照样能起。
func (s *Server) registerRelease(mux *http.ServeMux) {
	if s.ReleaseDir == "" {
		return
	}
	mux.HandleFunc("GET /api/release", s.handleRelease)
	mux.HandleFunc("GET /download/{name}", s.handleDownload)
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	exe := filepath.Join(s.ReleaseDir, "flipper-client.exe")
	sha, size, mod, err := s.releases.get(exe)
	if err != nil {
		http.Error(w, "还没有发布任何版本", http.StatusNotFound)
		return
	}
	rel := Release{
		Version:          s.ReleaseVersion,
		URL:              "/download/flipper-client.exe",
		SHA256:           sha,
		Size:             size,
		ServerMinVersion: s.MinClientVersion,
		ReleasedAt:       mod.UTC().Format(time.RFC3339),
	}
	if notes, err := os.ReadFile(filepath.Join(s.ReleaseDir, "RELEASE.txt")); err == nil {
		rel.Notes = strings.TrimSpace(string(notes))
	}
	// 二进制换了 manifest 就得跟着变,不能缓存
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(rel)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctype, ok := downloadable[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.ReleaseDir, name)

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache")
	// 公司网络下 11MB 断一次很常见。ServeFile 自带 Range 续传,
	// 比自己 io.Copy 强得多——千万别图省事换掉
	http.ServeFile(w, r, path)
}
