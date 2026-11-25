# UI 调度循环 P3：Web/WASM 适配边界与验证记录

> 对应提案 §5.2 / §12 P3。原则：Web 端复用浏览器事件循环，Loop 在 Web 只做
> 队列与适配；**只在实际验证通过的平台能力上声明语义**。本文档区分
> 「沙箱已验证」「浏览器待实测」「明确不承诺」三类。

## 1. 现状事实

- `backend/web` 的 RAF UIExecutor（`dom_js.go`）已经是「复用浏览器事件循环」
  的实现：同一帧内多次 `Post` 去抖登记，只挂一次 `requestAnimationFrame`，
  在帧回调里批量执行；Go 侧没有第二套事件循环。
- `examples/playground/web` 通过 `app.SetUIExecutor(win)` 用该 RAF executor
  驱动绘制与 signal 投递桥接。
- js/wasm 宿主提供 `setTimeout` / `queueMicrotask`（前端加载器已在使用），
  但「Go 回调 ↔ 浏览器微任务」的交错顺序未经实测。

## 2. 阶段一决议

1. **Web 不切换到 `loop.Queue`**。现有 RAF UIExecutor 的语义等价于
   Loop 的宏任务投递面（Post = 下一宿主轮次执行，不在调用栈同步跑），
   且已是线上验证过的路径。切换前提 = §4 的浏览器实测全部通过。
2. **不把 `PostMicro` 映射为 `queueMicrotask`**（提案 §5.2 明确禁止把
   Post/PostMicro 都映射成同一种浏览器 API；也未验证 Go WASM 回调与原生
   JS 微任务的实际顺序）。Web 的微任务能力在实测前标记为**受限能力**：
   仅当宿主（rAF 帧回调 / 事件回调尾部）显式调用 `RunMicrotasks` 检查点
   时才执行，与 §4.1 的宿主节奏定义一致。
3. `loop.Queue` 本身在 GOOS=js 编译通过并在 node wasm 宿主全量测试绿色，
   说明队列/预算/生命周期/Promise/watchdog 语义不依赖特定宿主 API——
   Web 适配被卡住的只有 §4 的时序实测项。

## 3. 沙箱已验证（可复跑）

运行方式：`GOOS=js GOARCH=wasm go test -c -o /tmp/loop_wasm.test ./runtime/loop/ && node scripts/runwasm_test.js /tmp/loop_wasm.test`

| 项 | 结果 |
| --- | --- |
| 完整 loop/promise/watchdog 套件于 js/wasm（node 宿主） | PASS |
| `TestWasmPostRunsOnlyInHostTurn`：Post 仅经宿主轮次执行、调用栈内不同步 | PASS |
| `TestWasmMicroCheckpointOrder`：宿主轮内宏→微顺序 | PASS |
| `TestWasmPromiseContinuationHostTurn`：continuation 不在注册栈同步执行 | PASS |
| `TestWasmStopRejectsAndDrops`：Stop 后 ErrClosed、任务丢弃 | PASS |
| `GOOS=js GOARCH=wasm go vet ./runtime/loop/` | 通过 |
| linux `go test -race` 回归 | 通过 |

注：node 宿主与浏览器同属 JS 事件循环语义（setTimeout/queueMicrotask 的
宏/微分层一致），但 **rAF 回调时序、页面卸载行为、真实浏览器并发** 与 node
不等价——下列项目不得因 node 通过而声明。

## 4. 真实浏览器实测记录（2026-11-25，沙箱 Chrome headless）

工具：`scripts/runwasm_browser.js`（CDP 驱动 Chrome 跑 GOOS=js 测试二进制，
console 经 CDP 收集判定 PASS/FAIL）、`scripts/browser_smoke.js`（真实加载
`playground.html` 单文件构建并模拟双击/键入）。宿主：**Google Chrome
158.0.8127.36 --headless=new**。

| §12 P3 验收项 | 实测结果 |
| --- | --- |
| Post 的浏览器调度行为 | 完整 loop 套件 39 用例在 Chrome 内 PASS（含 `TestWasmPostRunsOnlyInHostTurn` 调用栈不执行契约） |
| PostMicro 的调度行为 + 微任务嵌套投递 | `TestBrowserNativeMicrotaskBeforeLoopCheckpoint` PASS：`Promise.then` / `queueMicrotask` 均先于宿主轮次，Loop 微任务在同一轮次内、宏任务之后收尾——「Loop 微任务 ≠ 浏览器原生微任务」升级为浏览器实测结论 |
| rAF 宿主节奏 | `TestBrowserRAFAndTimeoutBothArrive`：rAF 与 setTimeout(0) 均到达（本次观测顺序 [timeout, raf]，跨引擎先后不做承诺）；`TestBrowserLoopMacroMicroViaRAF`：rAF 帧轮内宏→微成立 |
| Go WASM callback 与浏览器事件顺序 | 由上述交错断言固化；未来漂移会直接打破测试而非静默变化 |
| 页面关闭/App 关闭后的任务处理 | 浏览器内 `TestWasmStopRejectsAndDrops` PASS；playground 冒烟中交互后无残留异常 |
| 真实 playground 全链路 | `SMOKE_OK`：首帧 rows=24；双击第 0 行词「package」→ 选区色块宽 54.78px、光标 col=7；键入 'x' 替换选区 → 光标 col=1；caret 可见；无任何页面异常 |
| 卸载（unload）时序、真实 GPU 帧节奏、Safari/Firefox、真实键鼠硬件 | 沙箱无法覆盖，**仍需用户侧验证**；Web 切换 loop.Queue 前建议补这三项 |

复跑命令：
```
GOOS=js GOARCH=wasm go test -c -o /tmp/loop.test.wasm ./runtime/loop/
node scripts/runwasm_browser.js /tmp/loop.test.wasm -- -test.v -test.timeout 200s
node scripts/browser_smoke.js examples/playground/web/playground.html
```

## 5. 风险对照（提案 §14）

- 「WASM 调度顺序误判」→ 以 §2.2/§4 受限声明规避，未实测不承诺。
- 「双事件循环」→ Web 保持浏览器单一循环，Loop 若接入仅做队列。
- 「过早引入完整 Promise 规范」→ UIPromise 的 Web 承诺同样以 §4.3 实测为门槛。
