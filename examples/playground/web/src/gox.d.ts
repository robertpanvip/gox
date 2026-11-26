// wasm_exec.js（Go 工具链生成）与 playground wasm 暴露的 Gox API 的严格类型声明。
// 只声明本前端实际用到的面，全部用具体类型，杜绝 any 漏进严格 TS。

/** Go wasm 运行时实例（来自 wasm_exec.js 的全局 Go 构造器）。 */
export interface GoWasmRuntime {
  readonly importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

/** Go 运行时构造器（wasm_exec.js 挂到 window.Go）。 */
export interface GoWasmConstructor {
  new (): GoWasmRuntime;
}

/** 编辑器光标（0 基行 / 列）。 */
export interface GoxCursor {
  readonly line: number;
  readonly col: number;
}

/** wasm 侧暴露的编辑 + 内建 LSP API，由 main.go 用 syscall/js 注册。 */
export interface GoxPlaygroundAPI {
  key(key: string, shift?: boolean, ctrl?: boolean): void;
  /** 解释当前源码并运行（Ctrl+Enter）。 */
  run(): void;
  /** 撤销一步编辑（Ctrl+Z）。 */
  undo(): void;
  /** 重做一步编辑（Ctrl+Shift+Z / Ctrl+Y）。 */
  redo(): void;
  /** 虚拟滚动：按行数移动可见窗口（deltaY 换算，向下为正）。 */
  scrollLines(lines: number): void;
  focus(): void;
  sample(source: string): void;
  cursor(): GoxCursor;
  /** 点击落点换算：行号 + 相对代码行左缘的 x 像素 → 列（与 caret 同源度量）。 */
  pickColumn(line: number, xPixel: number): number;
  setCursor(line: number, col: number): void;
  /** 双击选词：锚点落词首、光标落词尾（CJK 整段成词）。 */
  selectWord(line: number, col: number): void;
  /** 三击选行（含行尾换行符，末行只选内容）。 */
  selectLine(line: number): void;
  /** 拖拽起点：锚点与光标同落一点（即无选区定点）。 */
  setSelect(line: number, col: number): void;
  /** 拖拽移动 / Shift+点击：只动光标，锚点保持。 */
  extendTo(line: number, col: number): void;
  /** 全选（Ctrl+A）。 */
  selectAll(): void;
  clearSelection(): void;
  /** 选中文本；无选区返回空串（Ctrl+C / Ctrl+X 用）。 */
  selectedText(): string;
  deleteSelection(): void;
  /** 粘贴文本（可含换行）。 */
  paste(text: string): void;
  /** 注入实测的窄字符（ASCII）单格宽度（像素）。 */
  setCharWidth(width: number): void;
  /** 注入实测的宽字符（CJK）单格宽度（像素）。 */
  setWideWidth(width: number): void;
  /** 交互后 caret 常亮；静置后恢复闪烁。 */
  settle(): void;
  idle(): void;
  insert(text: string): void;
  setSource(text: string): void;
  lspDiagnostics(): string;
  lspComplete(line: number, col: number): string;
  lspHover(line: number, col: number): string;
  lspDefinition(line: number, col: number): string;
  /** 内嵌 std 源文件全文（定义速览的"打开完整文件"用）；不适用返回 "null"。 */
  stdSource(pkg: string, file: string): string;
  /** 全部引用（声明点+使用点）；不适用返回 "null"。 */
  lspReferences(line: number, col: number): string;

  /** QuickFix 列表（补/删 import）；无诊断返回 "null"。 */
  lspCodeActions(): string;

  /** 语义 token（LSP 编码数组）：{"data":[...]}。 */
  lspSemanticTokens(): string;

  /** 参数提示数组（.gx 0 基坐标）。 */
  lspInlayHints(): string;

  /** 补全项惰性文档：{"doc":"..."}，不可达返回 "null"。 */
  lspResolveDoc(pkg: string, name: string): string;
  lspSymbols(): string;
  lspFormat(): string;
}

/** wasm 加载各阶段耗时（毫秒）。 */
export interface LoadTiming {
  decodeMs: number;
  inflateMs: number;
  compileMs: number;
  readyMs: number;
}

declare global {
  interface Window {
    readonly Go: GoWasmConstructor;
    // Gox 由 wasm 在 Render 完成后注册；加载期可能尚未就绪，故可选。
    readonly Gox?: GoxPlaygroundAPI;
    // file:// 直接打开时由单文件 HTML 注入的 wasm base64；服务器部署时不存在。
    readonly __GX_PLAYGROUND_WASM_B64__?: string;
    // 加载阶段埋点：解码/编译/就绪耗时（毫秒），仅诊断用。
    __GX_TIMING__?: LoadTiming;
  }
}
