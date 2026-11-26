// 自测探针：headless Chrome + CDP。模式：
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
  // 新契约（d499781 重构后）：选区用 span 的 selection-range 属性 + 原生 paint
  // （色块层 gx-sel 已退役）。断言不弱化：范围必须精确等于首词 package 的 0:7，
  // 且文本层仍在顶层可读（elementFromPoint 命中代码行本身）。
  const selSpan = [...document.querySelectorAll('.gx-code-line')].find(e => e.getAttribute('selection-range'));
  if (!selSpan) return JSON.stringify({ err: "双击后无 selection-range" });
  const range = selSpan.getAttribute('selection-range');
  if (range !== selSpan2Range(selSpan)) return JSON.stringify({ err: "选区范围与词不符", range });
  const rb = selSpan.getBoundingClientRect();
  const topEl = document.elementFromPoint(rb.left + 4, rb.top + rb.height / 2);
  return JSON.stringify({
    selW: rb.width > 0 ? 1 : 0,
    selRange: range,
    codeZ: getComputedStyle(selSpan).zIndex,
    codePos: getComputedStyle(selSpan).position,
    topIsCodeLine: topEl ? (topEl === selSpan || topEl.closest('.gx-code-line') !== null) : false,
    topClass: topEl ? String(topEl.className) : null,
  });
  function selSpan2Range(e) {
    const t = e.getAttribute('data-code') || "";
    const m = /^[A-Za-z_]*/.exec(t);
    return "0:" + (m ? m[0].length : 0);
  }
})()`;

// 探针 C：wasm 内嵌类型层端到端——真实浏览器里注入类型错误源码，
// 断言 lspDiagnostics 产出 code="type" 诊断、坐标回投 .gx（0 基 line 4）、
// 消息零生成文件坐标泄露；同时输出首次类型检查就绪耗时。
const GSYNTAX_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  const src = "package main\\n\\nfunc f() {\\n\\tn := 41\\n\\tvar g string = n\\n\\t_ = g\\n}\\n";
  const t0 = performance.now();
  Gox.setSource(src);
  let diags = [];
  for (let i = 0; i < 250; i++) {
    diags = JSON.parse(Gox.lspDiagnostics());
    if (diags.some(d => d.code === "type")) break;
    await wait(20);
  }
  const t1 = performance.now();
  const td = diags.filter(d => d.code === "type");
  return JSON.stringify({ msFirst: Math.round(t1 - t0), total: diags.length, typeHit: td.length > 0,
    sample: td.slice(0, 2).map(d => [d.range.start.line, d.range.start.character, String(d.message).slice(0, 48)]),
    leaked: diags.some(d => String(d.message).includes("check.go")) });
})()`;

// 探针 D：浏览器端类型感知补全——setSource 后在 b.（*strings.Replacer）处
// 调 lspComplete，断言 Replace 候选、kind=2(method)、detail 带签名；
// 同时验证非点号路径（fallbackItems>0）未被接线破坏。
const COMP_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.lspComplete && window.Gox.setSource)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  const src = 'package main\\n\\nimport "strings"\\n\\nfunc f() {\\n\\tb := strings.NewReplacer()\\n\\tb.\\n\\t_ = b\\n}\\n';
  Gox.setSource(src);
  await wait(900);
  const list = JSON.parse(Gox.lspComplete(6, 3));
  const hit = (list.items || []).find(i => i.label === "Replace");
  const fb = JSON.parse(Gox.lspComplete(2, 8));
  return JSON.stringify({ typeHit: !!hit, kind: hit ? hit.kind : 0,
    detail: hit ? String(hit.detail).slice(0, 46) : "",
    fallbackItems: (fb.items || []).length });
})()`;

// 探针 E：参数签名提示——fmt.Sprintf 括号后/逗号后分别要求 active 0/1；
// 非调用上下文必须返回 null（宿主降级），Label 必须含真实参数名。
const SIG_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.setSource && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  if (typeof Gox.lspSignature !== "function") return JSON.stringify({ err: "no-lspSignature" });
  const src = 'package main\\n\\nimport "fmt"\\n\\nfunc f() {\\n\\tfmt.Sprintf("%d", "x")\\n\\t_ = 0\\n}\\n';
  Gox.setSource(src);
  await wait(900);
  const s1 = JSON.parse(Gox.lspSignature(5, 13));
  const s2 = JSON.parse(Gox.lspSignature(5, 19));
  const s3 = JSON.parse(Gox.lspSignature(6, 3));
  const ok1 = !!(s1 && s1.signatures && s1.signatures.length === 1 && s1.activeParameter === 0
    && String(s1.signatures[0].label).indexOf("format string") >= 0);
  const ok2 = !!(s2 && s2.activeParameter === 1);
  const label = s1 && s1.signatures && s1.signatures[0] ? String(s1.signatures[0].label).slice(0, 64) : "";
  return JSON.stringify({ ok: !!(ok1 && ok2 && s3 === null), label });
})()`;

