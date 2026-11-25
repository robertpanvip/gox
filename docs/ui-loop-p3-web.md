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

## 4. 浏览器待实测清单（P3 验收项，未完成前不切换 Web）

1. `requestAnimationFrame` 帧回调相对 `setTimeout(0)` 回调的先后，与
   `loop.Queue` 检查点接入后 UI 帧一致性（连续 Post 不产生每任务一次 Draw）。
2. Go WASM 回调执行中发生 `queueMicrotask` / Promise 微任务时的交错顺序记录
   （实测前，微任务检查点只允许宿主显式调用 `RunMicrotasks`）。
3. 页面卸载 / `app.Close()` 时：Loop `Stop` 后浏览器侧不再有任务执行、
   Dropped 计数可观测、Promise continuation 观察到调度错误。
4. playground 真实交互（打字/点击/补全）下 RAF executor 全链路无回归。

实测方式：为 playground web 增加集成测试页（浏览器打开，事件时序写入可视
日志并断言），由真实浏览器跑通后更新本文档第 3 节。

## 5. 风险对照（提案 §14）

- 「WASM 调度顺序误判」→ 以 §2.2/§4 受限声明规避，未实测不承诺。
- 「双事件循环」→ Web 保持浏览器单一循环，Loop 若接入仅做队列。
- 「过早引入完整 Promise 规范」→ UIPromise 的 Web 承诺同样以 §4.3 实测为门槛。
