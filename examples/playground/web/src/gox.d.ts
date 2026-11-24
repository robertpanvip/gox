// wasm_exec.js（Go 工具链生成）与 playground wasm 暴露的 Gox API 的严格类型声明。
// 只声明本前端实际用到的面，全部用只读/具体类型，杜绝 any 漏进严格 TS。

/** Go wasm 运行时实例（来自 wasm_exec.js 的全局 Go 构造器）。 */
interface GoWasmRuntime {
  readonly importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

/** Go 运行时构造器（wasm_exec.js 挂到 window.Go）。 */
interface GoWasmConstructor {
  new (): GoWasmRuntime;
}

/**
 * wasm 侧暴露的编辑 API。由 main.go 用 syscall/js 注册：
 * key 把浏览器按键翻译成 GX 编辑命令，focus 聚焦编辑器。
 */
interface GoxPlaygroundAPI {
  key(key: string): void;
  focus(): void;
  sample(source: string): void;
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

/** wasm 加载各阶段耗时（毫秒）。 */
interface LoadTiming {
  decodeMs: number;
  inflateMs: number;
  compileMs: number;
  readyMs: number;
}

export {};
