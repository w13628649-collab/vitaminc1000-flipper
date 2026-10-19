"""Persistent REA stdio connection for this user-selected Albion client."""

import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import tomllib

ROOT = Path(__file__).resolve().parent
CONFIG = Path.home() / ".codex" / "config.toml"
TARGET = Path(r"C:\Users\ludy\AppData\Local\AlbionOnline\game\GameAssembly.dll")
registration = tomllib.loads(CONFIG.read_text(encoding="utf-8-sig"))["mcp_servers"]["rea"]
registration["args"] = [str(ROOT / "vendor" / "rea" / "scripts" / "rea.mjs"), "mcp"]
environment = os.environ.copy()
environment.update(registration.get("env", {}))
# This 85 MiB client needs a longer first auto-analysis than small fixtures.
environment["REA_GHIDRA_STARTUP_TIMEOUT_MS"] = "2400000"
environment["GHIDRA_HEADLESS_MAXMEM"] = "4G"
environment["REA_GHIDRA_DISABLED_ANALYZERS"] = "Non-Returning Functions - Discovered,Decompiler Switch Analysis,x86 Constant Reference Analyzer,Function ID"
server = subprocess.Popen([registration["command"], *registration["args"]],
                          stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          text=True, encoding="utf-8", env=environment, bufsize=1)
responses = queue.Queue()
next_id = 0


def stdout_reader():
    for line in server.stdout:
        try:
            responses.put(json.loads(line))
        except json.JSONDecodeError:
            responses.put({"invalid_line": line[:1000]})
    responses.put({"closed": True})


def stderr_reader():
    with (ROOT / "server-stderr.log").open("a", encoding="utf-8") as output:
        for line in server.stderr:
            output.write(line)
            output.flush()


threading.Thread(target=stdout_reader, daemon=True).start()
threading.Thread(target=stderr_reader, daemon=True).start()


def request(method, params, timeout=2500):
    global next_id
    next_id += 1
    identifier = next_id
    server.stdin.write(json.dumps({"jsonrpc": "2.0", "id": identifier,
                                  "method": method, "params": params}) + "\n")
    server.stdin.flush()
    deadline = time.monotonic() + timeout
    while True:
        response = responses.get(timeout=max(0.01, deadline - time.monotonic()))
        if response.get("closed"):
            raise RuntimeError("REA server exited")
        if response.get("id") == identifier:
            if "error" in response:
                raise RuntimeError(str(response["error"]))
            return response["result"]


def unpack(result):
    if result.get("structuredContent") is not None:
        return result["structuredContent"]
    for content in result.get("content", []):
        if content.get("type") == "text":
            try:
                return json.loads(content["text"])
            except json.JSONDecodeError:
                pass
    return result


def call(name, arguments, label=None):
    started = time.monotonic()
    result = request("tools/call", {"name": name, "arguments": arguments})
    label = label or f"{next_id:03d}-{name}"
    path = ROOT / "evidence" / f"{label}.json"
    path.parent.mkdir(exist_ok=True)
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    value = unpack(result)
    record = {"label": label, "operation": name, "parameters": arguments,
              "duration_seconds": round(time.monotonic() - started, 2),
              "is_error": result.get("isError", False), "output": str(path)}
    if isinstance(value, dict):
        record["evidence_id"] = value.get("evidence_id")
        record["response_keys"] = list(value)
        if "error" in value:
            record["error"] = value["error"]
    with (ROOT / "calls.jsonl").open("a", encoding="utf-8") as log:
        log.write(json.dumps(record, ensure_ascii=False) + "\n")
    preview = {key: item for key, item in record.items() if key != "error"}
    if isinstance(record.get("error"), dict):
        preview["error"] = {key: record["error"].get(key) for key in ("code", "category", "message")}
    print(json.dumps(preview, ensure_ascii=False), flush=True)
    return value


def close():
    try:
        call("export_evidence_bundle", {"path": str(ROOT / "evidence-bundle.json"), "overwrite": True}, "export-bundle")
        call("close_binary", {}, "close-target")
    finally:
        server.stdin.close()
        try:
            server.wait(timeout=30)
        except subprocess.TimeoutExpired:
            server.terminate()
            server.wait(timeout=30)


initialization = request("initialize", {
    "protocolVersion": "2025-06-18", "capabilities": {},
    "clientInfo": {"name": "albion-static-investigation", "version": "1"},
})
server.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
server.stdin.flush()
(ROOT / "initialization.json").write_text(json.dumps(initialization, indent=2), encoding="utf-8")
call("open_binary", {"path": str(TARGET), "provider_id": "ghidra"}, "open-gameassembly")
call("binary_session", {}, "bound-session")
print("REA_READY_FOR_QUERY", flush=True)
try:
    processed = 0
    while True:
        command_file = ROOT / "commands.jsonl"
        lines = command_file.read_text(encoding="utf-8").splitlines() if command_file.exists() else []
        if processed >= len(lines):
            time.sleep(0.5)
            continue
        job = json.loads(lines[processed])
        processed += 1
        if job.get("finish"):
            break
        try:
            value = call(job["name"], job.get("arguments", {}), job.get("label"))
            if isinstance(value, dict):
                normalized = value.get("normalized_result")
                if isinstance(normalized, list):
                    print(json.dumps({"rows": len(normalized), "preview": normalized[:20]}, ensure_ascii=False), flush=True)
                elif isinstance(normalized, dict):
                    print(json.dumps({"normalized_preview": str(normalized)[:2500]}, ensure_ascii=False), flush=True)
        except Exception as error:
            print(json.dumps({"job_error": str(error)}, ensure_ascii=False), flush=True)
finally:
    close()
