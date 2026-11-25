// 自测探针：headless Chrome + CDP。两种模式：
//   node scripts/selfcheck.js stacking <playground.html 绝对路径>
//   node scripts/selfcheck.js provider <dir 绝对路径>
// 只读断言，不改任何状态。退出码：0=断言通过，1=断言失败(发现 bug)，2=环境错误。
const fs = require("fs");
const path = require("path");
const http = require("http");
const { spawn } = require("child_process");

const mode = process.argv[2];
const target = path.resolve(process.argv[3]);
const port = 24770 + Math.floor(Math.random() * 200);
const udd = fs.mkdtempSync("/tmp/gxsc-");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let chrome = null, server = null;
const fin = (code, msg) => {
  try { chrome && chrome.kill("SIGKILL"); } catch {}
  try { server && server.close(); } catch {}
  try { fs.rmSync(udd, { recursive: true, force: true }); } catch {}
  if (msg) console.log(msg);
  process.exit(code);
};

const STACKING_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 30000;
  while (!(window.Gox && window.Gox.cursor)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(800);
  const rows = [...document.querySelectorAll('.gx-editor-row')];
  const span = rows[0].querySelector('.gx-code-line');
  const r = span.getBoundingClientRect();
  const x = r.left + 4, y = r.top + r.height / 2;
  const base = { clientX: x, clientY: y, bubbles: true, cancelable: true };
  for (const d of [1, 2]) {
    span.dispatchEvent(new PointerEvent('pointerdown', { ...base, detail: d, pointerType: 'mouse' }));
    span.dispatchEvent(new PointerEvent('pointerup', { ...base, detail: d, pointerType: 'mouse' }));
    span.dispatchEvent(new MouseEvent('click', { ...base, detail: d }));
  }
  span.dispatchEvent(new MouseEvent('dblclick', { ...base, detail: 2 }));
  await wait(600);
  const sel = [...document.querySelectorAll('.gx-sel')].map(s => s.getBoundingClientRect()).find(b => b.width > 0);
  if (!sel) return JSON.stringify({ err: "无选区色块" });
  const topEl = document.elementFromPoint(sel.left + sel.width / 2, sel.top + sel.height / 2);
  return JSON.stringify({
    selW: sel.width,
    codeZ: getComputedStyle(span).zIndex,
    codePos: getComputedStyle(span).position,
    topIsCodeLine: topEl ? (topEl === span || topEl.closest('.gx-code-line') !== null) : false,
    topClass: topEl ? String(topEl.className) : null,
  });
})()`;

// 探针 C：Go 语法诊断——真实浏览器内调 Gox.setSource 注入坏 Go，再经
// Gox.lspDiagnostics() 取诊断（面板渲染由 TS 事件驱动，程序化注入不触发它，
// 故断言走 API 层）。契约：parse 诊断行=错误所在 .gx 行（0 基 3）、消息不含
// 生成文件坐标 check.go。
const GSYNTAX_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 30000;
  while (!(window.Gox && window.Gox.setSource && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  Gox.setSource("package main\\n\\nfunc f() {\\n\\tx := ;\\n}\\n");
  await wait(500);
  const diags = JSON.parse(Gox.lspDiagnostics());
  const parse = diags.filter(d => d.code === "parse");
  const hit = parse.find(d => d.range.start.line >= 2 && d.range.start.line <= 4);
  return JSON.stringify({ n: diags.length, np: parse.length,
    sample: parse.slice(0, 3).map(d => [d.range.start.line, d.range.start.character, d.message.slice(0, 40)]),
    leaked: diags.some(d => (d.message || "").includes("check.go")),
    ok: !!hit && !diags.some(d => (d.message || "").includes("check.go")) });
})()`;

