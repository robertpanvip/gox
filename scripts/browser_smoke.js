// 真实浏览器 playground 冒烟（提案 §12 P3 第 3 条）：headless Chrome 直接加载
// 单文件构建 playground.html（wasm 内联，file:// 即可），验证：
//   1. wasm 启动、Gox API 就绪、编辑器行渲染出真实 DOM；
//   2. 数据驱动双击选词：找第一行「以 ASCII 单词开头」的代码行，双击词首 →
//      .gx-sel 选区色块宽度 > 0，光标落在该词末尾（不硬编码示例内容）；
//   3. 有选区时键入 'x' → 引擎「替换选区」语义：光标回到列 1；
//   4. 全程无页面异常（exceptionThrown）。
// 用法：node scripts/browser_smoke.js [playground.html 路径]
// 退出码：0=SMOKE_OK，1=断言失败，2=环境错误。
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");

const file = path.resolve(process.argv[2] || "examples/playground/web/playground.html");
if (!fs.existsSync(file)) {
  console.error("SMOKE_FAIL: 文件不存在", file);
  process.exit(2);
}
const port = 21777 + Math.floor(Math.random() * 300);
const udd = fs.mkdtempSync("/tmp/gxsmoke-");
const chrome = spawn(process.env.CHROME_BIN || "google-chrome", [
  "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
  `--user-data-dir=${udd}`, `--remote-debugging-port=${port}`, "about:blank",
], { stdio: "ignore" });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const errs = [];

const fin = (code, msg) => {
  try { chrome.kill("SIGKILL"); } catch {}
  try { fs.rmSync(udd, { recursive: true, force: true }); } catch {}
  if (msg) console.log(msg);
  process.exit(code);
};

