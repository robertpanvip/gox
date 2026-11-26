# GX Playground Windows exe 交叉编译指南

> 记录在 Linux（Ubuntu 22.04）沙箱上把桌面版 Playground 编译成自包含
> Windows x64 exe 的完整流程。产物 `GX-Playground.exe`（约 29 MB）只依赖
> Windows 系统自带 DLL，拷贝到任何 64 位 Windows 双击即可运行。

## 一、产物与验证口径

| 项 | 值 |
|---|---|
| 源码入口 | `examples/playground/desktop`（GLFW + OpenGL + CPU raster blit） |
| 渲染依赖 | `gox-backend-skia`（纯 Go，richardwilkes/canvas）+ `go-gl`（cgo） |
| 布局依赖 | `github.com/ahati/yoga-go-binding`（**cgo C++ 静态库，Windows 需自编译**） |
| 目标 | `GOOS=windows GOARCH=amd64`，GUI 子系统（无控制台黑窗） |
| 依赖检查 | 只允许 KERNEL32 / USER32 / GDI32 / OPENGL32 / SHELL32 / msvcrt |

验证命令（链接完成后立刻确认没有第三方 DLL 依赖）：

```bash
x86_64-w64-mingw32-objdump -p GX-Playground.exe | grep "DLL Name" | sort -u
```

若出现 `libstdc++-6.dll` / `libgcc-1.dll` / `libwinpthread-1.dll`，
说明 C++ 运行库没有静态链接，见 §四。

## 二、工具链安装

```bash
apt-get update
apt-get install -y gcc-mingw-w64-x86-64 g++-mingw-w64-x86-64
which x86_64-w64-mingw32-gcc   # 应输出 /usr/bin/x86_64-w64-mingw32-gcc
```

g++-mingw 包提供 C++ 头文件与 libstdc++（yoga 是 C++ 库，只装 gcc-mingw 会在
链接期报 `cannot find -lstdc++`）。

**注意**：Ubuntu 的 mingw 包把静态库放在 gcc 私有目录
`/usr/lib/gcc/x86_64-w64-mingw32/13-win32/` 下，其中 `libstdc++.dll.a` 是
DLL 导入库、`libstdc++.a` 才是静态库。Go 用 `gcc`（而非 `g++`）驱动最终链接，
`-static-libstdc++` 选项会被 gcc 驱动静默忽略，所以后面统一用
`-extldflags -static`（§四）解决。

## 三、编译 Windows 版 yoga 静态库（关键步骤）

`yoga-go-binding` 模块内只带 **Linux ELF** 格式的 `lib/libyogacore.a`
（可用 `objdump -f` 确认，`elf64-x86-64` 即 Linux 版），Windows 链接必然失败
（`undefined reference to YGValueAuto` 等）。而且模块头文件对齐的是 yoga
**main 分支**（含 `YGNodeLayoutGetRawWidth`、`YGNodeStyleSetFlexBasisFitContent`
等 3.2.1 尚无的 API），因此必须用 main 分支源码编译：

```bash
git clone --depth 1 https://github.com/facebook/yoga.git /tmp/yoga-src
cd /tmp/yoga-src

# 校验：模块头文件应与 main 分支一致（允许仅差 #include <stdbool.h>）
diff yoga/YGNode.h  <yoga-go-binding模块>/include/yoga/YGNode.h

# 逐文件编译（yoga 3.x 需要 C++20 concepts，-std 必须是 c++20）
for f in $(find yoga -name "*.cpp"); do
  x86_64-w64-mingw32-g++ -std=c++20 -O2 -DNDEBUG -I. -c "$f" -o "${f%.cpp}.o"
done

mkdir -p /tmp/yoga-win && ar rcs /tmp/yoga-win/libyogacore.a $(find yoga -name "*.o")

# 自检：PE 目标格式 + 关键符号齐全
x86_64-w64-mingw32-nm /tmp/yoga-win/libyogacore.a | grep "T YGNodeLayoutGetRawWidth\|R YGValueAuto"
```

然后做一个**本地模块副本**（module cache 只读，直接改会被 go 校验拒绝）：

```bash
YB=$(go env GOMODCACHE)/github.com/ahati/yoga-go-binding@v0.1.0
cp -r $YB /tmp/yoga-win-binding && chmod -R u+w /tmp/yoga-win-binding
cp /tmp/yoga-win/libyogacore.a /tmp/yoga-win-binding/lib/libyogacore.a
```

在 `examples/playground/go.mod` 追加一行（**仅构建期需要，构建完必须还原**）：

