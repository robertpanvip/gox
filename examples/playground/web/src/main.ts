// GX Playground 前端加载器 —— 严格 TypeScript 7。
//
// 职责：
//   1. 加载 wasm_exec.js 提供的 Go 运行时 → 实例化 app.wasm → go.run；
//   2. 把浏览器键盘事件翻译成 Gox.key() 调用；
//   3. 用 wasm 内建的 goxlsp 会话（无服务端）实现诊断 / 补全 / hover / 格式化 UI。
// 业务渲染与编辑逻辑全部在 wasm 里（GX 源码编译产物 + gxweb 后端），
// 本文件只做引导、输入转发与 LSP 浮层，全程零 any。

import type { GoxCursor, GoxPlaygroundAPI } from "./gox.js";

interface Position { readonly line: number; readonly character: number }
interface Range { readonly start: Position; readonly end: Position }
interface Diagnostic {
  readonly range: Range;
  readonly severity: number;
  readonly source: string;
  readonly message: string;
}
interface CompletionItem {
  readonly label: string;
  readonly kind?: number;
  readonly detail?: string;
}
interface CompletionList {
  readonly isIncomplete: boolean;
  readonly items: readonly CompletionItem[];
}
interface HoverResult { readonly contents: { readonly kind: string; readonly value: string } }
/** 光标结构复用共享声明（见 gox.d.ts）。 */
type Cursor = GoxCursor;

interface Elements {
  readonly loading: HTMLElement;
  readonly app: HTMLElement;
  readonly hint: HTMLElement;
  readonly diagnostics: HTMLElement;
  readonly completion: HTMLElement;
  readonly hover: HTMLElement;
}

function requireElement(id: string): HTMLElement {
  const el: HTMLElement | null = document.getElementById(id);
  if (el === null) {
    throw new Error(`缺少挂载元素 #${id}`);
  }
  return el;
}

function collectElements(): Elements {
  // 列光标现在是 GX 场景节点（.gx-caret），由 wasm 的 caretLeft/caretTop getter
  // 驱动，与点击换算共用同一套字符宽度；前端不再维护 fixed 覆盖层。
  return {
    loading: requireElement("loading"),
    app: requireElement("app"),
    hint: requireElement("hint"),
    diagnostics: requireElement("lsp-diagnostics"),
    completion: requireElement("lsp-completion"),
    hover: requireElement("lsp-hover"),
  };
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === "string") {
    return err;
  }
  return "未知错误";
}

function showError(loading: HTMLElement, err: unknown): void {
  loading.textContent = `加载失败: ${errorMessage(err)}`;
  loading.setAttribute("data-state", "error");
}

function assertWasmSupport(): void {
  if (typeof WebAssembly === "undefined") {
    throw new Error("当前浏览器不支持 WebAssembly");
  }
}

/** JSON.parse 返回 any，这里显式收敛到 unknown 再断言，严格模式下无隐式 any。 */
function parseJSON<T>(text: string): T {
  const value: unknown = JSON.parse(text);
  return value as T;
}

async function waitForGox(timeoutMs: number = 8000): Promise<GoxPlaygroundAPI> {
  const started: number = Date.now();
  while (Date.now() - started < timeoutMs) {
    const api: GoxPlaygroundAPI | undefined = window.Gox;
    if (api !== undefined && typeof api.key === "function" && typeof api.lspDiagnostics === "function") {
      return api;
    }
    await new Promise<void>((resolve: () => void): number => window.setTimeout(resolve, 30));
  }
  throw new Error("等待 Gox 编辑 / LSP API 超时");
}

/**
 * 只有"可打印的单字符"才作为文本插入。Home/End/Delete/PageUp/F1 等功能键
 * 一律不灌进编辑器，否则会在源码里插入 "End"/"Home" 这种字面串。
 * Ctrl/Meta/Alt 组合也不产生文本（组合键有各自的快捷键语义）。
 */
function isPrintableChar(ev: KeyboardEvent): boolean {
  if (ev.ctrlKey || ev.metaKey || ev.altKey) {
    return false;
  }
  return ev.key.length === 1;
}

/** 编辑器识别的导航/编辑键，需要 preventDefault 阻止页面滚动。 */
const HANDLED_KEYS: ReadonlySet<string> = new Set<string>([
  "Backspace", "Delete", "Enter", "Tab",
  "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
  "Home", "End", "PageUp", "PageDown",
]);

