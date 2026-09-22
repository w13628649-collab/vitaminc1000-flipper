package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	c.mu.Lock()
	defer c.mu.Unlock()

	// 先看缓存是否还有效。注意 Stat 只用来判断"要不要重算",
	// 真正的 size 和 mtime 从**打开后的那个文件句柄**上取——
	// 先 Stat 后 Open 的话,发布的瞬间会给出 sha 是新文件、
	// size 是旧文件的 manifest,客户端校验必然失败
	if st, serr := os.Stat(path); serr == nil && c.key == cacheKey(st) {
		return c.sha, c.size, c.modTime, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	st, err := f.Stat()
	if err != nil {
		return "", 0, time.Time{}, err
	}
	// 读完之后文件又变了(发布脚本正好在这一刻覆盖)。这一轮的结果
	// 已经不可信,不缓存,让下次请求重算
	if st.Size() != n {
		return "", 0, time.Time{}, errShifting
	}
	c.key, c.sha, c.size, c.modTime = cacheKey(st), hex.EncodeToString(h.Sum(nil)), n, st.ModTime()
	return c.sha, c.size, c.modTime, nil
}

// cacheKey 拿 mtime + size 判缓存是否失效。
//
// 理论上保留时间戳的发布流程(rsync -t、tar -p)能骗过它,
// 但我们的 release.sh 是 cp,mtime 必变。真要防死得每次全量重算,
// 13MB 的哈希不值当。
func cacheKey(st os.FileInfo) string {
	return st.ModTime().UTC().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(st.Size(), 10)
}

var errShifting = errors.New("发布文件正在被替换,稍后重试")

// readCapped 读文件但不超过 limit 字节。
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
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
	// 版本号没配的话,manifest 就是个空承诺:客户端拿它跟自己比,
	// 空串永远"不相等",于是每次启动都以为有新版。宁可 503 说清楚
	if s.ReleaseVersion == "" {
		slog.Warn("发布目录配了但没给版本号,更新接口关闭", "dir", s.ReleaseDir)
		http.Error(w, "服务端没配 -release-version,更新接口暂不可用",
			http.StatusServiceUnavailable)
		return
	}
	exe := filepath.Join(s.ReleaseDir, "flipper-client.exe")
	sha, size, mod, err := s.releases.get(exe)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "还没有发布任何版本", http.StatusNotFound)
			return
		}
		// 权限错、目录配错、IO 错全压成 404 的话,排查时会一直
		// 以为"文件没放上去",实际上是别的问题
		slog.Error("读发布文件失败", "path", exe, "err", err)
		http.Error(w, "读发布文件失败", http.StatusInternalServerError)
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
	// 更新说明有上限。这个文件是人手写的,写秃噜了不该把 manifest 撑爆
	if notes, err := readCapped(filepath.Join(s.ReleaseDir, "RELEASE.txt"), 8<<10); err == nil {
		rel.Notes = strings.TrimSpace(string(notes))
	} else if !os.IsNotExist(err) {
		slog.Warn("读更新说明失败", "err", err)
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
	// 只靠 Last-Modified 的话,回滚到一个 mtime 更早的二进制会被
	// 中间缓存当成"旧版本"用 304 挡回去。加个 ETag 才认得出内容变了
	if name == "flipper-client.exe" {
		if sha, _, _, err := s.releases.get(path); err == nil {
			w.Header().Set("ETag", `"`+sha+`"`)
		}
	}
	// 公司网络下 13MB 断一次很常见。ServeFile 自带 Range 续传,
	// 比自己 io.Copy 强得多——千万别图省事换掉
	http.ServeFile(w, r, path)
}
