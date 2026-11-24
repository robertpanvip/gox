// GX Playground 前端加载器 —— 严格 TypeScript 7。
//
// 职责：
//   1. 加载 wasm_exec.js 提供的 Go 运行时 → 实例化 app.wasm → go.run；
//   2. 等待 wasm 暴露 Gox API 后，把浏览器键盘事件翻译成 Gox.key() 调用。
// 业务渲染与编辑逻辑全部在 wasm 里（GX 源码编译产物 + gxweb 后端），
// 本文件只做引导与输入转发，不含任何绘制/编辑逻辑，全程零 any。

interface Elements {
  readonly loading: HTMLElement;
  readonly app: HTMLElement;
  readonly hint: HTMLElement;
}

interface GoxPlaygroundAPI {
  key(key: string): void;
  focus(): void;
  sample(source: string): void;
}

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
    hint: requireElement("hint"),
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

/**
 * 轮询等待 wasm 侧注册 Gox API。wasm 的 go.run 里先 Render 再注册全局，
 * 因此这里用短间隔重试，直到接口就绪或超时。
 */
async function waitForGox(timeoutMs: number = 5000): Promise<GoxPlaygroundAPI> {
  const started: number = Date.now();
  while (Date.now() - started < timeoutMs) {
    const api: GoxPlaygroundAPI | undefined = window.Gox;
    if (api !== undefined && typeof api.key === "function") {
      return api;
    }
    await new Promise<void>((resolve: () => void): number => window.setTimeout(resolve, 30));
  }
  throw new Error("等待 Gox 编辑 API 超时");
}

/** 把 KeyboardEvent 映射为 Gox.key 的参数；可打印字符直接取 e.key。 */
function keyName(ev: KeyboardEvent): string {
  return ev.key;
}

/** 绑定键盘输入到 GX 编辑命令，并让挂载点可聚焦。 */
function bindKeyboard(app: HTMLElement, api: GoxPlaygroundAPI): void {
  app.tabIndex = 0;
  app.addEventListener("mousedown", (): void => {
    api.focus();
    app.focus();
  });
  app.addEventListener("keydown", (ev: KeyboardEvent): void => {
    // 阻止方向键/退格滚动页面，让编辑器独占这些键。
    if (ev.key === "Backspace" || ev.key.startsWith("Arrow")) {
      ev.preventDefault();
    }
    api.key(keyName(ev));
  });
}

/** 把 base64 解码为字节缓冲，供 file:// 下离线实例化 wasm，不依赖 fetch。 */
function decodeBase64(base64: string): ArrayBuffer {
  const binary: string = atob(base64);
  const buffer: ArrayBuffer = new ArrayBuffer(binary.length);
  const bytes: Uint8Array = new Uint8Array(buffer);
  for (let i: number = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return buffer;
}

/**
 * 实例化 wasm：单文件 file:// 打开时用内嵌 base64；服务器部署时回退 fetch。
 * 浏览器在 file:// 下禁止 fetch 本地文件，故必须走 base64 分支。
 */
async function instantiateWasm(go: InstanceType<Window["Go"]>): Promise<WebAssembly.WebAssemblyInstantiatedSource> {
  const embedded: string | undefined = window.__GX_PLAYGROUND_WASM_B64__;
  if (embedded !== undefined && embedded.length > 0) {
    return await WebAssembly.instantiate(decodeBase64(embedded), go.importObject);
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

  // Gox API 在 go.run 内部注册；启动 run（不等待其 resolve，正常运行期间
  // main 里的 select{} 让它永不返回），随后等待 API 就绪并绑定键盘。
  void go.run(result.instance);
  const api: GoxPlaygroundAPI = await waitForGox();
  bindKeyboard(elements.app, api);
  elements.hint.textContent = "就绪 · 点击预览区聚焦，键盘输入实时进入 GX 编辑器";
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