/** 找到鼠标所在的行容器与 0 基行号。
 *
 * 整行（行号槽 + 代码 + 行尾空白）都带 .gx-editor-row，点击空行或文字之后的
 * 空白处也能命中——此前只认 .gx-code-line，空行 span 宽度为 0，点到空白直接
 * 返回 null，所以"有字的行能点、没字的行点不了"。列换算仍用行内代码 span 的
 * 左缘：空行的 span 没有内容但左缘位置是准确的，pickColumn 会算出列 0。 */
function lineAt(app: HTMLElement, ev: MouseEvent): { el: HTMLElement; line: number } | null {
  const target: EventTarget | null = ev.target;
  if (!(target instanceof HTMLElement)) {
    return null;
  }
  const row: HTMLElement | null = target.closest<HTMLElement>(".gx-editor-row");
  if (row === null) {
    return null;
  }
  const rows: HTMLElement[] = Array.from(app.querySelectorAll<HTMLElement>(".gx-editor-row"));
  const line: number = rows.indexOf(row);
  if (line < 0) {
    return null;
  }
  const el: HTMLElement = row.querySelector<HTMLElement>(".gx-code-line") ?? row;
  return { el, line };
}

// ---- 字符度量注入 + caret 常亮计时（Ace 风格） ----
// 列光标是 GX 场景节点，点击落点换算与 caret 定位都在 wasm 侧共用同一套
// runeCells × charWidth。前端只负责：启动时测一次等宽字体单格宽度注入 wasm，
// 以及每次交互后让 caret 常亮约 1s 再恢复闪烁（Ace 的 restartTimer）。

/** 用 canvas 实测 monospace 字体一个 ASCII 格（"0"）的像素宽度并注入 wasm。 */
function injectCharWidth(api: GoxPlaygroundAPI, app: HTMLElement): void {
  const canvas: HTMLCanvasElement = document.createElement("canvas");
  const ctx: CanvasRenderingContext2D | null = canvas.getContext("2d");
  if (ctx === null) {
    return; // 极端降级：wasm 侧保留 13px monospace 默认宽度。
  }
  const line: HTMLElement | null = app.querySelector<HTMLElement>(".gx-code-line");
  const font: string = line !== null
    ? window.getComputedStyle(line).font
    : "13px ui-monospace, Menlo, Consolas, monospace";
  ctx.font = font;
  // 窄字符取多个平均值；宽字符（CJK）单独实测——很多 monospace 字体里
  // CJK 并非恰好 2×ASCII（如 ui-monospace：ASCII 7.83px、CJK 13.02px）。
  const narrow: number = ctx.measureText("0000000000").width / 10;
  const wide: number = ctx.measureText("中中中").width / 3;
  if (narrow > 0 && Number.isFinite(narrow)) {
    api.setCharWidth(narrow);
  }
  if (wide > 0 && Number.isFinite(wide)) {
    api.setWideWidth(wide);
  }
}

let settleTimer: number | undefined;

/** 交互后让 caret 常亮，静置 1s 后恢复闪烁。 */
function settleCaret(api: GoxPlaygroundAPI): void {
  api.settle();
  if (settleTimer !== undefined) {
    window.clearTimeout(settleTimer);
  }
  settleTimer = window.setTimeout((): void => { api.idle(); }, 1000);
}

// ---- 诊断面板 ----

function renderDiagnostics(api: GoxPlaygroundAPI, elements: Elements): void {
  const panel: HTMLElement = elements.diagnostics;
  const diags: readonly Diagnostic[] = parseJSON<readonly Diagnostic[]>(api.lspDiagnostics());
  panel.replaceChildren();
  const title: HTMLElement = document.createElement("span");
  title.className = "lsp-title";
  if (diags.length === 0) {
    title.textContent = "LSP ✓ 无诊断";
    panel.classList.add("lsp-ok");
    panel.classList.remove("lsp-errors");
  } else {
    title.textContent = `LSP · ${diags.length} 个问题`;
    panel.classList.add("lsp-errors");
    panel.classList.remove("lsp-ok");
  }
  panel.append(title);
  for (const d of diags) {
    const item: HTMLButtonElement = document.createElement("button");
    item.className = "lsp-diag-item";
    item.type = "button";
    item.textContent = `${d.range.start.line + 1}:${d.range.start.character + 1}  ${d.message}`;
    item.addEventListener("click", (): void => {
      // 点击诊断：把光标移动到报错行（通过方向键模拟，保持编辑器为唯一真相源）。
      const cursor: Cursor = api.cursor();
      const delta: number = d.range.start.line - cursor.line;
      const key: string = delta > 0 ? "ArrowDown" : "ArrowUp";
      for (let i: number = 0; i < Math.abs(delta); i++) {
        api.key(key);
      }
      api.focus();
      settleCaret(api);
    });
    panel.append(item);
  }
}