// 探针 F：导航链——lspDefinition(std快览+local回跳)/lspHover真实签名/lspComplete import 路径/stdSource 全文。
const NAV_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.setSource && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  if (typeof Gox.lspDefinition !== "function") return JSON.stringify({ err: "no-lspDefinition" });
  const src = 'package main\\n\\nimport "fmt"\\n\\nfunc main() {\\n\\ts := fmt.Sprintf("%d", "x")\\n\\t_ = s\\n}\\n';
  Gox.setSource(src);
  await wait(900);
  const d = JSON.parse(Gox.lspDefinition(5, 16));
  const okDef = !!(d && d.kind === "std" && d.pkg === "fmt" && String(d.snippet).indexOf("func Sprintf") >= 0);
  const h = JSON.parse(Gox.lspHover(5, 16));
  const okHov = !!(h && h.contents && String(h.contents.value).indexOf("func(") >= 0);
  const dl = JSON.parse(Gox.lspDefinition(5, 2));
  const okLocal = !!(dl && dl.kind === "local");
  let okFull = false;
  if (okDef && typeof Gox.stdSource === "function") {
    const full = JSON.parse(Gox.stdSource(d.pkg, d.file));
    okFull = !!(full && String(full.text).length > 500 && String(full.text).indexOf("func Sprintf") >= 0);
  }
  Gox.setSource('package main\\n\\nimport "str\\n');
  await wait(400);
  const imp = JSON.parse(Gox.lspComplete(2, 11));
  const okImp = !!(((imp && imp.items) || []).find(i => i.label === "strings"));
  Gox.setSource(src);
  await wait(700);
  Gox.setCursor(5, 16);
  await wait(150);
  const appEl = document.getElementById("app") || document.body;
  appEl.dispatchEvent(new KeyboardEvent("keydown", { key: "/", ctrlKey: true, bubbles: true, cancelable: true }));
  await wait(500);
  const pop = document.getElementById("gx-defpop");
  const okPop = !!(pop && String(pop.textContent).indexOf("print.go") >= 0);
  let okView = false;
  if (okPop) {
    const btns = [...pop.querySelectorAll("button")];
    const full = btns.find((b) => String(b.textContent).indexOf("完整文件") >= 0);
    if (full !== undefined) {
      full.click();
      await wait(700);
      const v = document.getElementById("gx-defview");
      okView = !!(v && String(v.textContent).indexOf("func Sprintf") >= 0);
    }
  }
  return JSON.stringify({ ok: okDef && okHov && okLocal && okFull && okImp && okPop && okView,
    okDef, okHov, okLocal, okFull, okImp, okPop, okView });
})()`;

// 探针 H：references + QuickFix 闭环——引用清单、补 import 后复诊清零、删未使用 import。
const NAV2_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.setSource && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  if (typeof Gox.lspReferences !== "function") return JSON.stringify({ err: "no-lspReferences" });
  if (typeof Gox.lspCodeActions !== "function") return JSON.stringify({ err: "no-lspCodeActions" });
  const srcA = 'package main\\n\\nimport "fmt"\\n\\nfunc main() {\\n\\ts := fmt.Sprintf("%d", "x")\\n\\t_ = s\\n}\\n';
  Gox.setSource(srcA);
  await wait(900);
  const refs = JSON.parse(Gox.lspReferences(5, 16));
  const okRefs = !!(refs && refs.length >= 2
    && refs.some(r => r.isDef && r.pkg === "fmt") && refs.some(r => !r.isDef));
  const srcB = 'package main\\n\\nfunc main() {\\n\\ts := fmt.Sprintf("%d", "x")\\n\\t_ = s\\n}\\n';
  Gox.setSource(srcB);
  await wait(900);
  const acts = JSON.parse(Gox.lspCodeActions());
  const add = acts && acts.find(a => String(a.title).indexOf("fmt") >= 0 && a.edit && a.edit.text);
  let okFix = false, okFixClean = false;
  if (add) {
    const ls = srcB.split('\\n');
    ls.splice(add.edit.line, 0, add.edit.text.replace(/\\n$/, ""));
    Gox.setSource(ls.join('\\n'));
    await wait(900);
    const di = JSON.parse(Gox.lspDiagnostics());
    okFix = true;
    okFixClean = !di.some(d => String(d.message).indexOf("undefined") >= 0);
  }
  const srcC = 'package main\\n\\nimport "fmt"\\n\\nfunc main() {\\n\\tprintln(1)\\n}\\n';
  Gox.setSource(srcC);
  await wait(900);
  const acts2 = JSON.parse(Gox.lspCodeActions());
  const del = acts2 && acts2.find(a => a.edit && a.edit.delete === true);
  let okUnused = false, okUnusedClean = false;
  if (del) {
    okUnused = String(del.title).indexOf("fmt") >= 0;
    const ls2 = srcC.split('\\n');
    ls2.splice(del.edit.line, 1);
    Gox.setSource(ls2.join('\\n'));
    await wait(900);
    const di2 = JSON.parse(Gox.lspDiagnostics());
    okUnusedClean = !di2.some(d => String(d.message).indexOf("imported and not used") >= 0);
  }
  return JSON.stringify({ ok: okRefs && okFix && okFixClean && okUnused && okUnusedClean,
    okRefs, okFix, okFixClean, okUnused, okUnusedClean });
})()`;