```
replace github.com/ahati/yoga-go-binding => /tmp/yoga-win-binding
```

## 四、链接成自包含 exe

```bash
cd examples/playground

CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
go build -ldflags "-H windowsgui -extldflags -static" \
  -o GX-Playground.exe ./desktop

# 构建完立刻还原 go.mod，避免把本地 replace 提交进仓库
git checkout -- go.mod
```

各 `-ldflags` 选项的作用：

| 选项 | 作用 | 缺了会怎样 |
|---|---|---|
| `-H windowsgui` | GUI 子系统，双击运行不弹黑色控制台 | 每次启动多一个控制台窗口 |
| `-extldflags -static` | libstdc++/libgcc/mingw 运行时全部静态链入 | 用户机器报缺少 `libstdc++-6.dll` |

> `-static-libstdc++` 单独传无效：Go 用 gcc 驱动链接，该选项是 g++ 驱动专属，
> 会被静默忽略。整链 `-static` 是可靠做法（系统 DLL 不受影响，仍动态加载）。

## 五、踩坑清单（按遇到顺序）

1. **`cannot find -lstdc++`** → 只装了 gcc-mingw，补 `g++-mingw-w64-x86-64`。
2. **yoga 一堆 `undefined reference`（YGValueAuto 等）** → 模块内置静态库是
   Linux ELF，Windows 必须按 §三重编。
3. **换了 yoga 3.2.1 仍缺 `YGNodeLayoutGetRawWidth` 等** → 模块头文件来自
   yoga main 分支，新 API 在 3.2.1 里还没有；必须编 main 分支源码。
4. **`fatal error: yoga/Yoga.h: No such file`** → 需 `-I`（yoga 仓库根），
   源码内是 `#include <yoga/...>`。
5. **`'concept' does not name a type`** → yoga 3.x 用 C++20，`-std` 不能低于
   c++20。
6. **`-static-libstdc++` 传了没生效**（exe 仍导入 libstdc++-6.dll）→ 见 §四，
   用整链 `-static`。
7. **`-extldflags` 改了但产物没变** → 确认命令真的进入链接步骤（`go build -x`
   grep `extldflags`）；若被缓存复用，检查 go.mod 的 replace 是否生效
   （build 缓存键含链接参数，replace 失效时会用旧库报错而不是静默跳过）。
8. **go 校验拒绝本地 replace** → replace 目标必须是完整模块副本（含 go.mod），
   且目录可写；不能直接改 `$GOMODCACHE` 内的原模块（只读 + 哈希校验）。

## 六、Linux 本机版（对照）

同目录直接 `go build -o gx-playground ./desktop` 即可。cgo 需要的 X11 开发库
由 `linkflags.go` 的 `#cgo linux LDFLAGS` 指向仓库根 `.demolibs/`（内含 X11
系列运行时 .so 软链），沙箱无需额外安装系统包。

## 七、一条龙脚本（摘自实际构建记录）

```bash
# 0) 工具链
apt-get install -y gcc-mingw-w64-x86-64 g++-mingw-w64-x86-64

# 1) Windows 版 yoga
git clone --depth 1 https://github.com/facebook/yoga.git /tmp/yoga-src
cd /tmp/yoga-src
for f in $(find yoga -name "*.cpp"); do
  x86_64-w64-mingw32-g++ -std=c++20 -O2 -DNDEBUG -I. -c "$f" -o "${f%.cpp}.o"
done
mkdir -p /tmp/yoga-win && ar rcs /tmp/yoga-win/libyogacore.a $(find yoga -name "*.o")

# 2) 本地模块副本
YB=$(go env GOMODCACHE)/github.com/ahati/yoga-go-binding@v0.1.0
cp -r $YB /tmp/yoga-win-binding && chmod -R u+w /tmp/yoga-win-binding
cp /tmp/yoga-win/libyogacore.a /tmp/yoga-win-binding/lib/libyogacore.a

# 3) 构建（go.mod 先加 replace，构建后还原）
cd examples/playground
echo "replace github.com/ahati/yoga-go-binding => /tmp/yoga-win-binding" >> go.mod
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
go build -ldflags "-H windowsgui -extldflags -static" -o GX-Playground.exe ./desktop
git checkout -- go.mod

# 4) 验证
x86_64-w64-mingw32-objdump -p GX-Playground.exe | grep "DLL Name" | sort -u
# 期望：GDI32 / KERNEL32 / msvcrt / OPENGL32 / SHELL32 / USER32，无第三方 DLL
```
