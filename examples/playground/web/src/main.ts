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
  readonly caret: HTMLElement;
}

function requireElement(id: string): HTMLElement {
  const el: HTMLElement | null = document.getElementById(id);
  if (el === null) {
    throw new Error(`缺少挂载元素 #${id}`);
  }
  return el;
}

function collectElements(): Elements {
  // 列光标覆盖层：fixed 定位到 body，跟随代码行滚动；不进 #app 以免被
  // GX 场景树 patch 时清掉。样式由 index.html 的 #gx-caret 定义。
  const caret: HTMLElement = document.createElement("div");
  caret.id = "gx-caret";
  caret.hidden = true;
  document.body.append(caret);
  return {
    loading: requireElement("loading"),
    app: requireElement("app"),
    hint: requireElement("hint"),
    diagnostics: requireElement("lsp-diagnostics"),
    completion: requireElement("lsp-completion"),
    hover: requireElement("lsp-hover"),
    caret,
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

/** 找到鼠标所在的代码行元素与 0 基行号。 */
function lineAt(app: HTMLElement, ev: MouseEvent): { el: HTMLElement; line: number } | null {
  const target: EventTarget | null = ev.target;
  if (!(target instanceof HTMLElement)) {
    return null;
  }
  const el: HTMLElement | null = target.closest(".gx-code-line");
  if (el === null) {
    return null;
  }
  const lines: HTMLElement[] = Array.from(app.querySelectorAll<HTMLElement>(".gx-code-line"));
  const line: number = lines.indexOf(el);
  return line >= 0 ? { el, line } : null;
}

// ---- 等宽字符测量（列定位 + 列光标共用） ----
// 代码行用 monospace 13px，但 CJK 宽约 2×ASCII。用 canvas measureText 逐字符
// 累加，点击列和光标位置都按真实像素算，中文/缩进/制表都不再错位。

let measureCtx: CanvasRenderingContext2D | null = null;

/** 代码行的 CSS font 缩写，供 canvas measureText 使用。 */
function codeFont(el: HTMLElement): string {
  const css: string = window.getComputedStyle(el).font;
  if (css.length > 0) {
    return css;
  }
  const size: string = window.getComputedStyle(el).fontSize;
  return `${size} ui-monospace, Menlo, Consolas, monospace`;
}

/** 返回字符串 s 在给定字体下的像素宽度。 */
function textWidth(s: string, font: string): number {
  if (measureCtx === null) {
    const canvas: HTMLCanvasElement = document.createElement("canvas");
    measureCtx = canvas.getContext("2d");
  }
  if (measureCtx === null) {
    // 极端降级：按等宽 8px/ASCII、16px/CJK 估算。
    let w: number = 0;
    for (const ch of s) { w += ch.charCodeAt(0) >= 0x2e80 ? 16 : 8; }
    return w;
  }
  measureCtx.font = font;
  return measureCtx.measureText(s).width;
}

/** 按鼠标 x 在代码行内定位列（0 基，按 rune 计，与 wasm 光标一致）。 */
function columnAt(el: HTMLElement, ev: MouseEvent): number {
  const rect: DOMRect = el.getBoundingClientRect();
  const x: number = ev.clientX - rect.left;
  const font: string = codeFont(el);
  const code: string = el.getAttribute("data-code") ?? "";
  const chars: readonly string[] = Array.from(code); // 按 rune 切分
  let acc: number = 0;
  for (let i: number = 0; i < chars.length; i++) {
    const cw: number = textWidth(chars[i] ?? "", font);
    if (acc + cw / 2 >= x) {
      return i;
    }
    acc += cw;
  }
  return chars.length;
}

/** 渲染闪烁的列光标：fixed 覆盖层，跟随光标行列与编辑器滚动。 */
function renderCaret(api: GoxPlaygroundAPI, elements: Elements): void {
  const caret: HTMLElement = elements.caret;
  const cursor: Cursor = api.cursor();
  const lines: NodeListOf<HTMLElement> = elements.app.querySelectorAll<HTMLElement>(".gx-code-line");
  const el: HTMLElement | undefined = lines[cursor.line];
  if (el === undefined) {
    caret.hidden = true;
    return;
  }
  const rect: DOMRect = el.getBoundingClientRect();
  const code: string = el.getAttribute("data-code") ?? "";
  const chars: readonly string[] = Array.from(code);
  const prefix: string = chars.slice(0, Math.min(cursor.col, chars.length)).join("");
  const x: number = rect.left + textWidth(prefix, codeFont(el));
  caret.style.left = `${x}px`;
  caret.style.top = `${rect.top}px`;
  caret.style.height = `${rect.height}px`;
  caret.hidden = false;
}

/** DOM 更新走 RAF 提交，光标刷新排到下一帧读最新几何。 */
function scheduleCaret(api: GoxPlaygroundAPI, elements: Elements): void {
  window.requestAnimationFrame((): void => { renderCaret(api, elements); });
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
      scheduleCaret(api, elements);
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
  scheduleCaret(api, elements);
}

function showCompletion(
  api: GoxPlaygroundAPI, elements: Elements, cursor: Cursor, x: number, y: number,
): void {
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
  panel.style.left = `${x}px`;
  panel.style.top = `${y}px`;
  panel.hidden = false;
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

function showHover(api: GoxPlaygroundAPI, elements: Elements, ev: MouseEvent): void {
  const app: HTMLElement = elements.app;
  const hit = lineAt(app, ev);
  if (hit === null) {
    elements.hover.hidden = true;
    return;
  }
  const col: number = columnAt(hit.el, ev);
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
    // 点击代码行：按鼠标位置定位光标（行号从 DOM 顺序取，列按字符像素算）。
    const hit = lineAt(app, ev);
    if (hit !== null) {
      api.setCursor(hit.line, columnAt(hit.el, ev));
    }
    // 点示例行/预览按钮也会改变光标与源码，统一刷新（两次 RAF 等 GX 提交）。
    scheduleCaret(api, elements);
    window.requestAnimationFrame((): void => { scheduleCaret(api, elements); });
    api.focus();
    app.focus();
  });
  app.addEventListener("keydown", (ev: KeyboardEvent): void => {
    // Ctrl+Space：补全；Shift+Alt+F：格式化；先于编辑器转发处理。
    if (ev.ctrlKey && ev.key === " ") {
      ev.preventDefault();
      const cursor: Cursor = api.cursor();
      const rect: DOMRect = app.getBoundingClientRect();
      showCompletion(api, elements, cursor, rect.left + 90, rect.top + 150);
      return;
    }
    if (ev.shiftKey && ev.altKey && ev.key.toLowerCase() === "f") {
      ev.preventDefault();
      const formatted: string = api.lspFormat();
      api.setSource(formatted);
      renderDiagnostics(api, elements);
      scheduleCaret(api, elements);
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
    // 输入后刷新诊断与列光标（wasm 侧随每次 key 同步重算，这里只拉结果）。
    window.setTimeout((): void => { renderDiagnostics(api, elements); }, 0);
    scheduleCaret(api, elements);
  });
  // 编辑器容器滚动 / 窗口缩放时，光标覆盖层要跟着代码行移动。
  app.addEventListener("scroll", (): void => { scheduleCaret(api, elements); }, true);
  window.addEventListener("resize", (): void => { scheduleCaret(api, elements); });
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
  renderDiagnostics(api, elements);
  scheduleCaret(api, elements);
  elements.hint.textContent = "就绪 · 点击代码任意位置定位 · Ctrl+Space 补全 · Shift+Alt+F 格式化 · 悬停看符号 · 左侧选示例即运行";
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
