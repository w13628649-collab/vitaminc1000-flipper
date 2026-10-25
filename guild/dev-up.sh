#!/usr/bin/env bash
# 本机开发环境一键起:dockerd → TimescaleDB 容器 → (需要时)编服务端和客户端 → 服务端跑起来。
#
# 幂等,重复跑不会坏。WSL 默认没有 systemd,重启 WSL 之后 dockerd 和服务端
# 都不会自己起来,再跑一次这个就行。从 Windows 那边:
#
#   wsl -d Ubuntu -u root -- bash guild/dev-up.sh          # 源码变了才编
#   wsl -d Ubuntu -u root -- bash guild/dev-up.sh build    # 不管变没变都重编
#
# **源码没变就不编**(用户 2026-10-25:「启动脚本无需每次都编译」;推翻 10-17 去掉 NOBUILD、
# 每次都编的做法,改成自动判断,改了代码照样会编)。判断办法:编进二进制的文件(.go、go.mod/go.sum、
# 界面资源、内置数据、迁移、图标资源)里有任何一个比服务端或客户端产物新,或者产物不在,就重编。
# 拉代码、切分支、改文件都会刷新 mtime,所以不会漏;测试文件、testdata、文档、脚本不算。
# 没重编、服务端也在正常跑时不重启它。
#
# 日常用仓库根的 dev-up.cmd 更好:它跑完会在 Windows
# 后台挂一个隐藏的 `wsl ... sleep infinity`。不挂的话,最后一个 wsl.exe 会话
# 退出后 WSL 会把整个发行版关掉,dockerd、数据库、服务端一起被杀,
# 日志里连"正在关闭"都没有(实测:拉起来十几分钟后就停了)
#
# 和 release.sh 的区别:不跑 gofmt/vet/test 门禁(Windows 检出是 CRLF,
# gofmt -l 会把每个文件都列出来),客户端连本机,数据库也归它管。
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$PATH:/usr/local/go/bin"

PORT="${PORT:-18420}"
PG_PORT="${PG_PORT:-55432}"
DEPLOY="${DEPLOY:-/opt/albion-guild}"
PG_IMAGE="${PG_IMAGE:-timescale/timescaledb:latest-pg16}"
DSN="postgres://postgres:dev@127.0.0.1:${PG_PORT}/flipper"
SERVER_ADDR="http://127.0.0.1:${PORT}"
SERVER_BIN="$DEPLOY/guild-server"
CLIENT_BIN="../dist/flipper-client.exe"

FORCE_BUILD=0
case "${1:-}" in
  "") ;;
  build|-b|--build) FORCE_BUILD=1 ;;
  *) echo "用法:dev-up.sh [build]   (build = 不管源码变没变都重编)" >&2; exit 2 ;;
esac

# need_build 说明为什么要编(打印出来);不用编时返回 1
need_build() {
  if [ "$FORCE_BUILD" = 1 ]; then echo "要求重编"; return 0; fi
  [ -x "$SERVER_BIN" ] || { echo "还没有服务端二进制"; return 0; }
  [ -f "$CLIENT_BIN" ] || { echo "还没有客户端 exe"; return 0; }
  # 和两个产物里旧的那个比:上次只编成了一个的话也会重编
  local ref="$SERVER_BIN" newer
  [ "$CLIENT_BIN" -ot "$ref" ] && ref="$CLIENT_BIN"
  newer=$(find . \( -name testdata -o -name '.*' ! -name . \) -prune -o -type f \
    ! -name '*_test.go' ! -name '*.md' ! -name '*.sh' ! -name 'config.yaml' \
    -newer "$ref" -print -quit)
  if [ -n "$newer" ]; then echo "源码比上次编的新(比如 ${newer#./})"; return 0; fi
  return 1
}

if WHY=$(need_build); then
  BUILD=1
  VERSION="${VERSION:-$(date +%Y%m%d)-dev}"
else
  BUILD=0
  # 沿用上次的二进制,版本号也沿用(客户端下载接口报的要和 exe 对得上)
  VERSION="${VERSION:-$(cat "$DEPLOY/VERSION" 2>/dev/null || echo "$(date +%Y%m%d)-dev")}"
fi

echo "→ dockerd"
if ! docker info >/dev/null 2>&1; then
  service docker start >/dev/null
  for _ in $(seq 30); do docker info >/dev/null 2>&1 && break; sleep 1; done
fi
docker info >/dev/null 2>&1 || { echo "✗ dockerd 起不来,看 /var/log/docker.log" >&2; exit 1; }

