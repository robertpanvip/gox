# wasm-demo —— Gox Web 后端最小示例

把「场景树 + Web 后端」一起编译成 WebAssembly，在浏览器里用真实 DOM 渲染。

## 目录说明

- `main.go`（`//go:build js && wasm`）：装配层 + 计数器场景树。场景树等价于 GX 编译器从 JSX 产出的 `gxscene.Render(Template{...})`，可直接替换为 `.gx` 编译产物。
- `main_stub.go`：native 下的空 `main`，让 `go build ./...` 不因 build tag 报错。
- `src/main.ts`：**严格 TypeScript** 前端加载器（fetch → instantiateStreaming → go.run + 错误呈现）。
- `src/wasm-exec.d.ts`：wasm_exec.js 全局 `Go` 的最小严格类型声明。
- `tsconfig.json`：TypeScript 7 严格配置（`strict` + `noUncheckedIndexedAccess` + `exactOptionalPropertyTypes` + `noUnusedLocals` 等全套），全程零 `any`。
- `index.html`：加载 wasm_exec.js + `dist/main.js`，挂载点是 `#app`。
- `build.sh`：一键「TS 严格检查/编译 → wasm 编译 → 拷贝运行时」。

## 构建与运行

```bash
cd examples/wasm-demo
bash build.sh                    # TS 严格编译 + 产出 app.wasm / wasm_exec.js / dist/main.js
python3 -m http.server 8080      # 需 HTTP 服务（wasm 不能 file:// 加载）
# 浏览器打开 http://localhost:8080/
```

前端用 **TypeScript 7.0.2**（`npm install` 本地固定版本），严格模式编译：

```bash
npm install              # 安装 typescript@7.0.2（已锁定精确版本）
npm run typecheck        # tsc --noEmit，严格全套，零错误
npm run build            # tsc → dist/main.js
```

`tsconfig.json` 开启的严格项：`strict`、`noImplicitAny`、`strictNullChecks`、`useUnknownInCatchVariables`（catch 用 `unknown` + 类型守卫）、`noUnusedLocals/Parameters`、`noUncheckedIndexedAccess`、`exactOptionalPropertyTypes`、`noPropertyAccessFromIndexSignature`、`noImplicitReturns`、`noFallthroughCasesInSwitch`、`noImplicitOverride`、`verbatimModuleSyntax`。前端源码不含任何 `any`；wasm_exec.js 是 Go 工具链生成的运行时胶水，仅通过 `wasm-exec.d.ts` 以只读类型接入。

点击「点我 +1」：signal 写入 → gxapp 精确更新场景树文本 → Web 后端按 `ChangeSetText` 增量打补丁到 DOM —— 与桌面 Skia/Rust 后端走的是同一条 gxapp 帧管线。

## 与 GX 配合

`main.go` 里的 `view()` 就是编译产物的形态。用 GX 写界面时：

1. 写 `counter.gx`（JSX + signal）；
2. `gox build counter.gx -o counter.go` 生成 `view()`；
3. 保留 `main()` 装配样板不变，`GOOS=js GOARCH=wasm go build` 即可。

后端侧不需要为 wasm 写任何 glue：DOM 事件由 Web 后端的 `FireEvent` 统一回投到场景树。

## 后端能力对照

| gxapp 契约 | Web 后端实现 |
|---|---|
| `Window` | `Open`/`Draw`/`Run`(立即返回)/`Close`/`Config` |
| `FramePresenter` | `Present`：首帧/FullSync 全量重建，其余按 Changes 增量 |
| `Focuser` | `Focus(id)` → DOM `element.focus()` |
| `Measurer` | `Measure(id)` → `getBoundingClientRect()` |
| `ComputedStyler` | `ComputedStyle(id, state)` → `getComputedStyle()` |

平台分层：`dom_js.go`（`js && wasm`，syscall/js 真实 DOM）与 `dom_mem.go`（其他平台，内存镜像供单测），两者共用 `Driver` 接口；`go build ./...` 在原生平台不会触碰 syscall/js。
