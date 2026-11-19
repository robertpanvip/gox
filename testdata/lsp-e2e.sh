#!/usr/bin/env bash
# LSP 端到端驱动脚本：启动 gox-lsp，通过 stdin/stdout JSON-RPC 完成
# initialize → didOpen → documentSymbol → definition → shutdown，并校验响应。
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.27.3

go build -o /tmp/gox-lsp-e2e ./cmd/gox-lsp
python3 - <<'PY'
import json, subprocess

p = subprocess.Popen(["/tmp/gox-lsp-e2e"], stdin=subprocess.PIPE,
                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)

def send(method, params=None, mid=None):
    m = {"jsonrpc": "2.0", "method": method}
    if params is not None: m["params"] = params
    if mid is not None: m["id"] = mid
    body = json.dumps(m).encode()
    p.stdin.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
    p.stdin.flush()

def read():
    hdr = {}
    while True:
        line = p.stdout.readline()
        if not line: return None
        if line in (b"\r\n", b"\n"): break
        k, _, v = line.decode().partition(":")
        hdr[k.strip().lower()] = v.strip()
    n = int(hdr.get("content-length", 0))
    return json.loads(p.stdout.read(n)) if n else None

def req(method, params, mid):
    send(method, params, mid)
    while True:
        m = read()
        if m is None: raise SystemExit("LSP 意外退出")
        if m.get("id") == mid: return m

src = 'package main\nimport "fmt"\npub func greet() { fmt.Println("hi") }\nfunc main() { greet() }\n'
uri = "file:///tmp/e2e.gx"

r = req("initialize", {"processId": None, "rootUri": None, "capabilities": {}}, 1)
assert "result" in r, ("initialize", r)
send("initialized", {})
send("textDocument/didOpen", {"textDocument": {"uri": uri, "languageId": "gx", "version": 1, "text": src}})

r = req("textDocument/documentSymbol", {"textDocument": {"uri": uri}}, 2)
syms = r.get("result") or []
names = [s["name"] for s in syms] if isinstance(syms, list) else []
assert "greet" in names or "Greet" in names, ("documentSymbol", syms)

r = req("textDocument/definition",
        {"textDocument": {"uri": uri}, "position": {"line": 3, "character": 15}}, 3)
assert r.get("result") is not None, ("definition", r)

# rename：greet -> hello（普通符号），校验返回 edits
r = req("textDocument/rename",
        {"textDocument": {"uri": uri}, "position": {"line": 2, "character": 9},
         "newName": "hello"}, 4)
assert "result" in r and "changes" in (r.get("result") or {}), ("rename", r)

send("shutdown", None, 5)
send("exit", None)
p.stdin.close()
print("LSP 端到端：initialize / didOpen / documentSymbol / definition / rename / shutdown 全部通过")
PY