echo "→ 数据库 flipper-pg ($PG_IMAGE, :$PG_PORT)"
# 数据在命名卷 flipper-pg 里,删容器不丢数据;要清库就 docker volume rm flipper-pg
if docker container inspect flipper-pg >/dev/null 2>&1; then
  docker start flipper-pg >/dev/null
else
  docker run -d --name flipper-pg --restart unless-stopped \
    -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=flipper \
    -p "${PG_PORT}:5432" -v flipper-pg:/var/lib/postgresql/data "$PG_IMAGE" >/dev/null
fi
for _ in $(seq 60); do
  docker exec flipper-pg pg_isready -q -U postgres -d flipper && break
  sleep 1
done
docker exec flipper-pg pg_isready -U postgres -d flipper

mkdir -p "$DEPLOY/release" ../dist

start_server() {
  # 必须绑 127.0.0.1,不能写 0.0.0.0。Go 对通配地址开的是 [::] 双栈 socket,
  # WSL 的 localhost 转发看到 IPv6 监听就只在 Windows 上挂 ::1,
  # 客户端连 127.0.0.1 会直接 connection refused
  ( cd "$DEPLOY"
    nohup ./guild-server -addr "127.0.0.1:${PORT}" -dsn "$DSN" \
      -release-dir "$DEPLOY/release" -release-version "$VERSION" \
      >> server.log 2>&1 < /dev/null & )
}

if [ "$BUILD" = 0 ]; then
  echo "→ 源码没变,不编,沿用上次的(版本 $VERSION);要强制重编:dev-up.cmd build"
  if pgrep -x guild-server >/dev/null && curl -sf -o /dev/null "$SERVER_ADDR/api/stats"; then
    echo "→ 服务端已经在跑 :$PORT"
  else
    echo "→ 启动服务端 :$PORT"
    pkill -x guild-server 2>/dev/null || true
    for _ in $(seq 20); do pgrep -x guild-server >/dev/null || break; sleep 0.5; done
    start_server
  fi
else
  echo "→ 编服务端和客户端(版本 $VERSION):$WHY"
  CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o "$DEPLOY/guild-server.new" ./cmd/server
  # 客户端交叉编译:gopacket 在 Windows 上运行时加载 wpcap.dll,go-webview2 是纯 Go,
  # 都不需要 cgo。defaultServer 写死成本机,双击就能连上
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
    -ldflags "-H windowsgui -X main.version=$VERSION -X main.defaultServer=$SERVER_ADDR" \
    -o ../dist/flipper-client.exe.new ./cmd/client
  # 客户端开着的时候 exe 被 Windows 锁住,直接覆盖会失败、脚本停在这里、
  # 服务端也不重启。Windows 允许给运行中的 exe 改名(只是不许覆盖和删除),
  # 所以先把旧的挪开再换上新的:正在跑的那个不受影响,重开就是新版
  #
  # 挪开的文件名要带时间戳:上上次挪开的那个可能还在跑(用户一直没重开客户端),
  # 删不掉也覆盖不了,固定叫 .old 的话第二次部署就卡在这里。
  # 清理时删得掉的删,还在跑的留着,下次再清
  for old in ../dist/flipper-client.exe.old*; do
    [ -e "$old" ] && rm -f "$old" 2>/dev/null || true
  done
  if [ -e ../dist/flipper-client.exe ]; then
    mv -f ../dist/flipper-client.exe "../dist/flipper-client.exe.old-$(date +%H%M%S)"
  fi
  mv -f ../dist/flipper-client.exe.new ../dist/flipper-client.exe
  cp ../dist/flipper-client.exe "$DEPLOY/release/flipper-client.exe"
  echo "$VERSION" > "$DEPLOY/VERSION"

  echo "→ 重启服务端 :$PORT"
  pkill -x guild-server 2>/dev/null || true
  for _ in $(seq 20); do pgrep -x guild-server >/dev/null || break; sleep 0.5; done
  [ -e "$DEPLOY/guild-server.new" ] && mv -f "$DEPLOY/guild-server.new" "$DEPLOY/guild-server"
  start_server
fi

for _ in $(seq 40); do
  curl -sf -o /dev/null "$SERVER_ADDR/api/stats" && break
  sleep 0.5
done
if ! curl -sf -o /dev/null "$SERVER_ADDR/api/stats"; then
  echo "✗ 服务端没起来,看日志:" >&2
  tail -n 30 "$DEPLOY/server.log" >&2
  exit 1
fi

echo
echo "✓ 服务端 $SERVER_ADDR   日志 $DEPLOY/server.log"
echo "✓ 客户端 $(cd ../dist && pwd)/flipper-client.exe"
curl -s "$SERVER_ADDR/api/release"; echo
