"""Refresh local REA reference files without analyzing a target or editing config."""

from collections import Counter
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import platform
import queue
import subprocess
import threading
import time
import tomllib

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / ".rea"
CONFIG = Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex") / "config.toml"
REGISTRATION = tomllib.loads(CONFIG.read_text(encoding="utf-8-sig"))["mcp_servers"]["rea"]
COMMAND = [REGISTRATION["command"], *REGISTRATION["args"]]
if COMMAND[-1] != "mcp":
    raise RuntimeError("Expected the documented REA public launcher ending in mcp")
CLI = COMMAND[:-1]
ENV = os.environ.copy()
ENV.update(REGISTRATION.get("env", {}))
STAMP = datetime.now(timezone(timedelta(hours=8))).isoformat(timespec="seconds")


def write_json(name, value):
    (OUTPUT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def run_cli(*args):
    result = subprocess.run([*CLI, *args], capture_output=True, text=True,
                            encoding="utf-8", env=ENV, timeout=60)
    if result.returncode:
        raise RuntimeError(f"REA {args} failed: {result.stderr[:2000]}")
    return result.stdout


doctor = json.loads(run_cli("doctor", "--client", "codex", "--provider", "ghidra", "--skill", "--json"))
cli_help = run_cli("--help")
process = subprocess.Popen(COMMAND, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, text=True, encoding="utf-8", env=ENV)
messages = queue.Queue()
errors = []


def read_stdout():
    for line in process.stdout:
        try:
            messages.put(json.loads(line))
        except json.JSONDecodeError:
            messages.put({"invalid_line": line[:200]})
    messages.put({"closed": True})


def read_stderr():
    for line in process.stderr:
        errors.append(line)


threading.Thread(target=read_stdout, daemon=True).start()
threading.Thread(target=read_stderr, daemon=True).start()


def send(value):
    process.stdin.write(json.dumps(value) + "\n")
    process.stdin.flush()


def request(identifier, method, params):
    send({"jsonrpc": "2.0", "id": identifier, "method": method, "params": params})
    deadline = time.monotonic() + 30
    while True:
        response = messages.get(timeout=max(0.01, deadline - time.monotonic()))
        if response.get("closed"):
            raise RuntimeError("MCP server exited before returning a response")
        if response.get("id") == identifier:
            if "error" in response:
                raise RuntimeError(str(response["error"]))
            return response["result"]


try:
    initialization = request(1, "initialize", {
        "protocolVersion": "2025-06-18", "capabilities": {},
        "clientInfo": {"name": "rea-workspace-reference", "version": "1.0"},
    })
    send({"jsonrpc": "2.0", "method": "notifications/initialized"})
    tools = []
    cursor = None
    identifier = 2
    while True:
        page = request(identifier, "tools/list", {"cursor": cursor} if cursor else {})
        tools.extend(page["tools"])
        identifier += 1
        cursor = page.get("nextCursor")
        if not cursor:
            break
    session = request(identifier, "tools/call", {"name": "binary_session", "arguments": {}})
    if session.get("isError"):
        raise RuntimeError("binary_session reported a tool error")
finally:
    process.stdin.close()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.terminate()
        process.wait(timeout=10)

OUTPUT.mkdir(exist_ok=True)
allowed_env = {k: v for k, v in REGISTRATION.get("env", {}).items()
               if k in {"GHIDRA_INSTALL_DIR", "JAVA_HOME", "REA_ANALYSIS_PROVIDER",
                        "REA_GHIDRA_STARTUP_TIMEOUT_MS", "GHIDRA_HEADLESS_MAXMEM"}}
write_json("environment.json", {
    "recorded_at": STAMP, "workspace": str(ROOT), "config_path": str(CONFIG),
    "registration": {"command": REGISTRATION["command"], "args": REGISTRATION["args"],
                     "startup_timeout_sec": REGISTRATION.get("startup_timeout_sec"), "env": allowed_env},
    "os": platform.platform(), "python": platform.python_version(),
    "doctor_healthy": doctor["healthy"], "scope_checks": doctor["scope_checks"],
    "provider_inspections": doctor.get("providerInspections", []),
    "cli_package_version": doctor["identity"]["cli_package_version"],
    "skill": doctor["identity"]["skill"],
    "catalog_counts": doctor["identity"]["catalog"]["counts"],
    "catalog_digests": doctor["identity"]["catalog"]["digests"],
    "mcp_initialization": initialization, "mcp_tool_count": len(tools),
    "mcp_session_probe_is_error": session.get("isError", False),
    "mcp_process_exit_code": process.returncode,
    "active_agent_tools_exposed": False,
    "active_agent_note": "No REA MCP tools were exposed to this Codex turn when recorded. Separate configured stdio launch succeeded; future turns must inspect their own tools.",
    "native_target_analysis_verified": False,
})
write_json("mcp-tools.json", {"recorded_at": STAMP, "server_info": initialization["serverInfo"], "tools": tools})
write_json("tool-effects.json", doctor["identity"]["catalog"])
write_json("session-baseline.json", session)
(OUTPUT / "cli-help.txt").write_text(cli_help, encoding="utf-8")
catalog = ["# REA MCP 工具索引", "", f"记录时间：{STAMP}；服务版本：{initialization['serverInfo']['version']}；工具数：{len(tools)}。", "",
           "从实际 stdio `tools/list` 生成。精确参数和返回约束查 `mcp-tools.json`；副作用查 `tool-effects.json`。", "",
           "工具被列出不代表当前主机、目标和引擎均支持它。用活动会话的 `binary_session.result.tool_availability` 判断。", "",
           "| 工具 | 描述 |", "| --- | --- |"]
for tool in sorted(tools, key=lambda item: item["name"]):
    description = " ".join(tool.get("description", "").split()).replace("|", "\\|")
    catalog.append(f"| `{tool['name']}` | {description} |")
(OUTPUT / "TOOLS.md").write_text("\n".join(catalog) + "\n", encoding="utf-8")
print(json.dumps({"recorded_at": STAMP, "doctor_healthy": doctor["healthy"],
                  "mcp_server": initialization["serverInfo"], "mcp_tools": len(tools),
                  "session_probe_ok": not session.get("isError", False),
                  "process_exit": process.returncode, "output": str(OUTPUT)}, ensure_ascii=False))