// ---- 补全浮层 ----

let completionItems: readonly CompletionItem[] = [];
let completionIndex: number = 0;

function hideCompletion(elements: Elements): void {
  elements.completion.hidden = true;
  completionItems = [];
}

function applyCompletion(api: GoxPlaygroundAPI, elements: Elements, index: number): void {
  const item: CompletionItem | undefined = completionItems[index];
  if (item === undefined) {
    return;
  }
  api.insert(item.label);
  hideCompletion(elements);
  renderDiagnostics(api, elements);
  api.focus();
  settleCaret(api);
}

interface CompletionAnchor {
  readonly left: number;
  readonly top: number;
  readonly bottom: number;
}

/** 补全浮层的锚点取光标场景节点 .gx-caret 的真实屏幕位置。
 *
 * caret 是 GX 场景节点，left/top 由 wasm 的 caretLeft/caretTop getter 驱动，
 * 与点击落点换算共用同一套字符度量——所以浮层永远贴着光标，不再用写死的
 * app 左上角 + 固定偏移（此前光标在第 8 行时浮层仍停在编辑器顶部，位置全错）。
 * caret 失焦时 CSS 把它 display:none，此时 rect 全 0，回退到编辑器左上角。 */
function completionAnchor(app: HTMLElement): CompletionAnchor {
  const caret: HTMLElement | null = app.querySelector<HTMLElement>(".gx-caret");
  if (caret !== null) {
    const r: DOMRect = caret.getBoundingClientRect();
    if (r.height > 0 && r.width > 0) {
      return { left: r.left, top: r.top, bottom: r.bottom };
    }
  }
  const r: DOMRect = app.getBoundingClientRect();
  return { left: r.left + 90, top: r.top + 150, bottom: r.top + 172 };
}

function showCompletion(api: GoxPlaygroundAPI, elements: Elements, cursor: Cursor): void {
  const raw: string = api.lspComplete(cursor.line, cursor.col);
  const list: CompletionList = parseJSON<CompletionList>(raw);
  completionItems = list.items;
  completionIndex = 0;
  const panel: HTMLElement = elements.completion;
  panel.replaceChildren();
  if (list.items.length === 0) {
    panel.hidden = true;
    return;
  }
  const shown: readonly CompletionItem[] = list.items.slice(0, 12);
  shown.forEach((item: CompletionItem, idx: number): void => {
    const row: HTMLElement = document.createElement("div");
    row.className = idx === 0 ? "lsp-completion-row selected" : "lsp-completion-row";
    row.textContent = item.detail !== undefined ? `${item.label}  · ${item.detail}` : item.label;
    row.addEventListener("mousedown", (ev: MouseEvent): void => {
      ev.preventDefault();
      applyCompletion(api, elements, idx);
    });
    panel.append(row);
  });
  // 先显示才能量到真实高度；再按视口收窄，下方放不下就翻到光标上方。
  panel.hidden = false;
  const anchor: CompletionAnchor = completionAnchor(elements.app);
  const box: DOMRect = panel.getBoundingClientRect();
  const margin: number = 8;
  let y: number = anchor.bottom + 4;
  if (y + box.height > window.innerHeight - margin) {
    const above: number = anchor.top - box.height - 4;
    y = above >= margin ? above : Math.max(margin, window.innerHeight - box.height - margin);
  }
  const x: number = Math.max(margin, Math.min(anchor.left, window.innerWidth - box.width - margin));
  panel.style.left = `${x}px`;
  panel.style.top = `${y}px`;
}

function completionKeydown(ev: KeyboardEvent, api: GoxPlaygroundAPI, elements: Elements): boolean {
  if (elements.completion.hidden) {
    return false;
  }
  if (ev.key === "Escape") {
    hideCompletion(elements);
    return true;
  }
  if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
    const rows: NodeListOf<HTMLElement> = elements.completion.querySelectorAll<HTMLElement>(".lsp-completion-row");
    if (rows.length === 0) {
      return true;
    }
    rows[completionIndex]?.classList.remove("selected");
    completionIndex = ev.key === "ArrowDown"
      ? (completionIndex + 1) % rows.length
      : (completionIndex - 1 + rows.length) % rows.length;
    rows[completionIndex]?.classList.add("selected");
    return true;
  }
  if (ev.key === "Enter") {
    applyCompletion(api, elements, completionIndex);
    return true;
  }
  return false;
}

