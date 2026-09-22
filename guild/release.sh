#!/usr/bin/env bash
# 发一版:编客户端和服务端,推到部署目录,重启服务端。
#
# 之前这套命令只写在 docs/progress.md 里,中间还夹着一步手工 mv,
# 发两次就会漏掉一步。
set -euo pipefail
cd "$(dirname "$0")"

SERVER_ADDR="${SERVER_ADDR:-http://10.30.31.30:18420}"
DEPLOY="${DEPLOY:-/opt/albion-guild}"
DSN="${DSN:-postgres://postgres:dev@127.0.0.1:55432/flipper}"

# 版本号补零成三位。不补的话 20260922-10 的字典序小于 20260922-7,
# 版本比较就只能做"不相等 = 有新版",没法排序
SEQ="${SEQ:-001}"
VERSION="${VERSION:-$(date +%Y%m%d)-$(printf '%03d' "$((10#$SEQ))")}"

echo "→ 版本 $VERSION,服务端 $SERVER_ADDR"

echo "→ 检查"
gofmt -l ./cmd ./internal | (! grep .) || { echo "有文件没格式化"; exit 1; }
go vet -tags pcap ./...
go test ./... >/dev/null

echo "→ 编 Windows 客户端"
# 两个依赖都不需要 cgo:gopacket 运行时加载 wpcap.dll,
# go-webview2 是纯 Go 的。-H windowsgui 藏掉控制台
mkdir -p ../dist
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
  -ldflags "-H windowsgui -X main.version=$VERSION -X main.defaultServer=$SERVER_ADDR" \
  -o ../dist/flipper-client.exe ./cmd/client

echo "→ 编服务端"
go build -ldflags "-X main.version=$VERSION" -o "$DEPLOY/guild-server" ./cmd/server

echo "→ 推发布目录"
mkdir -p "$DEPLOY/release"
cp ../dist/flipper-client.exe "$DEPLOY/release/"
cp ../dist/使用说明.txt "$DEPLOY/release/" 2>/dev/null || true
( cd "$DEPLOY/release"
  python3 - <<'PY'
import zipfile, pathlib
files = [f for f in ("flipper-client.exe", "使用说明.txt") if pathlib.Path(f).exists()]
with zipfile.ZipFile("flipper-client.zip", "w", zipfile.ZIP_DEFLATED) as z:
    for f in files:
        z.write(f)
PY
  sha256sum flipper-client.exe flipper-client.zip > SHA256.txt )

echo "→ 重启服务端"
pkill -x guild-server || true
( cd "$DEPLOY"
  nohup ./guild-server -addr 0.0.0.0:18420 -dsn "$DSN" -scan 30m \
    -release-dir "$DEPLOY/release" -release-version "$VERSION" \
    > server.log 2>&1 & )

# 等它起来,别刚 pkill 完就去 curl
for _ in $(seq 20); do
  if curl -sf -o /dev/null "$SERVER_ADDR/api/release"; then break; fi
  read -t 1 -u 3 3< /dev/null || true
done

echo
curl -s "$SERVER_ADDR/api/release" | python3 -m json.tool
echo
echo "下载:$SERVER_ADDR/download/flipper-client.zip"
