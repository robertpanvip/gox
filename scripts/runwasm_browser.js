// 在真实 Chrome（headless + CDP）中运行 GOOS=js GOARCH=wasm 的 go test 二进制。
// 用法：node scripts/runwasm_browser.js <test.wasm 路径> [-- 测试参数...]
//   例：node scripts/runwasm_browser.js /tmp/loop.test.wasm -- -test.v -test.timeout 200s
// 需要环境存在 google-chrome（或 CHROME_BIN 指定）；wasm_exec.js 取自 go env GOROOT。
// 判定：转发 Go 测试的 console 输出；以输出中的 PASS/FAIL/panic 行为准；
// 退出码：0=PASS，1=FAIL/panic，2=宿主/超时错误。
const fs = require("fs");
const path = require("path");
const http = require("http");
const { spawnSync, spawn } = require("child_process");

const argv = process.argv.slice(2);
const sep = argv.indexOf("--");
const wasmPath = argv[0];
const testArgs = sep >= 0 ? argv.slice(sep + 1) : [];

const goroot = spawnSync("go", ["env", "GOROOT"], { encoding: "utf8" }).stdout.trim();
const execJs = path.join(goroot, "lib", "wasm", "wasm_exec.js");

const dir = fs.mkdtempSync("/tmp/gxwasm-");
const udd = fs.mkdtempSync("/tmp/gxchrome-");
fs.copyFileSync(execJs, path.join(dir, "wasm_exec.js"));
fs.copyFileSync(wasmPath, path.join(dir, "app.test.wasm"));
const argsJSON = JSON.stringify(["app.test.wasm", ...testArgs]);
fs.writeFileSync(
  path.join(dir, "index.html"),
  `<!doctype html><meta charset="utf-8"><title>gx wasm browser test</title>
<script src="wasm_exec.js"></script>
<script>
(async () => {
  try {
    const go = new Go();
    go.argv = ${argsJSON};
    const r = await WebAssembly.instantiateStreaming(fetch("app.test.wasm"), go.importObject);
    await go.run(r.instance);
    console.log("__GO_RETURN__0");
  } catch (e) {
    console.log("__GO_RETURN__" + ((e && e.message) || e));
  }
})();
</script>`,
);

const server = http.createServer((req, res) => {
  const rel = req.url === "/" ? "index.html" : req.url.slice(1).split("?")[0];
  const f = path.join(dir, rel);
  if (!f.startsWith(dir) || !fs.existsSync(f)) { res.writeHead(404); res.end(); return; }
  const type = f.endsWith(".html") ? "text/html" : f.endsWith(".wasm") ? "application/wasm" : "text/javascript";
  res.writeHead(200, { "Content-Type": type });
  fs.createReadStream(f).pipe(res);
});

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let chrome = null;
const cleanup = (code) => {
  try { if (chrome) chrome.kill("SIGKILL"); } catch {}
  try { fs.rmSync(udd, { recursive: true, force: true }); } catch {}
  server.close();
  fs.rmSync(dir, { recursive: true, force: true });
  process.exit(code);
};

server.listen(0, "127.0.0.1", async () => {
  const pageURL = `http://127.0.0.1:${server.address().port}/index.html`;
  const port = 21222 + Math.floor(Math.random() * 500);
  const chromeBin = process.env.CHROME_BIN || "google-chrome";
  chrome = spawn(chromeBin, [
    "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
    `--user-data-dir=${udd}`, `--remote-debugging-port=${port}`, "about:blank",
  ], { stdio: "ignore" });

  const fail = (msg) => { console.error("BROWSER_ERROR:", msg); cleanup(2); };

  // 走 browser endpoint + flatten attach：先 Runtime.enable 再导航，
  // 保证不丢任何 console 事件（page endpoint 直接带 URL 创建会漏掉早期输出）。
  let version = null;
  for (let i = 0; i < 100 && !version; i++) {
    await sleep(150); // Chrome 冷启动 ~1s，轮询就绪
    try { version = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json(); } catch (e) {}
  }
  if (!version || !version.webSocketDebuggerUrl) return fail("无 browser 调试端点");

  const ws = new WebSocket(version.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  let session = null;
  const send = (method, params = {}) => new Promise((resolve) => {
    const mid = ++id;
    pending.set(mid, resolve);
    ws.send(JSON.stringify({ id: mid, method, params, ...(session ? { sessionId: session } : {}) }));
  });
  let output = "";
  let verdict = null;
  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg.result); pending.delete(msg.id); }
    if (msg.method === "Runtime.consoleAPICalled") {
      for (const a of (msg.params && msg.params.args) || []) {
        const s = a.value !== undefined ? String(a.value) : (a.description || "");
        output += s + "\n";
        for (const ln of s.split("\n")) {
          const t = ln.trim();
          if (verdict === null && (t === "PASS" || t === "FAIL" || t.startsWith("panic:") || t.startsWith("__GO_RETURN__"))) {
            if (t.startsWith("__GO_RETURN__")) {
              const c = Number(t.slice("__GO_RETURN__".length));
              verdict = c === 0 ? 0 : 1; // go.run 正常返回视为通过
            } else {
              verdict = t === "PASS" ? 0 : 1;
            }
          }
        }
      }
    }
    if (msg.method === "Runtime.exceptionThrown") {
      const d = msg.params && msg.params.exceptionDetails;
      const desc = (d && d.exception && d.exception.description) || JSON.stringify(d) || "";
      // wasm_exec 在 Go os.Exit 后的排队 resume 必然抛这个，属宿主伪异常，不算失败。
      if (desc.includes("Go program has already exited")) return;
      output += "EXCEPTION: " + desc + "\n";
      if (verdict === null) verdict = 1;
    }
  };
  ws.onopen = async () => {
    const { targetId } = await send0("Target.createTarget", { url: "about:blank" });
    const att = await send0("Target.attachToTarget", { targetId, flatten: true });
    session = att.sessionId;
    await send("Runtime.enable");
    await send("Page.enable");
    await send("Page.navigate", { url: pageURL });
  };
  function send0(method, params) { // attach 前无 sessionId
    return new Promise((resolve) => {
      const mid = ++id;
      pending.set(mid, resolve);
      ws.send(JSON.stringify({ id: mid, method, params }));
    });
  }

  const start = Date.now();
  while (verdict === null && Date.now() - start < 240000) await sleep(200);
  process.stdout.write(output);
  if (verdict === null) return fail("等待测试结束超时");
  cleanup(verdict);
});
