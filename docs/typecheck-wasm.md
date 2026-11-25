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