const PROVIDER_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 30000;
  while (document.querySelectorAll('div').length < 40) {
    if (Date.now() > deadline) return JSON.stringify({ err: "场景未渲染" });
    await wait(250);
  }
  await wait(800);
  const isPanel = d => d.textContent && (d.textContent.includes('浅色面板 · 子树锁定') || d.textContent.includes('深色面板 · 子树锁定'))
    && d.querySelector('button') && parseInt(getComputedStyle(d).paddingLeft) >= 14;
  const panels = [...document.querySelectorAll('div')].filter(isPanel);
  const read = d => ({ tag: d.textContent.slice(0, 5), bg: getComputedStyle(d).backgroundColor,
    shadow: getComputedStyle(d).boxShadow.slice(0, 30) });
  const info = panels.map(read);
  const light = info.find(x => x.tag.includes('浅色'));
  const dark = info.find(x => x.tag.includes('深色'));
  return JSON.stringify({ n: panels.length, light, dark,
    ok: !!light && !!dark && light.bg === 'rgb(255, 255, 255)' &&
        dark.bg === 'rgb(44, 44, 46)' && light.shadow !== dark.shadow });
})()`;

(async () => {
  const expr = mode === "stacking" ? STACKING_EXPR : (mode === "gsyntax" ? GSYNTAX_EXPR : PROVIDER_EXPR);
  let url;
  if (mode === "stacking" || mode === "gsyntax") {
    url = "file://" + target;
  } else {
    server = http.createServer((q, s) => {
      let rel = q.url === "/" ? "/index.html" : q.url.split("?")[0];
      const f = path.join(target, rel.replace(/^\/+/, ""));
      if (!f.startsWith(target) || !fs.existsSync(f) || !fs.statSync(f).isFile()) { s.writeHead(404); s.end(); return; }
      const t = f.endsWith(".html") ? "text/html" : f.endsWith(".wasm") ? "application/wasm" : "text/javascript";
      s.writeHead(200, { "Content-Type": t });
      fs.createReadStream(f).pipe(s);
    });
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    url = `http://127.0.0.1:${server.address().port}/index.html`;
  }

  chrome = spawn(process.env.CHROME_BIN || "google-chrome", [
    "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
    `--user-data-dir=${udd}`, `--remote-debugging-port=${port}`, "about:blank",
  ], { stdio: "ignore" });
  let ver = null;
  for (let i = 0; i < 120 && !ver; i++) {
    await sleep(150);
    try { ver = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json(); } catch {}
  }
  if (!ver) return fin(2, "ENV: devtools 未就绪");

  const ws = new WebSocket(ver.webSocketDebuggerUrl);
  let id = 0, session = null;
  const pend = new Map();
  const send = (m, p = {}) => new Promise((res) => {
    const mid = ++id; pend.set(mid, res);
    ws.send(JSON.stringify({ id: mid, method: m, params: p, ...(session ? { sessionId: session } : {}) }));
  });
  const pageErrs = [];
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pend.has(m.id)) { pend.get(m.id)(m); pend.delete(m.id); return; }
    if (m.method === "Runtime.exceptionThrown") {
      const d = m.params.exceptionDetails;
      pageErrs.push(String(((d.exception && d.exception.description) || d.text) || "").split("\n")[0].slice(0, 140));
    }
  };
  await new Promise((r) => { ws.onopen = r; });
  const ct = await send("Target.createTarget", { url: "about:blank" });
  const att = await send("Target.attachToTarget", { targetId: ct.result.targetId, flatten: true });
  session = att.result.sessionId;
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Page.navigate", { url });

  const r = await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true });
  const out = r.result && r.result.result ? r.result.result.value : "EVALFAIL:" + JSON.stringify(r).slice(0, 200);
  console.log(mode.toUpperCase(), out);
  if (pageErrs.length) console.log("PAGE_ERRORS", JSON.stringify(pageErrs.slice(0, 3)));
  let v = {};
  try { v = JSON.parse(String(out)); } catch {}
  const pass = mode === "stacking"
    ? !v.err && v.selW > 0 && v.topIsCodeLine === true
    : v.ok === true;
  fin(pass ? 0 : 1);
})().catch((e) => fin(2, "ENV/CRASH: " + String(e).slice(0, 200)));
