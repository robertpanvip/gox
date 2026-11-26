# Playground 浏览器内类型检查层（embedstd）实现记录

## 机制
- `internal/typecheck`：`Lookup` 虚拟文件系统接口 + `FSRoot(fs.FS)` 通用实现；
  `Importer` 持久会话（std/gox 类型图一次构建跨编辑复用）；诊断只经 Error 回调
  收集（返回首错与回调同源，重复收集会翻倍——`TestCheckNoDuplicateError` 锁定）。
- `cmd/gox-stdlib-gen`：构建期生成器。闭包 = `go list -deps ./runtime/... ./widgets/... ./pkg/sourcemap`，
  过滤复用 `DiskLookup` 同一套 GOOS/GOARCH/build-tag/cgo 规则（无第二份名单），
  输出 `_srcs/{std,gox}` + `VERSION`（锁 go 版本，生成与编译必须同工具链）。
- `internal/typecheck/embedstd`：`//go:embed all:_srcs`；未生成数据时 `Lookup()=nil`
  → goxlsp 类型层整体缺席（零行为变化安全阀）；数据目录带下划线前缀，go 工具链
  与 .gitignore 双重隔离，仓库零增长。
- `pkg/goxlsp/session`：GoSource 非空且 embedstd 就绪时，诊断经
  `SourceMap.GoToGx` 回投 .gx 坐标，Code="type"，持久 Importer 挂会话。

## 实测数字（2026-11-25，go1.27.4，Chrome 158 headless）
| 指标 | 数值 |
|---|---|
| 生成源码量 | 197 包 / 10.4MB（js/wasm 视图；linux-only/cgo 文件天然排除） |
| app.wasm | ≈29MB（raw，嵌入源码为主要增量） |
| playground.html 单文件 | 6.26MB → **10.28MB**（+4.0MB，优于 4.6MB 预估） |
| 浏览器内首次类型检查就绪 | **9ms**（setSource→type 诊断可读，含 std 图首建） |
| 诊断质量 | `4:16 type: cannot use n (variable of type int) as string value...`，坐标回投 .gx，零 `check.go` 泄露 |

## 使用
```
go run ./cmd/gox-stdlib-gen                     # 构建 wasm 前生成（playground build.sh 应接入）
GOOS=js GOARCH=wasm go build ... examples/playground/web
node scripts/selfcheck.js gsyntax <playground.html 绝对路径>   # 类型层端到端探针
```
类型层覆盖白名单闭包（gox runtime/widgets/pkg + std 197 包）；用户 import 白名单外
包时产生一条 could-not-import 提示后继续（优雅降级）。完整 go build 级兜底仍在桌面
gox-lsp / CLI `gox check -check`。

## 类型感知成员补全（P1+）

点号上下文（`b.`、`strings.`）的补全从正则符号表升级为 go/types 真实类型查询，
浏览器与桌面同源：

- 核心：`internal/typecheck.Complete(src, byteOffset, lookup)` —— 光标前插
  占位标识符补全语法 → 定位点号左侧表达式 → 持久 Importer 求类型 →
  枚举包级导出成员 + 方法集；跨包未导出成员过滤；无新依赖（未引入 x/tools）。
- 浏览器（pkg/goxlsp → Gox.lspComplete）：数据源 `embedstd.Lookup()`。
- 桌面（internal/lsp）：数据源 `typecheck.DiskLookup(GOROOT/src, GOOS, GOARCH)`，
  惰性建会话；诊断仍走原有 go build 管线，类型层零干扰。
- 零回归契约：非点号上下文原样返回旧符号表；无模块根/数据未生成时整体回退。
- 坐标链：`.gx (line,col)` → GxToGo → Go 字节偏移（1 基）→ 候选按 Go 字节
  偏移回投 `.gx`（UTF-16 安全）；构建脚本先跑 `cmd/gox-stdlib-gen` 保证
  embed 数据与 wasm 同工具链。

实测（headless Chrome 158，`node scripts/selfcheck.js comp playground.html`）：
`{"typeHit":true,"kind":2,"detail":"func(s string) string","fallbackItems":17}`
——*strings.Replacer 上补出 Replace 方法带真实签名，非点号路径 17 项候选不丢。

## 参数签名提示（signature help）

光标位于调用实参区（触发字符 `(` / `,`）时，浏览器与桌面 LSP 均给出真实签名：

- 核心：`internal/typecheck/signature.go`——对未写完的调用，截断光标后缀、注入占位
  实参 `GxSigX_`，并按未闭合括号栈（类型感知配对，跳过字符串/注释/字面量）倒序补齐
  闭合符，还原成可解析 probe；再从持久 Importer 的类型图读取被调 `*types.Signature`。
  Label 与各参数字节区间遵循 LSP `ParameterInformation.label` 约定；active 为顶层
  逗号数并随可变参数钳位。内建函数（append/len 等）在 go/types 无公开签名访问器，
  按"无提示"降级，不猜测。
- 浏览器：`Gox.lspSignature(line, col)`（`pkg/goxlsp`，数据源 embedstd）。
- 桌面：`textDocument/signatureHelp`，触发字符已在 initialize 注册（数据源本机 GOROOT）。
- 零回归契约：非调用上下文 / 类型层缺席 / 坐标映射失败 → null 或空 help，
  编辑器隐藏提示即可。

实测（headless Chrome 158，`node scripts/selfcheck.js sig playground.html`）：
`{"ok":true,"label":"Sprintf(format string, a ...any) string"}`——括号后与逗号后的
active 分别为 0/1，非调用位置返回 null；单测红→绿对照见
`internal/typecheck/signature_test.go`。
