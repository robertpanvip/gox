// wasm_exec.js 由 Go 工具链生成（不在本仓手写），这是它的最小严格类型声明。
// 只声明本前端实际用到的面，全部用 unknown / 只读类型，杜绝 any 漏进严格 TS。

/** Go wasm 运行时实例（来自 wasm_exec.js 的全局 Go 构造器）。 */
interface GoWasmRuntime {
  /** 提供给 WebAssembly.instantiate 的 import 对象。 */
  readonly importObject: WebAssembly.Imports;
  /** 运行已实例化的 wasm 模块；Go main 返回时 Promise resolve。 */
  run(instance: WebAssembly.Instance): Promise<void>;
  /** 请求运行时以给定退出码结束。 */
  exit(code: number): void;
}

/** Go 运行时构造器（wasm_exec.js 挂到 window.Go）。 */
interface GoWasmConstructor {
  new (): GoWasmRuntime;
}

declare global {
  interface Window {
    readonly Go: GoWasmConstructor;
  }
}

export {};
