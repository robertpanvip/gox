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
  key(key: string): void;
  focus(): void;
  sample(source: string): void;
  cursor(): GoxCursor;
  /** 点击落点换算：行号 + 相对代码行左缘的 x 像素 → 列（与 caret 同源度量）。 */
  pickColumn(line: number, xPixel: number): number;
  setCursor(line: number, col: number): void;
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
