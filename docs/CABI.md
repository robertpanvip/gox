# C-ABI / JNA 集成

`bindings/cabi` 把 GX 转译器暴露为 C 共享库，供 IntelliJ IDEA 插件经 JNA 调用。

## 构建

需要 CGO 与 Go 1.27：

```bash
cd gox
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=1 \
  go build -buildmode=c-shared -o libgox.so ./bindings/cabi
```

产出：
- `libgox.so` — 共享库
- `libgox.h` — 生成的头文件（含函数签名）

## 导出函数

| 函数 | 签名 | 说明 |
|---|---|---|
| `gx_translate` | `char* gx_translate(char* source)` | 转译 GX 源码，返回 JSON |
| `gx_validate` | `char* gx_validate(char* goSource)` | 校验 Go 源码语法 |
| `gx_map_lookup` | `char* gx_map_lookup(char* mapJson, int line, int col)` | 位置反查 |
| `gx_version` | `char* gx_version()` | 版本字符串 |
| `gx_free` | `void gx_free(void* ptr)` | **必须调用**以释放返回字符串 |

`gx_translate` 返回：

```json
{
  "goSource": "package main\n...",
  "sourceMap": "{\"version\":1,...}",
  "error": ""
}
```

所有返回字符串由 Go 侧 `C.CString` 分配，**调用方必须用 `gx_free` 释放**，否则内存泄漏。

## Java / JNA 绑定示例

```java
import com.sun.jna.Library;
import com.sun.jna.Native;
import com.sun.jna.Pointer;

public interface GoxLib extends Library {
    GoxLib INSTANCE = Native.load("gox", GoxLib.class);

    Pointer gx_translate(String source);
    Pointer gx_validate(String goSource);
    Pointer gx_map_lookup(String mapJson, int line, int col);
    Pointer gx_version();
    void gx_free(Pointer ptr);
}
```

调用并释放：

```java
Pointer p = GoxLib.INSTANCE.gx_translate(source);
try {
    String json = p.getString(0, StandardCharsets.UTF_8);
    // 解析 json → goSource / sourceMap
} finally {
    GoxLib.INSTANCE.gx_free(p);
}
```

> `Native.load("gox", ...)` 会在系统库路径查找 `libgox.so`；也可用绝对路径 `Native.load("/path/to/libgox.so", ...)`。确保运行时 `--app-bindings` 或 `jna.library.path` 指向库所在目录。

## 与 LSP 的分工

- **IDE 编辑体验**（补全、跳转、hover、实时诊断）走独立 LSP server（`gox-lsp`，stdio）。
- **一次性转译 / 校验 / 位置映射**走 C-ABI（同步调用，低延迟）。

两者共享 `pkg/compiler` 与 `pkg/sourcemap`，行为一致。
