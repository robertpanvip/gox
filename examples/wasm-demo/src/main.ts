// wasm-demo 前端加载器 —— 严格 TypeScript。
//
// 职责：加载 wasm_exec.js 提供的 Go 运行时 → 实例化 app.wasm → 运行。
// 业务渲染全部在 wasm 里（场景树 + gxweb 后端经 syscall/js 操作 DOM），
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

/** 收集前端需要的 DOM 节点。 */
function collectElements(): Elements {
  return {
    loading: requireElement("loading"),
    app: requireElement("app"),
  };
}

/** 把异常规整为可读文案；unknown 收紧后不能用 error.message 直接访问。 */
function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === "string") {
    return err;
  }
  return "未知错误";
}

/** 显示加载失败，并把节点从「加载中」切换为错误态。 */
function showError(loading: HTMLElement, err: unknown): void {
  loading.textContent = `加载失败: ${errorMessage(err)}`;
  loading.setAttribute("data-state", "error");
}

/** 校验浏览器是否支持 WebAssembly.instantiateStreaming。 */
function assertWasmSupport(): void {
  if (typeof WebAssembly === "undefined") {
    throw new Error("当前浏览器不支持 WebAssembly");
  }
}

/**
 * 引导 wasm：fetch → instantiateStreaming → go.run。
 * go.run 的 Promise 在 Go main 返回时 resolve；demo 的 main 里有 select{}，
 * 故正常运行期间它不会 resolve——事件循环由浏览器 DOM 驱动。
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

  // 首帧已由 app.Render 同步绘制完成，此时可移除加载提示。
  elements.loading.remove();

  await go.run(result.instance);
}

function main(): void {
  const elements: Elements = collectElements();
  void boot(elements).catch((err: unknown): void => {
    showError(elements.loading, err);
  });
}

// wasm_exec.js 是普通 <script>（非 module），它先于本脚本执行，
// 因此 window.Go 此时一定可用；DOMContentLoaded 后运行保证挂载点已就绪。
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", main, { once: true });
} else {
  main();
}
