// Web 版 Apple 组件库 showcase 的前端加载器 —— 严格 TypeScript。
//
// 职责：加载 wasm_exec.js 提供的 Go 运行时 → 实例化 app.wasm → 运行。
// 场景树渲染、Apple 主题、矢量图标、主题切换全部在 wasm 里完成（共享 showcase 包），
// 本文件只做引导与错误呈现，不含任何绘制逻辑。

interface Elements {
  readonly loading: HTMLElement;
  readonly app: HTMLElement;
}

/** 按 id 取元素，取不到直接抛错——让引导期问题尽早暴露而非静默白屏。 */
function requireElement(id: string): HTMLElement {
  const el: HTMLElement | null = document.getElementById(id);
  if (el === null) {
    throw new Error(`缺少挂载元素 #${id}`);
  }
  return el;
}

function collectElements(): Elements {
  return {
    loading: requireElement("loading"),
    app: requireElement("app"),
  };
}

/** unknown 收紧后不能用 error.message 直接访问。 */
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

/**
 * 引导 wasm。Go showcase 的 main 里有 select{}，正常运行期间 go.run 不 resolve。
 * 首帧由 app.Render 同步绘制，故移除 loading 提示的时机放在 go.run 之前。
 */
async function boot(elements: Elements): Promise<void> {
  assertWasmSupport();

  const go = new window.Go();
  const response: Response = await fetch("app.wasm");
  if (!response.ok) {
    throw new Error(`获取 app.wasm 失败: HTTP ${response.status}`);
  }

  const result: WebAssembly.WebAssemblyInstantiatedSource =
    await WebAssembly.instantiateStreaming(response, go.importObject);

  elements.loading.remove();
  await go.run(result.instance);
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