// ---- hover ----

let hoverTimer: number | undefined;

/** 把鼠标 x 交给 wasm 的 pickColumn 换算成列——与 caret 渲染同源。 */
function columnFromEvent(api: GoxPlaygroundAPI, el: HTMLElement, line: number, ev: MouseEvent): number {
  const rect: DOMRect = el.getBoundingClientRect();
  return api.pickColumn(line, ev.clientX - rect.left);
}

function showHover(api: GoxPlaygroundAPI, elements: Elements, ev: MouseEvent): void {
  const app: HTMLElement = elements.app;
  const hit = lineAt(app, ev);
  if (hit === null) {
    elements.hover.hidden = true;
    return;
  }
  const col: number = columnFromEvent(api, hit.el, hit.line, ev);
  const hover: HoverResult | null = parseJSON<HoverResult | null>(api.lspHover(hit.line, col));
  if (hover === null) {
    elements.hover.hidden = true;
    return;
  }
  // markdown 仅用 **粗体** 与 `代码`，这里做最小渲染，避免引入富文本依赖。
  elements.hover.textContent = hover.contents.value.replace(/\*\*/g, "").replace(/`/g, "");
  elements.hover.style.left = `${Math.min(ev.clientX + 14, window.innerWidth - 340)}px`;
  elements.hover.style.top = `${Math.min(ev.clientY + 14, window.innerHeight - 100)}px`;
  elements.hover.hidden = false;
}

// ---- 输入绑定 ----

function bindKeyboard(elements: Elements, api: GoxPlaygroundAPI): void {
  const app: HTMLElement = elements.app;
  app.tabIndex = 0;
  app.addEventListener("mousedown", (ev: MouseEvent): void => {
    // 点击代码行：行号从 DOM 顺序取，列交给 wasm 的 pickColumn（与 caret 同源）。
    // caret 是 GX 场景节点，光标列写入后由响应式 getter 自动重绘，无需前端追随。
    const hit = lineAt(app, ev);
    if (hit !== null) {
      api.setCursor(hit.line, columnFromEvent(api, hit.el, hit.line, ev));
    }
    settleCaret(api);
    api.focus();
    app.focus();
  });
  app.addEventListener("keydown", (ev: KeyboardEvent): void => {
    // Ctrl/Cmd+Enter：解释当前源码并运行（工具栏 Run 按钮走 GX 节点，同一动作）。
    if ((ev.ctrlKey || ev.metaKey) && ev.key === "Enter") {
      ev.preventDefault();
      api.run();
      return;
    }
    // Ctrl/Cmd+Z 撤销、Ctrl/Cmd+Shift+Z / Ctrl+Y 重做。必须最先拦截：
    // 组合键不带可打印字符，落到后面的 isPrintableChar/navKeys 判定会被直接
    // return，所以此前按 Ctrl+Z 毫无反应。撤销/重做走 wasm 的独立 API。
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "z") {
      ev.preventDefault();
      if (ev.shiftKey) { api.redo(); } else { api.undo(); }
      window.setTimeout((): void => { renderDiagnostics(api, elements); }, 0);
      settleCaret(api);
      return;
    }
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "y") {
      ev.preventDefault();
      api.redo();
      window.setTimeout((): void => { renderDiagnostics(api, elements); }, 0);
      settleCaret(api);
      return;
    }
    // Ctrl+Space：补全；Shift+Alt+F：格式化；先于编辑器转发处理。
    if (ev.ctrlKey && ev.key === " ") {
      ev.preventDefault();
      const cursor: Cursor = api.cursor();
      showCompletion(api, elements, cursor);
      return;
    }
    if (ev.shiftKey && ev.altKey && ev.key.toLowerCase() === "f") {
      ev.preventDefault();
      const formatted: string = api.lspFormat();
      api.setSource(formatted);
      renderDiagnostics(api, elements);
      settleCaret(api);
      return;
    }
    if (completionKeydown(ev, api, elements)) {
      ev.preventDefault();
      return;
    }
    // 功能键：阻止默认行为（方向键/空格滚动页面等），但不作为文本插入。
    if (HANDLED_KEYS.has(ev.key)) {
      ev.preventDefault();
    }
    // 只有可打印单字符或编辑器明确支持的导航键才转发给 wasm；
    // Home/End/PageUp/Delete/Tab/F1 等不转发，避免变成乱码插入源码。
    const navKeys: ReadonlySet<string> = new Set<string>([
      "Backspace", "Enter", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
    ]);
    if (!isPrintableChar(ev) && !navKeys.has(ev.key)) {
      return;
    }
    api.key(ev.key);
    // 输入后刷新诊断（wasm 侧随每次 key 同步重算，这里只拉结果），并让 caret 常亮。
    window.setTimeout((): void => { renderDiagnostics(api, elements); }, 0);
    settleCaret(api);
  });
  // caret 是编辑器容器内的场景节点，滚动/缩放时浏览器自然跟随，无需前端重定位。
  app.addEventListener("mousemove", (ev: MouseEvent): void => {
    if (hoverTimer !== undefined) {
      window.clearTimeout(hoverTimer);
    }
    hoverTimer = window.setTimeout((): void => { showHover(api, elements, ev); }, 260);
  });
  app.addEventListener("mouseleave", (): void => {
    elements.hover.hidden = true;
  });
  document.addEventListener("mousedown", (ev: MouseEvent): void => {
    const target: EventTarget | null = ev.target;
    if (target instanceof HTMLElement && target.closest("#lsp-completion") === null) {
      hideCompletion(elements);
    }
  });
}

// ---- wasm 加载 ----

function decodeBase64(base64: string): ArrayBuffer {
  const binary: string = atob(base64);
  const buffer: ArrayBuffer = new ArrayBuffer(binary.length);
  const bytes: Uint8Array = new Uint8Array(buffer);
  for (let i: number = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return buffer;
}

async function gunzip(compressed: ArrayBuffer): Promise<ArrayBuffer> {
  if (typeof DecompressionStream === "undefined") {
    throw new Error("当前浏览器不支持 DecompressionStream，无法打开单文件页面");
  }
  const stream: ReadableStream<Uint8Array> =
    new Blob([compressed]).stream().pipeThrough(new DecompressionStream("gzip"));
  return await new Response(stream).arrayBuffer();
}

async function instantiateWasm(go: InstanceType<Window["Go"]>): Promise<WebAssembly.WebAssemblyInstantiatedSource> {
  const embedded: string | undefined = window.__GX_PLAYGROUND_WASM_B64__;
  if (embedded !== undefined && embedded.length > 0) {
    const tStart: number = performance.now();
    const gzBuffer: ArrayBuffer = decodeBase64(embedded);
    const tDecoded: number = performance.now();
    const wasmBytes: ArrayBuffer = await gunzip(gzBuffer);
    const tInflated: number = performance.now();
    const result: WebAssembly.WebAssemblyInstantiatedSource =
      await WebAssembly.instantiate(wasmBytes, go.importObject);
    const tReady: number = performance.now();
    window.__GX_TIMING__ = {
      decodeMs: +(tDecoded - tStart).toFixed(1),
      inflateMs: +(tInflated - tDecoded).toFixed(1),
      compileMs: +(tReady - tInflated).toFixed(1),
      readyMs: +tReady.toFixed(1),
    };
    return result;
  }
  const response: Response = await fetch("app.wasm");
  if (!response.ok) {
    throw new Error(`获取 app.wasm 失败: HTTP ${response.status}`);
  }
  return await WebAssembly.instantiateStreaming(response, go.importObject);
}

async function boot(elements: Elements): Promise<void> {
  assertWasmSupport();
  const go = new window.Go();
  const result: WebAssembly.WebAssemblyInstantiatedSource = await instantiateWasm(go);
  elements.loading.remove();
  void go.run(result.instance);
  const api: GoxPlaygroundAPI = await waitForGox();
  bindKeyboard(elements, api);
  // 等 GX 首帧把代码行挂到 DOM 后再实测字体宽度并注入，保证 caret 与点击换算同源。
  window.requestAnimationFrame((): void => {
    injectCharWidth(api, elements.app);
  });
  renderDiagnostics(api, elements);
  settleCaret(api);
  elements.hint.textContent = "就绪 · 点击代码任意位置定位（含空行） · Ctrl+Z 撤销 / Ctrl+Shift+Z 重做 · Ctrl+Space 补全 · Shift+Alt+F 格式化 · 悬停看符号 · 左侧选示例即运行";
}

function main(): void {
  const elements: Elements = collectElements();
  void boot(elements).catch((err: unknown): void => {
    showError(elements.loading, err);
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", main, { once: true });
} else {
  main();
}