(async () => {
  let ver = null;
  for (let i = 0; i < 120 && !ver; i++) {
    await sleep(150);
    try { ver = await (await fetch(`http://127.0.0.1:${port}/json/version`)).json(); } catch {}
  }
  if (!ver) return fin(2, "SMOKE_FAIL: chrome devtools 未就绪");

  const ws = new WebSocket(ver.webSocketDebuggerUrl);
  let id = 0;
  let session = null;
  const pend = new Map();
  const rpc = (method, params = {}) => new Promise((res) => {
    const mid = ++id;
    pend.set(mid, (m) => res(m.result !== undefined ? m.result : m));
    ws.send(JSON.stringify({ id: mid, method, params, ...(session ? { sessionId: session } : {}) }));
  });
  ws.onmessage = (ev) => {
    const m = JSON.parse(ev.data);
    if (m.id && pend.has(m.id)) { pend.get(m.id)(m); pend.delete(m.id); return; }
    if (m.method === "Runtime.exceptionThrown") {
      const d = m.params.exceptionDetails;
      const desc = (d.exception && d.exception.description) || d.text || "";
      if (!String(desc).includes("favicon")) errs.push(String(desc).split("\n")[0]);
    }
  };
  await new Promise((r) => { ws.onopen = r; });

  const { targetId } = await rpc("Target.createTarget", { url: "file://" + file });
  const att = await rpc("Target.attachToTarget", { targetId, flatten: true });
  session = att.sessionId;
  await rpc("Runtime.enable");

  // 1) 等 wasm 启动、Gox API 就绪（4MB 内联 wasm 解码+编译需要时间）
  let ready = false;
  for (let i = 0; i < 160 && !ready; i++) {
    const r = await rpc("Runtime.evaluate", {
      expression: "!!(window.Gox && typeof window.Gox.cursor === 'function')",
      returnByValue: true,
    });
    ready = !!(r && r.result && r.result.value);
    if (!ready) await sleep(250);
  }
  if (!ready) return fin(1, "SMOKE_FAIL: Gox 未就绪" + (errs.length ? " / " + errs[0] : ""));

  const ev = async (expression) => {
    const r = await rpc("Runtime.evaluate", { expression, returnByValue: true });
    if (r && r.exceptionDetails) {
      throw new Error("页面表达式异常: " + JSON.stringify(r.exceptionDetails).slice(0, 200));
    }
    return r && r.result ? r.result.value : undefined;
  };
  await sleep(600); // 首帧 + RAF 提交

  // 2) 编辑器 DOM 渲染
  const rows = await ev("document.querySelectorAll('.gx-editor-row').length");
  if (!rows || rows < 3) return fin(1, "SMOKE_FAIL: 编辑器行未渲染 rows=" + rows);

  // 3) 数据驱动：找第一行以 ASCII 单词开头的代码行（读 .gx-code-line 的
  //    data-code 快照），双击词首像素 → 光标应落在该词末尾、出现选区色块
  const info = JSON.parse(await ev(`(() => {
    const rows = [...document.querySelectorAll('.gx-editor-row')];
    for (let i = 0; i < rows.length; i++) {
      const span = rows[i].querySelector('.gx-code-line') || rows[i];
      const code = span.getAttribute('data-code') || '';
      const w = code.match(/^[A-Za-z_][A-Za-z0-9_]*(?=\\s|$)/);
      if (w) return JSON.stringify({ idx: i, word: w[0] });
    }
    return JSON.stringify({ idx: -1, word: '' });
  })()`));
  if (info.idx < 0) return fin(1, "SMOKE_FAIL: 找不到以单词开头的代码行");
  const wordEnd = info.word.length;

  await ev(`(() => {
    const rows = [...document.querySelectorAll('.gx-editor-row')];
    const row = rows[${info.idx}];
    const span = row.querySelector('.gx-code-line') || row;
    const r = span.getBoundingClientRect();
    const x = r.left + 4, y = r.top + r.height / 2;
    const base = { clientX: x, clientY: y, bubbles: true, cancelable: true };
    span.dispatchEvent(new PointerEvent('pointerdown', { ...base, detail: 1, pointerType: 'mouse' }));
    span.dispatchEvent(new PointerEvent('pointerup', { ...base, detail: 1, pointerType: 'mouse' }));
    span.dispatchEvent(new MouseEvent('click', { ...base, detail: 1 }));
    span.dispatchEvent(new PointerEvent('pointerdown', { ...base, detail: 2, pointerType: 'mouse' }));
    span.dispatchEvent(new PointerEvent('pointerup', { ...base, detail: 2, pointerType: 'mouse' }));
    span.dispatchEvent(new MouseEvent('click', { ...base, detail: 2 }));
    span.dispatchEvent(new MouseEvent('dblclick', { ...base, detail: 2 }));
    return true;
  })()`);
  await sleep(400); // 等 RAF 批量提交选区色块与光标

  const selW = await ev("(() => { const els = [...document.querySelectorAll('.gx-sel')]; return els.reduce((mx, s) => Math.max(mx, s.getBoundingClientRect().width), -1); })()");
  const cur0 = JSON.parse(await ev("JSON.stringify(window.Gox.cursor())"));

  // 4) 有选区时键入 'x' → 替换选区：词首列为 0，光标应落在列 1
  await ev(`(() => {
    const app = document.getElementById('app');
    app.dispatchEvent(new KeyboardEvent('keydown', { key: 'x', bubbles: true, cancelable: true }));
    return true;
  })()`);
  await sleep(400);
  const cur1 = JSON.parse(await ev("JSON.stringify(window.Gox.cursor())"));
  const caretVisible = await ev("(() => { const c = document.querySelector('.gx-caret'); return c ? getComputedStyle(c).display !== 'none' : false; })()");

  if (errs.length) return fin(1, "SMOKE_FAIL: 页面异常 " + errs[0]);
  if (!(selW > 0)) return fin(1, `SMOKE_FAIL: 双击未出现选区色块 selW=${selW}`);
  if (cur0.line !== info.idx || cur0.col !== wordEnd) {
    return fin(1, `SMOKE_FAIL: 双击选词光标=${JSON.stringify(cur0)} want {line:${info.idx},col:${wordEnd}} 词=「${info.word}」`);
  }
  if (cur1.line !== info.idx || cur1.col !== 1) {
    return fin(1, `SMOKE_FAIL: 替换选区后光标=${JSON.stringify(cur1)} want {line:${info.idx},col:1}`);
  }
  if (!caretVisible) return fin(1, "SMOKE_FAIL: 交互后光标未显示");
  fin(0, `SMOKE_OK: rows=${rows} 双击行${info.idx} 词「${info.word}」→选区宽=${selW}px 光标col=${cur0.col} 键入替换→col=${cur1.col} caret=可见 无页面异常`);
})().catch((e) => fin(1, "SMOKE_FAIL: " + String(e).slice(0, 200)));
