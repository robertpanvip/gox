# gx_gpu —— GX 独立 GPU 渲染管线（quad 管线）

从 Rust 桌面后端（`backend/rust`）抽离的独立渲染 crate。**不依赖 GX 场景协议、
布局或任何 GX 模块**，只消费一帧 GPU 形状的绘制指令（`GpuFrame`）。

## 三条架构约束

1. **GPU 优先**：默认构建走 wgpu——一份 WGSL shader 同时服务
   Vulkan（Linux/Android）、Metal（macOS）、DX12（Windows），无 per-platform
   图形代码。
2. **CPU 接管同一套 API（打包期选型，非运行时检测）**：绘制指令统一表达为
   `GpuFrame`（纯数据）。tiny-skia 软光栅实现与 wgpu 实现语义相同，但两套
   实现**不同时编译**，由打包命令选定，包体积各自最小：

   ```bash
   # GPU 版（默认）
   cargo build --release
   # CPU 版（无 wgpu 依赖，体积极小；离屏光栅）
   cargo build --release --no-default-features --features cpu
   ```

   GPU 构建下适配器不可用直接报错返回，不静默回退。API（`GpuFrame` /
   C ABI）在两种构建下完全一致，调用方代码零改动。
3. **独立抽离**：三种消费方式——
   - **rlib**：Rust 后端（`backend/rust`）`use gx_gpu` 直接驱动；
   - **staticlib**：Go 侧 CGO（含 `backend/skia` 的 Go 桌面端）链接
     `libgx_gpu.a`，按 `gx_gpu.h` 调用；
   - **staticlib**：Android NDK 经 JNI 直调同一份 C ABI
     （`ANativeWindow*` 句柄已接通）。

## 绘制模型

| 图元 | 内容 | 实现 |
|---|---|---|
| `Prim::Quad` | SDF 圆角矩形：纯色 / 描边环 / 线性渐变 | GPU：instanced quad + SDF shader；CPU：tiny-skia 路径 |
| `Prim::Tex` | 纹理四边形：图片/视频/canvas 光栅，UV 窗口 + 圆角遮罩 | GPU：纹理管线；CPU：窗口切片 + 变换 |

- **裁剪**：上层把几何与裁剪矩形求交后下发；GPU 端另设 scissor 硬裁软边
  溢出（连续同裁剪图元合并为一个批次，切换批次时更新 scissor）。
- **混合**：全链路预乘 alpha、gamma 空间（表面格式 `Bgra8Unorm`，写入即
  显示）——与 tiny-skia / Skia raster / 浏览器 canvas 同口径，GPU/CPU 两后端
  视觉语义一致。
- **渐变**：CSS 角度语义（0°=向上），色标经 uniform 表（每帧 64 渐变 ×
  8 槽），instance 只带索引；与 Go `paintGradientBackgroundShader` 同算法。
- **阴影不在后端**：上层按旧 6 层近似算法分解成多条 Quad（与 Skia 端
  `paintShadow` 同算法），两个后端自然一致。

## C ABI（gx_gpu.h）

```c
void *dev = gx_gpu_new(1280, 800, (uintptr_t)Display*, (uintptr_t)Window);
float clear[4] = {16, 24, 48, 255};
gx_gpu_begin(dev, 1280, 800, clear);
gx_gpu_upload_rgba(dev, 1, key, w, h, rgba, len, 1);
GxGpuQuad q = {...};
gx_gpu_quad(dev, &q);
GxGpuTex t = {...};
gx_gpu_tex(dev, &t);
gx_gpu_end(dev);            /* GPU 版 present；CPU 版光栅到缓冲 */
gx_gpu_read_rgba(dev, buf, cap);  /* 仅 CPU 版可回读像素 */
gx_gpu_free(dev);
```

Go 侧（后续接入）：cgo LDFLAGS 链接 `target/release/libgx_gpu.a`，结构体按
`gx_gpu.h` 布局（单测 `cquad_layout_matches_documented_c_struct` 锁定大小）。

## 测试

- **CPU 后端像素级单测**（无 GPU 依赖，13 项）：纯色/圆角/渐变中点/描边环/
  纹理 UV 窗口/上传去重/半透明。
- **GPU 后端离屏单测**（lavapipe/llvmpipe 可跑，无窗口）：WGSL 编译 + 管线
  创建 + 全图元离屏 draw 不 panic；纹理 (id,key) 去重。

```bash
cargo test                                 # GPU 版（含离屏渲染）
cargo test --no-default-features --features cpu   # CPU 版像素断言
```

## 已知边界（诚实声明）

- 渐变色标上限 8（uniform 表 64 个/帧；超出的渐变退化为纯色，正常 UI 不会触达）。
- CPU 后端纹理为最近邻取样（GPU 为线性），细微锯齿差异可接受。
- macOS/Windows 的 C ABI 原始句柄翻译尚未接通（rlib 路径不受影响——
  Rust 后端自持 winit 窗口）；随 Go 桌面端 / Windows 打包接入时补齐。
- 图片纹理按源指针缓存跨帧常驻（image.rs 按 src 缓存，规模受应用图片集
  上界约束）；视频/ canvas 按帧序淘汰。
