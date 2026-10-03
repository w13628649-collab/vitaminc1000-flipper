// genicon 从同一份几何(logo.go)生成客户端图标的全部产物:
//
//	<client>/icon/icon-{16,20,24,32,40,48,64,128,256}.png   各尺寸位图
//	<client>/icon/icon.ico                                  多尺寸 ICO
//	<client>/rsrc_windows_amd64.syso                        Windows 资源:图标组 #1 + 版本信息
//	<webui>/logo.svg                                        界面顶栏的品牌图标和 favicon
//
// 只用标准库,不依赖 rc.exe / windres / go-winres,Linux 上也能跑。
// 生成物都提交在仓库里,平时编译不用跑它。改了几何或配色之后,在 guild 目录下:
//
//	go generate ./cmd/client
//	go test ./cmd/genicon          # 确认仓库里的生成物和几何对得上
//
// .syso 文件名带 _windows_amd64,go build 只在编 Windows amd64 时把它交给链接器;
// 服务端和 Linux 构建看不见它。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// sizes 覆盖 Windows 在 100%~250% 缩放下会去取的尺寸:标题栏 16/20/24/32/40,
// 任务栏 24/32/48,桌面和资源管理器 32/48/64/128/256。
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

// artifacts 是一次生成的全部产物,键是相对 client / webui 目录的路径。
type artifacts struct {
	client map[string][]byte
	webui  map[string][]byte
}

func build() (artifacts, error) {
	a := artifacts{client: map[string][]byte{}, webui: map[string][]byte{}}
	imgs := make([]iconImage, 0, len(sizes))
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := enc.Encode(&buf, render(s)); err != nil {
			return a, fmt.Errorf("编码 %dpx: %w", s, err)
		}
		imgs = append(imgs, iconImage{s, buf.Bytes()})
		a.client[filepath.Join("icon", fmt.Sprintf("icon-%d.png", s))] = buf.Bytes()
	}
	a.client[filepath.Join("icon", "icon.ico")] = encodeICO(imgs)
	res := append(iconResources(imgs), resource{rtVersion, versionID, versionInfo()})
	a.client["rsrc_windows_amd64.syso"] = encodeSyso(res)
	a.webui["logo.svg"] = []byte(svgDoc())
	return a, nil
}

func main() {
	client := flag.String("client", "cmd/client", "客户端包目录(.syso 和 icon/ 放这里)")
	webui := flag.String("webui", "internal/webui/assets", "界面资源目录(logo.svg 放这里)")
	flag.Parse()
	log.SetFlags(0)

	a, err := build()
	if err != nil {
		log.Fatal(err)
	}
	for _, out := range []struct {
		dir   string
		files map[string][]byte
	}{{*client, a.client}, {*webui, a.webui}} {
		for _, name := range slices.Sorted(maps.Keys(out.files)) {
			data := out.files[name]
			path := filepath.Join(out.dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				log.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				log.Fatal(err)
			}
			log.Printf("%-48s %7d 字节", path, len(data))
		}
	}
}