// 探针 I：三件套——语义 token 编码、参数提示、resolve 文档（纯 API 面）。
const P1_EXPR = `
(async () => {
  const wait = ms => new Promise(r => setTimeout(r, ms));
  const deadline = Date.now() + 40000;
  while (!(window.Gox && window.Gox.setSource && window.Gox.lspDiagnostics)) {
    if (Date.now() > deadline) return JSON.stringify({ err: "Gox 超时未就绪" });
    await wait(250);
  }
  await wait(600);
  if (typeof Gox.lspSemanticTokens !== "function" || typeof Gox.lspInlayHints !== "function"
    || typeof Gox.lspResolveDoc !== "function") {
    return JSON.stringify({ err: "no-p1-exports" });
  }
  Gox.setSource('package main\\n\\nimport "fmt"\\n\\nfunc main() {\\n\\tfmt.Sprintf("%d", 1)\\n}\\n');
  await wait(900);
  const tk = JSON.parse(Gox.lspSemanticTokens());
  const okTok = !!(tk && tk.data && tk.data.length >= 30 && tk.data.length % 5 === 0);
  const hs = JSON.parse(Gox.lspInlayHints());
  const okHint = !!(hs && hs.some(h => h.label === "format:" && h.line === 5));
  const rd = JSON.parse(Gox.lspResolveDoc("fmt", "Sprintf"));
  const okDoc = !!(rd && String(rd.doc).indexOf("formats according") >= 0);
  return JSON.stringify({ ok: okTok && okHint && okDoc, okTok, okHint, okDoc });
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
  const expr = mode === "stacking" ? STACKING_EXPR : (mode === "gsyntax" ? GSYNTAX_EXPR : (mode === "comp" ? COMP_EXPR : (mode === "sig" ? SIG_EXPR : (mode === "nav" ? NAV_EXPR : (mode === "nav2" ? NAV2_EXPR : (mode === "p1" ? P1_EXPR : PROVIDER_EXPR))))));
  let url;
  if (mode === "stacking" || mode === "gsyntax" || mode === "comp" || mode === "sig" || mode === "nav" || mode === "nav2" || mode === "p1") {
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
  const pass = mode === "p1"
    ? !v.err && v.ok === true
    : mode === "nav2"
    ? !v.err && v.ok === true
    : mode === "nav"
    ? !v.err && v.ok === true
    : mode === "sig"
    ? !v.err && v.ok === true
    : mode === "comp"
    ? !v.err && v.typeHit === true && v.kind === 2 && v.fallbackItems > 0
    : mode === "stacking"
    ? !v.err && v.selW > 0 && v.topIsCodeLine === true
    : mode === "gsyntax"
      ? v.typeHit === true && v.leaked === false && v.sample && v.sample[0] && v.sample[0][0] === 4
      : v.ok === true;
  fin(pass ? 0 : 1);
})().catch((e) => fin(2, "ENV/CRASH: " + String(e).slice(0, 200)));
