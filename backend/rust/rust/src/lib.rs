//! GX 的 Rust 渲染后端 —— C ABI 入口。
//!
//! Go 侧通过 cgo 调用：
//!   - gx_window_new(w, h, title)        创建窗口句柄（同时创建事件循环并注入 proxy）
//!   - gx_window_draw(handle, buf, len)  传入二进制场景帧（零拷贝解析）
//!   - gx_window_pump(handle)            非阻塞泵一轮 winit 事件（P4 目标态：Host 轮询驱动）
//!   - gx_window_close(handle)           请求关闭
//!   - gx_window_set_event_cb(cb, ctx)   注册事件回调（click/IME/backspace）
//!
//! 线程模型（P4 目标态）：事件循环在 gx_window_new 创建、由 gx_window_pump 在
//! Host 主循环 goroutine 上逐轮非阻塞泵（winit pump_events 嵌入宿主 API）；
//! 创建与泵同线程（Handle.Render → Handle.Run 串行约定保证）。不再有自驱
//! gx_window_run 阻塞循环；多窗口 = 多窗口各持一个事件循环实例，由同一 Host
//! 主循环交替泵（单一 UI Loop 不变量在 Go 侧 Host 实现）。
//!
//! 帧传输与生命周期（评审 P0）：
//!   - draw() 把帧 push 进 FIFO 队列并通过 EventLoopProxy 唤醒事件循环；事件循环
//!     每次 redraw 排空队列、按顺序 apply，杜绝单槽覆盖导致的增量丢失。
//!   - Backend 用 Arc 引用计数管理：close 只发退出请求，不释放内存；pump 观测到
//!     Exit 后清空渲染器槽；内存由 gx_window_destroy 释放，消除 use-after-free。
//!   - 重绘改为事件驱动：新帧/尺寸/交互状态/动画/视频才请求重绘，静止时停止。

mod binprotocol;
mod canvas;
mod image;
mod layout;
mod paint;
mod passthrough;
mod protocol;
mod render;
mod scene;
mod style;
mod text_effects;
mod util;
mod video;
// 平台 SurfaceProvider（阶段 5/6）：由外部宿主（NDK/JNI、iOS 工程、浏览器
// JS glue）构造与消费，本 crate 内部（除测试）不构造——抑制 dead_code。
#[allow(dead_code)]
mod android_surface;
#[allow(dead_code)]
mod ios_surface;
#[allow(dead_code)]
mod web_surface;

#[cfg(test)]
mod tests_surface;

use raw_window_handle::{HasRawDisplayHandle as _, HasRawWindowHandle as _};
use render::Renderer;
use std::collections::VecDeque;
use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_int, c_uchar, c_void};
use std::sync::{Arc, Mutex};
use winit::{
    dpi::LogicalSize,
    event::{ElementState, Ime, MouseButton, WindowEvent},
    event_loop::{EventLoop, EventLoopProxy},
    keyboard::{Key, NamedKey},
    platform::pump_events::EventLoopExtPumpEvents,
    window::Window,
};

/// winit 平台适配层（BACKEND_ARCHITECTURE_V2 §七）：把 winit Window 适配成
/// 渲染内核的平台无关 SurfaceProvider。渲染内核（render.rs）不再 import
/// winit——未来 Android NDK / iOS CAMetalLayer 宿主各写一个同款适配器，
/// Renderer 一行不改。
struct WinitSurface {
    window: Arc<Window>,
}
// #[allow(deprecated)]：SurfaceProvider 的契约就是 raw handle（wgpu
// SurfaceTargetUnsafe::RawHandle 的输入），rwh 0.6 标记 deprecated 的
// HasRawDisplayHandle/HasRawWindowHandle 正是完成此转换的官方路径。
#[allow(deprecated)]
impl crate::render::SurfaceProvider for WinitSurface {
    fn pixel_size(&self) -> (u32, u32) {
        let s = self.window.inner_size();
        (s.width, s.height)
    }
    /// 逻辑尺寸（点）：物理像素 / scale_factor——E2E 快照与逻辑布局对齐。
    fn logical_size(&self) -> (f32, f32) {
        let s = self.window.inner_size();
        let scale = self.window.scale_factor();
        (
            (s.width as f64 / scale) as f32,
            (s.height as f64 / scale) as f32,
        )
    }
    fn raw_display_handle(&self) -> raw_window_handle::RawDisplayHandle {
        self.window.raw_display_handle().expect("display handle")
    }
    fn raw_window_handle(&self) -> raw_window_handle::RawWindowHandle {
        self.window.raw_window_handle().expect("window handle")
    }
    fn request_redraw(&self) {
        self.window.request_redraw();
    }
}

/// 从任意线程投递给事件循环的用户事件（评审 P0/P1）。
/// Wake：有新帧或需要重绘；Close：请求事件循环退出。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum GxEvent {
    Wake,
    Close,
}

/// 队列积压阈值（BACKEND_ARCHITECTURE_V2 §六.2：有界队列 + FullSync 恢复）。
/// 约等于 60fps 下 2 秒的积压。超过阈值执行背压：丢弃全部未消费增量帧并置
/// needs_fullsync，Go Host 在下一帧用最新 Root 全量重建镜像，双方恢复一致。
/// 高刷 UI 的正确目标是「及时显示最新的正确状态」，而不是把每个中间画面排队
/// 播完——无限 FIFO 只会让画面越来越落后。
const MAX_PENDING_FRAMES: usize = 120;

/// 共享帧状态：FIFO 队列保证增量不丢；proxy 用于跨线程唤醒；shutdown 用于优雅退出。
struct FrameQueue {
    /// 待消费帧队列（FIFO）。draw push_back，redraw drain 全部按序 apply。
    pending: VecDeque<Vec<u8>>,
    /// 事件循环代理（run 启动后注入）。draw/close 经它唤醒事件循环。
    proxy: Option<EventLoopProxy<GxEvent>>,
    /// 已收到退出请求。
    shutdown: bool,
    /// 是否已有一个 Wake 在途，避免高频 draw 堆积大量 wake 消息。
    wake_in_flight: bool,
    /// 已成功 apply 的帧数（诊断用）。
    applied: u64,
    // ---- 队列积压 / 延迟指标（评审 P1）----
    /// 累计提交帧数。
    submitted: u64,
    /// 历史最大积压帧数。
    peak_pending: usize,
    /// 提交时队列已达到阈值的次数（过载计数）。
    overload_events: u64,
    /// 提交到消费的最大延迟（毫秒）。
    max_latency_ms: u64,
    /// 当前批次最早一帧的入队时间（用于计算提交→消费延迟）。
    first_enqueue: Option<std::time::Instant>,
    /// 过载背压后请求的 FullSync：Go 侧经 gx_window_take_fullsync 取走（读后清零），
    /// 下一帧携带全量 Root 重建镜像。
    needs_fullsync: bool,
}

impl FrameQueue {
    fn new() -> Self {
        FrameQueue {
            pending: VecDeque::new(),
            proxy: None,
            shutdown: false,
            wake_in_flight: false,
            applied: 0,
            submitted: 0,
            peak_pending: 0,
            overload_events: 0,
            max_latency_ms: 0,
            first_enqueue: None,
            needs_fullsync: false,
        }
    }

    /// 入队一帧并记录积压 / 延迟指标。过载只计数不丢帧——增量帧必须保序。
    fn enqueue(&mut self, frame: Vec<u8>) {
        if self.pending.is_empty() {
            self.first_enqueue = Some(std::time::Instant::now());
        }
        if self.pending.len() >= MAX_PENDING_FRAMES {
            self.overload_events += 1;
            // 背压：丢弃全部未消费增量帧并请求 FullSync。被丢弃帧的后效由
            // 全量帧完整覆盖，不会永久失步——这与「直接删除某个中间帧」不同：
            // 后者的后续增量依赖它，必须配 FullSync 才能安全恢复。
            self.pending.clear();
            self.first_enqueue = None;
            self.needs_fullsync = true;
        }
        self.pending.push_back(frame);
        self.submitted += 1;
        if self.pending.len() > self.peak_pending {
            self.peak_pending = self.pending.len();
        }
    }

    /// 排空全部待消费帧，并结算本批次的提交→消费延迟。
    fn drain_all(&mut self) -> Vec<Vec<u8>> {
        if let Some(t0) = self.first_enqueue.take() {
            let ms = t0.elapsed().as_secs_f64() * 1000.0;
            let ms = ms.min(u64::MAX as f64) as u64;
            if ms > self.max_latency_ms {
                self.max_latency_ms = ms;
            }
        }
        self.pending.drain(..).collect()
    }

    /// 取走过载恢复请求（读后清零）。true 表示镜像已因背压丢帧失步，
    /// Go Host 下一帧必须携带 FullSync（全量 Root）。
    fn take_fullsync(&mut self) -> bool {
        let need = self.needs_fullsync;
        self.needs_fullsync = false;
        need
    }

    /// 诊断快照：[submitted, applied, peak_pending, overload_events, max_latency_ms]。
    fn stats(&self) -> [u64; 5] {
        [
            self.submitted,
            self.applied,
            self.peak_pending as u64,
            self.overload_events,
            self.max_latency_ms,
        ]
    }

    /// 唤醒事件循环请求一次重绘。Wake 消息合并：已有在途则不再发。
    fn wake(&mut self, ev: GxEvent) {
        let need = match ev {
            GxEvent::Wake => !self.wake_in_flight,
            GxEvent::Close => true, // Close 必须每次都发，确保能退出
        };
        if !need {
            return;
        }
        if let Some(p) = &self.proxy {
            if p.send_event(ev).is_ok() && ev == GxEvent::Wake {
                self.wake_in_flight = true;
            }
        }
    }
}

pub type EventCallback = Option<extern "C" fn(*const c_char, *mut c_void)>;

/// 窗口外观配置（经 C ABI 由 Go 侧 WindowConfig 翻译而来）。
/// decorated 用 i32 三态：-1=未设置(用默认 true)，0=无边框，1=有边框，
/// 以兼容"未设置"与"明确 false"的区分（Go 侧 Decorated *bool）。
#[derive(Clone, Copy)]
struct WindowStyle {
    decorated: i32,
    transparent: bool,
    always_on_top: bool,
    opacity: f32,
}

impl Default for WindowStyle {
    fn default() -> Self {
        WindowStyle {
            decorated: -1,
            transparent: false,
            always_on_top: false,
            opacity: 0.0,
        }
    }
}

struct Backend {
    /// 共享帧队列（FIFO）+ 事件循环唤醒 + 退出标志
    queue: Arc<Mutex<FrameQueue>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
    width: u32,
    height: u32,
    title: String,
    style: WindowStyle,
    /// 渲染器共享句柄：首次 pump 的 resumed 创建后填充；debug 导出 /
    /// 帧时钟等 C ABI 在泵运行期间经它访问（pump Exit 后置 None）。
    renderer: Arc<Mutex<Option<Renderer>>>,
    /// VSync/present 模式请求（0=Fifo 1=Mailbox 2=Immediate；P4.5 VSyncControl）。
    /// gx_window_set_vsync 写入：渲染器已创建则立即 reconfigure，否则首窗 resumed 应用。
    vsync: Arc<std::sync::atomic::AtomicU8>,
    /// 鼠标穿透请求（P4 遗留补齐 MousePassthrough）：gx_window_set_mouse_passthrough
    /// 写入；窗口未创建时先缓存，resumed 创建窗口后立即应用，pump Exit 清窗口槽。
    passthrough: Arc<std::sync::atomic::AtomicBool>,
    /// winit 窗口共享槽：resumed 创建后填充；pump Exit 清空（与 renderer 槽同契约）。
    window: Arc<Mutex<Option<Arc<Window>>>>,
    /// 事件循环实例（gx_window_new 创建；pump 返回 Exit 后丢弃）。
    el: Mutex<Option<EventLoop<GxEvent>>>,
    /// 跨 pump 持久的 winit 应用状态（窗口句柄 / 修饰键 / 首帧探针等）。
    app: Mutex<Option<WinitApp>>,
}

unsafe impl Send for Backend {}
unsafe impl Sync for Backend {}

#[no_mangle]
pub extern "C" fn gx_window_new(width: c_int, height: c_int, title: *const c_char) -> *mut c_void {
    gx_window_new_ex(width, height, title, -1, 0, 0, 0.0)
}

unsafe fn cstr(s: *const c_char) -> String {
    if s.is_null() {
        return String::new();
    }
    std::ffi::CStr::from_ptr(s).to_string_lossy().into_owned()
}

/// 扩展版窗口创建：携带无边框/透明/置顶/不透明度配置。
/// 各布尔参数为 0/1；decorated 为 -1(默认)/0/1；opacity<=0 或 >1 表示不设置。
#[no_mangle]
pub extern "C" fn gx_window_new_ex(
    width: c_int,
    height: c_int,
    title: *const c_char,
    decorated: c_int,
    transparent: c_int,
    always_on_top: c_int,
    opacity: f32,
) -> *mut c_void {
    let title_str = unsafe { cstr(title) };
    // 事件循环在创建时建立（P4 目标态：gx_window_new 创建、gx_window_pump 泵），
    // proxy 注入提前到创建期——draw/close 从第一帧起即可跨线程唤醒。
    let el = new_event_loop();
    let proxy = el.create_proxy();
    let backend = Backend {
        renderer: Arc::new(Mutex::new(None)),
        vsync: Arc::new(std::sync::atomic::AtomicU8::new(0)),
        passthrough: Arc::new(std::sync::atomic::AtomicBool::new(false)),
        window: Arc::new(Mutex::new(None)),
        queue: Arc::new(Mutex::new(FrameQueue::new())),
        cb: Arc::new(Mutex::new(None)),
        cb_ctx: Arc::new(Mutex::new(std::ptr::null_mut())),
        width: width.max(1) as u32,
        height: height.max(1) as u32,
        title: title_str,
        style: WindowStyle {
            decorated,
            transparent: transparent != 0,
            always_on_top: always_on_top != 0,
            opacity,
        },
        el: Mutex::new(Some(el)),
        app: Mutex::new(None),
    };
    {
        let mut q = backend.queue.lock().unwrap();
        q.proxy = Some(proxy);
        // draw 早于首次 pump（首帧在 pump 前提交）：补一次 wake。
        if !q.pending.is_empty() {
            q.wake(GxEvent::Wake);
        }
    }
    // Arc::into_raw：handle 是一份强引用的原始指针。pump 期间借用（不改计数），
    // destroy 时 Arc::from_raw 释放。close 不释放，避免 pump 仍在使用时 UAF。
    Arc::into_raw(Arc::new(backend)) as *mut c_void
}

/// 传入二进制帧。一次 memcpy 入 FIFO 队列，并唤醒事件循环请求重绘。
/// 返回 0=ok, 1=参数错, 2=窗口已请求关闭（调用方应停止提交）。
///
/// 队列保序（评审 P0）：每帧都入队，redraw 时 drain 全部按序 apply。这样即便
/// Go Host 连续提交多个增量帧、事件循环尚未消费，也不会因单槽覆盖丢失中间帧。
#[no_mangle]
pub extern "C" fn gx_window_draw(handle: *mut c_void, buf: *const c_uchar, len: c_int) -> c_int {
    if handle.is_null() || buf.is_null() || len <= 0 {
        return 1;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    let owned = slice.to_vec(); // 唯一一次拷贝
    let backend = unsafe { &*(handle as *const Backend) };
    let mut q = backend.queue.lock().unwrap();
    if q.shutdown {
        return 2;
    }
    q.enqueue(owned);
    q.wake(GxEvent::Wake); // 合并 wake，避免高频帧堆积
    0
}

/// 帧队列诊断指标（评审 P1）：写入 out[0..5] =
/// [submitted, applied, peak_pending, overload_events, max_latency_ms]。
/// out 必须指向至少 5 个 u64 的可写内存。返回 0 表示成功。
#[no_mangle]
pub extern "C" fn gx_window_frame_stats(handle: *mut c_void, out: *mut u64) -> c_int {
    if handle.is_null() || out.is_null() {
        return 1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let q = backend.queue.lock().unwrap();
    let s = q.stats();
    unsafe {
        for (i, v) in s.iter().enumerate() {
            *out.add(i) = *v;
        }
    }
    0
}

/// 过载恢复查询（BACKEND_ARCHITECTURE_V2 §六.2）：队列过载背压丢弃未消费帧后
/// 返回 1（读后清零），Go Host 收到 1 后下一帧必须以 FullSync（全量 Root）重建
/// Rust 镜像。无过载返回 0。
#[no_mangle]
pub extern "C" fn gx_window_take_fullsync(handle: *mut c_void) -> c_int {
    if handle.is_null() {
        return -1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let mut q = backend.queue.lock().unwrap();
    if q.take_fullsync() {
        1
    } else {
        0
    }
}

#[no_mangle]
pub extern "C" fn gx_window_set_event_cb(handle: *mut c_void, cb: EventCallback, ctx: *mut c_void) {
    if handle.is_null() {
        return;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    *backend.cb.lock().unwrap() = cb;
    *backend.cb_ctx.lock().unwrap() = ctx;
}

/// 构造 winit 应用状态（跨 pump 持久；首次 pump 时创建）。
/// 启动时间探针：仅当 GX_STARTUP_PROBE 设置时开启，默认零开销。
fn winit_app_for(backend: &Backend) -> WinitApp {
    WinitApp {
        state: backend.renderer.clone(),
        width: backend.width,
        height: backend.height,
        title: backend.title.clone(),
        style: backend.style,
        queue: backend.queue.clone(),
        cb: backend.cb.clone(),
        cb_ctx: backend.cb_ctx.clone(),
        probe: std::env::var_os("GX_STARTUP_PROBE").is_some(),
        t_run_start: Some(std::time::Instant::now()),
        t_resumed_done: None,
        first_frame_done: false,
        window: None,
        modifiers: winit::keyboard::ModifiersState::empty(),
        hover_cursor: String::new(),
        vsync: backend.vsync.clone(),
        passthrough: backend.passthrough.clone(),
        win_slot: backend.window.clone(),
    }
}

/// 非阻塞泵一轮 winit 事件（P4 目标态：Host 主循环逐轮驱动，ui-loop-p4 文档 §5.2）。
///
/// 返回 1=继续（正常，宿主下一轮再泵）；0=事件循环已退出（用户点了关闭按钮 /
/// gx_window_close 请求退出）——宿主应停止泵并走关闭流程；2=参数错/内部错误。
///
/// 实现用 winit 的 pump_events 嵌入 API（timeout=Some(ZERO)，绝不阻塞宿主循环）；
/// 应用状态跨 pump 持久化在 Backend.app（窗口句柄/修饰键等）。pump 返回 Exit 时
/// 清空渲染器槽（surface/设备与事件循环同生命周期）并丢弃事件循环与应用状态。
#[no_mangle]
pub extern "C" fn gx_window_pump(handle: *mut c_void) -> c_int {
    if handle.is_null() {
        return 2;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let mut el_slot = backend.el.lock().unwrap();
    let Some(mut el) = el_slot.take() else {
        return 1; // 已退出（重复 pump）：宿主按 ShouldClose 处理
    };
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let mut app_slot = backend.app.lock().unwrap();
        let mut app = app_slot.take().unwrap_or_else(|| winit_app_for(backend));
        let status = el.pump_app_events(Some(std::time::Duration::ZERO), &mut app);
        match status {
            winit::platform::pump_events::PumpStatus::Continue => {
                *app_slot = Some(app);
                1
            }
            winit::platform::pump_events::PumpStatus::Exit(_) => {
                // pump 退出即清空渲染器槽：Renderer 的 surface/设备与事件循环同
                // 生命周期，事件循环外不得再经 debug/时钟 ABI 访问（评审 P0 契约）。
                *app.state.lock().unwrap() = None;
                // 窗口槽同契约清空：穿透等 C ABI 在泵外不得再访问 winit 窗口。
                *app.win_slot.lock().unwrap() = None;
                0
            }
        }
    }));
    match result {
        Ok(code) => {
            if code == 1 {
                *el_slot = Some(el);
            }
            code
        }
        Err(_) => 2, // pump panic：宿主归因熔断该窗口
    }
}

/// 构造事件循环。Linux 下事件循环跑在 Go goroutine 经 cgo 进入的线程（非主线程），
/// 必须显式开启 any_thread；X11/Wayland 两条后端都要开，避免运行环境选 Wayland 时踩坑。
fn new_event_loop() -> EventLoop<GxEvent> {
    #[cfg(target_os = "linux")]
    {
        use winit::event_loop::EventLoopBuilder;
        use winit::platform::wayland::EventLoopBuilderExtWayland;
        use winit::platform::x11::EventLoopBuilderExtX11;
        let mut builder: EventLoopBuilder<GxEvent> = EventLoop::<GxEvent>::with_user_event();
        // 两条后端都开，完全限定调用避免 with_any_thread 歧义。
        EventLoopBuilderExtX11::with_any_thread(&mut builder, true);
        EventLoopBuilderExtWayland::with_any_thread(&mut builder, true);
        return builder.build().expect("event loop");
    }
    #[cfg(not(target_os = "linux"))]
    {
        return EventLoop::<GxEvent>::with_user_event()
            .build()
            .expect("event loop");
    }
}

struct WinitApp {
    /// 共享渲染器槽：pump 期间经 Mutex 访问；Backend 的 debug/时钟 ABI
    /// 在泵运行期间经同一 Arc 读取（评审 P0 生命周期：pump Exit 后随槽释放）。
    state: Arc<Mutex<Option<Renderer>>>,
    width: u32,
    height: u32,
    title: String,
    style: WindowStyle,
    queue: Arc<Mutex<FrameQueue>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
    // 启动探针状态（GX_STARTUP_PROBE 门控，默认 false/None）
    probe: bool,
    t_run_start: Option<std::time::Instant>,
    t_resumed_done: Option<std::time::Instant>,
    first_frame_done: bool,
    // 阶段 4 输入面：window 句柄（hover 消费 CSS cursor 切系统光标）与修饰键状态
    // （winit 在 KeyboardInput 前派发 ModifiersChanged，缓存供 key/pointer 事件携带）。
    window: Option<Arc<Window>>,
    modifiers: winit::keyboard::ModifiersState,
    hover_cursor: String,
    /// VSync 请求模式（P4.5 VSyncControl）：resumed 创建渲染器后据此应用。
    vsync: Arc<std::sync::atomic::AtomicU8>,
    /// 鼠标穿透请求（MousePassthrough）：resumed 创建窗口后应用；Exit 清窗口槽。
    passthrough: Arc<std::sync::atomic::AtomicBool>,
    /// Backend 的窗口共享槽（Arc 克隆）：resumed 填充、Exit 清空，供 C ABI 访问。
    win_slot: Arc<Mutex<Option<Arc<Window>>>>,
}

impl winit::application::ApplicationHandler<GxEvent> for WinitApp {
    fn resumed(&mut self, el: &winit::event_loop::ActiveEventLoop) {
        if self.state.lock().unwrap().is_some() {
            return;
        }
        let title = if self.title.is_empty() {
            "GX Rust Backend".to_string()
        } else {
            self.title.clone()
        };
        let mut attrs = Window::default_attributes()
            .with_inner_size(LogicalSize::new(self.width as f64, self.height as f64))
            .with_title(title)
            .with_resizable(false);
        // 外观：Decorated=nil(-1) 用默认；0 无边框；1 有边框。
        if self.style.decorated >= 0 {
            attrs = attrs.with_decorations(self.style.decorated != 0);
        }
        if self.style.transparent {
            attrs = attrs.with_transparent(true);
        }
        if self.style.always_on_top {
            attrs = attrs.with_window_level(winit::window::WindowLevel::AlwaysOnTop);
        }
        let window = Arc::new(el.create_window(attrs).expect("create window"));
        window.set_ime_allowed(true);
        self.window = Some(window.clone());
        // opacity: winit 0.30 无跨平台窗口级 opacity API（各平台扩展不一），
        // 透明效果请用 with_transparent + 场景节点的半透明 bg；此处不设置。
        let _ = self.style.opacity;
        let provider: Arc<dyn crate::render::SurfaceProvider> = Arc::new(WinitSurface {
            window: window.clone(),
        });
        *self.state.lock().unwrap() = Some(pollster::block_on(Renderer::new(provider)));
        // VSyncControl：创建期把请求的 present 模式应用到渲染器（gx_window_set_vsync
        // 在首窗前调用的场景在这补上）。不支持的模式保持 Fifo 并记日志。
        {
            let mut slot = self.state.lock().unwrap();
            if let Some(r) = slot.as_mut() {
                let m = vsync_mode(self.vsync.load(std::sync::atomic::Ordering::Relaxed));
                if !r.set_present_mode(m) {
                    eprintln!("[rust-backend] vsync mode {:?} unsupported, keep {:?}", m, r.present_mode());
                }
            }
        }
        // MousePassthrough：创建期请求在这应用（gx_window_set_mouse_passthrough
        // 先于首窗 pump 调用的场景）。失败只记日志，不阻断窗口创建。
        if self.passthrough.load(std::sync::atomic::Ordering::Relaxed) {
            if let Err(e) = crate::passthrough::set_mouse_passthrough(&window, true) {
                eprintln!("[rust-backend] mouse passthrough: {e}");
            }
        }
        // 窗口槽：debug/穿透等 C ABI 在泵运行期间经它访问窗口句柄。
        *self.win_slot.lock().unwrap() = Some(window.clone());
        // 首帧主动请求一次重绘：绘制首帧/应用 run 前已提交的帧。之后靠事件驱动。
        self.state
            .lock()
            .unwrap()
            .as_ref()
            .unwrap()
            .request_redraw();
        if self.probe {
            self.t_resumed_done = Some(std::time::Instant::now());
        }
    }

    /// 跨线程 wake：draw/close 经 EventLoopProxy 投递。
    fn user_event(&mut self, el: &winit::event_loop::ActiveEventLoop, event: GxEvent) {
        match event {
            GxEvent::Close => el.exit(),
            GxEvent::Wake => {
                // 清掉 wake_in_flight，允许后续 draw 再次投递（合并语义）。
                {
                    let mut q = self.queue.lock().unwrap();
                    q.wake_in_flight = false;
                }
                if let Some(renderer) = self.state.lock().unwrap().as_mut() {
                    renderer.request_redraw();
                }
            }
        }
    }

    fn window_event(
        &mut self,
        el: &winit::event_loop::ActiveEventLoop,
        _id: winit::window::WindowId,
        event: WindowEvent,
    ) {
        let mut renderer_slot = self.state.lock().unwrap();
        let Some(renderer) = renderer_slot.as_mut() else {
            return;
        };
        match event {
            WindowEvent::Resized(size) => {
                renderer.resize(size.width.max(1), size.height.max(1));
                renderer.request_redraw();
            }
            WindowEvent::RedrawRequested => {
                // 排空 FIFO 队列，按顺序 apply 每一帧（评审 P0：增量不丢）。
                // 若队列积压很多（事件循环落后于提交速率），逐帧 apply 会把镜像追到最新；
                // 每帧都成功时 applied 递增，任一增量因无基线失败会由 Go 侧全量重发兜底。
                let batch: Vec<Vec<u8>> = {
                    let mut q = self.queue.lock().unwrap();
                    q.drain_all()
                };
                for buf in &batch {
                    if renderer.apply_frame(buf) {
                        self.queue.lock().unwrap().applied += 1;
                    }
                }
                let t_pre_render = if self.probe {
                    Some(std::time::Instant::now())
                } else {
                    None
                };
                renderer.render();
                // 回流实测尺寸：把本帧布局结果经事件回调送回 Host，供虚拟列表按
                // node_id 订阅并修正 offsets/锚定。text 承载紧凑 JSON 数组。
                let measured = renderer.take_measured();
                if !measured.is_empty() {
                    // JSON 边界保持 [["<nid十进制>", w, h], ...]——Go publishMeasuredWire
                    // 按 r[0].(string) 解析后 gxscene.ParseNodeID 还原，宿主零改动。
                    let rows: Vec<Vec<serde_json::Value>> = measured
                        .iter()
                        .map(|(nid, w, h)| {
                            vec![
                                serde_json::Value::String(nid.to_string()),
                                serde_json::json!(w),
                                serde_json::json!(h),
                            ]
                        })
                        .collect();
                    let ev = protocol::InputEvent {
                        kind: "measured".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text: serde_json::to_string(&rows).unwrap_or_default(),
                        x: 0.0,
                        y: 0.0,
                        mods: String::new(),
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                // video 事件（state/meta/time）：解码线程产出，经事件回调回传 Host。
                for ve in video::take_events() {
                    let ev = protocol::InputEvent {
                        kind: ve.kind,
                        node_id: ve.node_id,
                        event: String::new(),
                        text: ve.text,
                        x: 0.0,
                        y: 0.0,
                        mods: String::new(),
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                if self.probe && !self.first_frame_done {
                    self.first_frame_done = true;
                    let t_first = std::time::Instant::now();
                    let ms = |a: std::time::Instant, b: std::time::Instant| -> f64 {
                        b.duration_since(a).as_secs_f64() * 1000.0
                    };
                    let run_start = self.t_run_start.unwrap_or(t_first);
                    let resumed = self.t_resumed_done.unwrap_or(t_first);
                    let new_ms = ms(run_start, resumed); // event loop + wgpu 设备 + 字体系统初始化
                    let frame_ms = t_pre_render.map(|t| ms(t, t_first)).unwrap_or(0.0); // 首帧布局+光栅化
                    let text = format!("init_ms={:.3};first_frame_ms={:.3}", new_ms, frame_ms);
                    let ev = protocol::InputEvent {
                        kind: "startup_first_frame".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text,
                        x: 0.0,
                        y: 0.0,
                        mods: String::new(),
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                // 按需重绘（评审 P1）：只有仍有动画/视频/滚动条过渡时才请求下一帧；
                // 静止场景停止 request_redraw，避免空转占用 CPU/GPU。新帧到达时由
                // gx_window_draw 经 proxy 发 Wake 重新驱动一次重绘。
                if renderer.needs_continuous_redraw() {
                    renderer.request_redraw();
                }
            }
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::ModifiersChanged(m) => {
                // winit 保证在 KeyboardInput 前派发；缓存供 key/pointer 事件携带。
                self.modifiers = m.state();
            }
            WindowEvent::CursorMoved { position, .. } => {
                let (x, y) = (position.x as f32, position.y as f32);
                renderer.set_cursor(x, y);
                // 原始指针移动事件（翻译层消费：分隔条拖拽 / 左键按住扩展选区）。
                let mods = mods_str(self.modifiers);
                fire(
                    &self.cb,
                    &self.cb_ctx,
                    &input_json("pointer_move", "", "", "", x, y, &mods),
                );
                // 伪状态 hover：命中节点变化时才 request_redraw（边沿检测，避免每帧重绘）。
                let hit = renderer.hit_node_id(x, y);
                // hover 消费 CSS cursor 属性切系统光标（边沿：形状变化才 set）。
                // 先取形状再交出 hit 所有权（set_hover 需要 Option<String> 值）。
                let shape = hit
                    .as_deref()
                    .and_then(|id| renderer.node_cursor(id))
                    .unwrap_or_default();
                if renderer.set_hover(hit) {
                    renderer.request_redraw();
                }
                if let Some(w) = &self.window {
                    if shape != self.hover_cursor {
                        self.hover_cursor = shape.clone();
                        w.set_cursor(cursor_icon_for(&shape));
                    }
                }
            }
            WindowEvent::MouseWheel { delta, .. } => {
                // 滚轮：统一归一为"行数"（LineDelta 原样；PixelDelta 按行高 20px 折算）。
                // winit 约定 y 正=向上滚；翻译层自行取负转"向下为正"。
                let (dx, dy) = match delta {
                    winit::event::MouseScrollDelta::LineDelta(x, y) => (x, y),
                    winit::event::MouseScrollDelta::PixelDelta(pos) => {
                        (pos.x as f32 / 20.0, pos.y as f32 / 20.0)
                    }
                };
                // 滚动路由（同 web 端浏览器职责）：光标下 overflow 容器由渲染端
                // 自治（镜像树补丁 + 重绘）；编辑器区 / 无容器 → 回传翻译层
                // （playground：EditorScrollLines 信号驱动编辑器虚拟滚动）。
                let (cx, cy) = renderer.cursor();
                let handled = renderer.scroll_container_at(cx, cy, dy * 40.0);
                if !handled {
                    fire(
                        &self.cb,
                        &self.cb_ctx,
                        &input_json("wheel", "", "", "", dx, dy, ""),
                    );
                }
            }
            WindowEvent::MouseInput {
                state: ElementState::Pressed,
                button: MouseButton::Left,
                ..
            } => {
                let (x, y) = renderer.cursor();
                let mods = mods_str(self.modifiers);
                // 原始按下事件（翻译层消费：选区起点 / 分隔条拖拽起点 / 多击计数）。
                fire(
                    &self.cb,
                    &self.cb_ctx,
                    &input_json("pointer_down", "", "left", "", x, y, &mods),
                );
                // active 伪状态：按下命中的节点进入 active（active 优先于 hover）。
                let hit = renderer.hit_node_id(x, y);
                if renderer.set_active(hit) {
                    renderer.request_redraw();
                }
                // focus 伪状态：点击输入框 → 该节点聚焦；点击其他区域 → 失焦（同浏览器）。
                // 这与 host 侧 Input 的 focusToken signal 互补：后者驱动 IME/光标，
                // 这里驱动 & 选择器的视觉样式；点击场景下两者指向同一节点。
                // 边沿检测：仅焦点变化才重绘。
                let input_ev = renderer.hit_input(x, y);
                let focus_id = input_ev.as_ref().map(|e| e.node_id.clone());
                if renderer.set_focused(focus_id) {
                    renderer.request_redraw();
                }
                if let Some(mut ev) = renderer.hit_click(x, y) {
                    ev.mods = mods;
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                } else if let Some(mut ev) = input_ev {
                    ev.mods = mods;
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            }
            WindowEvent::MouseInput {
                state: ElementState::Released,
                button: MouseButton::Left,
                ..
            } => {
                let (x, y) = renderer.cursor();
                fire(
                    &self.cb,
                    &self.cb_ctx,
                    &input_json(
                        "pointer_up",
                        "",
                        "left",
                        "",
                        x,
                        y,
                        &mods_str(self.modifiers),
                    ),
                );
                // 松开：退出 active，回到仅 hover 状态。
                if renderer.set_active(None) {
                    renderer.request_redraw();
                }
            }
            WindowEvent::CursorLeft { .. } => {
                // 光标离开窗口：退出 hover（否则会停在最后的 hover 态）+ 恢复默认箭头。
                if renderer.set_hover(None) {
                    renderer.request_redraw();
                }
                if let Some(w) = &self.window {
                    self.hover_cursor = String::new();
                    w.set_cursor(cursor_icon_for(""));
                }
            }
            WindowEvent::Ime(ime) => match ime {
                Ime::Enabled => fire(&self.cb, &self.cb_ctx, ev_json("ime_enabled")),
                Ime::Disabled => {}
                Ime::Preedit(s, _) => {
                    let ev = protocol::InputEvent {
                        kind: "ime_preedit".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text: s,
                        x: 0.0,
                        y: 0.0,
                        mods: String::new(),
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                Ime::Commit(s) => {
                    let ev = protocol::InputEvent {
                        kind: "ime_commit".into(),
                        node_id: String::new(),
                        event: String::new(),
                        text: s,
                        x: 0.0,
                        y: 0.0,
                        mods: String::new(),
                    };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            },
            WindowEvent::KeyboardInput {
                event:
                    winit::event::KeyEvent {
                        state: ElementState::Pressed,
                        logical_key,
                        repeat,
                        ..
                    },
                ..
            } => {
                // 原始按键事件（翻译层消费：方向键移动 / Home/End / Ctrl 组合快捷键）。
                // repeat 用 event 字段区分 "down"/"repeat"；Up 不发（现有消费方都只关心按下）。
                let phase = if repeat { "repeat" } else { "down" };
                let mods = mods_str(self.modifiers);
                fire(
                    &self.cb,
                    &self.cb_ctx,
                    &input_json("key", "", phase, &key_name(&logical_key), 0.0, 0.0, &mods),
                );
                if logical_key == Key::Named(NamedKey::Backspace) {
                    fire(&self.cb, &self.cb_ctx, ev_json("backspace"));
                } else if logical_key == Key::Named(NamedKey::Escape) {
                    el.exit();
                } else if let Some(text) = logical_key.to_text() {
                    // IME 未激活时（如纯 ASCII / XTEST 注入），字符走 KeyboardInput 而非 Ime::Commit。
                    // 过滤控制字符，其余按 commit 回写文本框。
                    if !text.chars().any(|c| c.is_control()) {
                        let ev = protocol::InputEvent {
                            kind: "ime_commit".into(),
                            node_id: String::new(),
                            event: String::new(),
                            text: text.to_string(),
                            x: 0.0,
                            y: 0.0,
                            mods: String::new(),
                        };
                        fire(&self.cb, &self.cb_ctx, &ev.to_json());
                    }
                }
            }
            _ => {}
        }
    }
}

/// 构造原始输入事件 JSON（mods 为空时省略字段，保持事件体最小）。
fn input_json(
    kind: &str,
    node_id: &str,
    event: &str,
    text: &str,
    x: f32,
    y: f32,
    mods: &str,
) -> String {
    let ev = protocol::InputEvent {
        kind: kind.into(),
        node_id: node_id.into(),
        event: event.into(),
        text: text.into(),
        x,
        y,
        mods: mods.into(),
    };
    ev.to_json()
}

/// winit LogicalKey → 翻译层键名：Named 键给变体名（Left/Home/Backspace/Escape...），
/// 字符键给字符本身（"a"），识别不了给空串。
fn key_name(k: &Key) -> String {
    match k {
        Key::Named(n) => format!("{n:?}"),
        Key::Character(s) => s.to_string(),
        _ => String::new(),
    }
}

/// 修饰键固定顺序串（翻译层按 contains 判断）："ctrl+shift+alt+super"。
/// 自由函数而非方法：window_event 里 renderer 可变借用 self.state 期间，
/// 方法调用会触发整 self 借用冲突；ModifiersState 是 Copy，先复制再传值。
fn mods_str(m: winit::keyboard::ModifiersState) -> String {
    let mut parts: Vec<&str> = Vec::with_capacity(4);
    if m.control_key() {
        parts.push("ctrl");
    }
    if m.shift_key() {
        parts.push("shift");
    }
    if m.alt_key() {
        parts.push("alt");
    }
    if m.super_key() {
        parts.push("super");
    }
    parts.join("+")
}

/// CSS cursor 值 → winit 系统光标（与旧 skia glwindow 的 setHoverCursor 同一张映射）。
/// "default"/"auto"/空 → Arrow；未识别值（wait/progress/move）暂按箭头。
fn cursor_icon_for(shape: &str) -> winit::window::CursorIcon {
    use winit::window::CursorIcon;
    match shape.trim().to_ascii_lowercase().as_str() {
        "pointer" | "hand" => CursorIcon::Pointer,
        "text" => CursorIcon::Text,
        "crosshair" => CursorIcon::Crosshair,
        "ew-resize" | "col-resize" => CursorIcon::EwResize,
        "ns-resize" | "row-resize" => CursorIcon::NsResize,
        _ => CursorIcon::Default,
    }
}

fn ev_json(kind: &str) -> &'static str {
    // 仅用于无文本事件；有文本走 InputEvent::to_json
    match kind {
        "ime_enabled" => r#"{"kind":"ime_enabled","node_id":"","event":"","text":"","x":0,"y":0}"#,
        _ => r#"{"kind":"backspace","node_id":"","event":"","text":"","x":0,"y":0}"#,
    }
}

fn fire(cb: &Arc<Mutex<EventCallback>>, ctx: &Arc<Mutex<*mut c_void>>, json: &str) {
    if let Some(f) = *cb.lock().unwrap() {
        let c = *ctx.lock().unwrap();
        if let Ok(s) = CString::new(json) {
            f(s.as_ptr(), c);
        }
    }
}

/// 请求关闭：设置 shutdown 并通过 proxy 唤醒事件循环退出。
/// 注意：这里【不释放】Backend 内存——pump 可能仍在使用它（评审 P0 生命周期竞争）。
/// 宿主观测到 gx_window_pump 返回 0 后调用 gx_window_destroy 释放。
#[no_mangle]
pub extern "C" fn gx_window_close(handle: *mut c_void) {
    if handle.is_null() {
        return;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    {
        let mut q = backend.queue.lock().unwrap();
        q.shutdown = true;
        q.wake(GxEvent::Close);
    }
}

/// 真正释放 Backend（Arc::from_raw 归还调用方持有的那份强引用）。
/// Go 侧必须在 gx_window_pump 返回 0（事件循环已退出）之后调用。
/// 释放前兜底清空渲染器槽 / winit 应用状态 / 事件循环，保证 GPU 资源先于
/// Backend 析构（pump Exit 路径已清，这里覆盖「未泵即 destroy」的短生命周期）。
#[no_mangle]
pub extern "C" fn gx_window_destroy(handle: *mut c_void) {
    if !handle.is_null() {
        unsafe {
            let backend = Arc::from_raw(handle as *const Backend);
            if let Ok(mut slot) = backend.app.lock() {
                *slot = None; // 先 drop WinitApp（持窗口 Arc）
            }
            if let Ok(mut slot) = backend.el.lock() {
                *slot = None; // 再 drop 事件循环（surface 随之失效）
            }
            if let Ok(mut slot) = backend.renderer.lock() {
                *slot = None;
            }
            drop(backend);
        }
    }
}

/// VSync 模式码 ↔ wgpu PresentMode（P4.5 VSyncControl）：
/// 0=Fifo（默认，帧节奏对齐屏幕刷新率——真高刷路径）
/// 1=Mailbox（三缓冲替换，低延迟不撕裂；X11 多数驱动不支持）
/// 2=Immediate（关 vsync 立即上屏，可能撕裂——240fps 压测/极限低延迟专用）。
fn vsync_mode(code: u8) -> wgpu::PresentMode {
    match code {
        1 => wgpu::PresentMode::Mailbox,
        2 => wgpu::PresentMode::Immediate,
        _ => wgpu::PresentMode::Fifo,
    }
}

/// 运行时切换 vsync/present 模式。渲染器已创建则立即 reconfigure 生效；
/// 未创建（首窗前）仅记录请求，由 resumed 应用。
/// 返回 0=成功；1=surface 不支持该模式（保持原值，请求仍记录）；2=参数/句柄无效。
#[no_mangle]
pub extern "C" fn gx_window_set_vsync(handle: *mut c_void, mode: c_int) -> c_int {
    if handle.is_null() || !(0..=2).contains(&mode) {
        return 2;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    backend
        .vsync
        .store(mode as u8, std::sync::atomic::Ordering::Relaxed);
    let mut slot = backend.renderer.lock().unwrap();
    match slot.as_mut() {
        Some(r) => {
            if r.set_present_mode(vsync_mode(mode as u8)) {
                0
            } else {
                1
            }
        }
        None => 0,
    }
}

/// 读取当前请求的 vsync 模式码（0/1/2；句柄无效返回 -1）。诊断用。
#[no_mangle]
pub extern "C" fn gx_window_get_vsync(handle: *mut c_void) -> c_int {
    if handle.is_null() {
        return -1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    backend.vsync.load(std::sync::atomic::Ordering::Relaxed) as c_int
}

/// 鼠标穿透开关（MousePassthrough，整窗级）。enabled!=0 = 整窗穿透（点击落到
/// 下层窗口）；0 = 恢复正常接收。
///
/// 返回 0=成功；1=空句柄；2=平台不支持/系统库缺失（错误经 eprintln 记录）。
/// 窗口未创建（首 pump 前）：只缓存请求，resumed 创建窗口后自动应用——这与
/// gx_window_set_vsync 的创建期语义一致，Go 侧可在 openEntry 装配段直接调用。
#[no_mangle]
pub extern "C" fn gx_window_set_mouse_passthrough(handle: *mut c_void, enabled: c_int) -> c_int {
    if handle.is_null() {
        return 1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    backend
        .passthrough
        .store(enabled != 0, std::sync::atomic::Ordering::Relaxed);
    // 已有窗口：立即应用；没有：等 resumed 应用。
    let win = backend.window.lock().unwrap().clone();
    if let Some(w) = win {
        if let Err(e) = crate::passthrough::set_mouse_passthrough(&w, enabled != 0) {
            eprintln!("[rust-backend] mouse passthrough: {e}");
            return 2;
        }
    }
    0
}

/// E2E 调试导出：{"win_w":..,"win_h":..,"boxes":[{label,testid,kind,x,y,w,h},...]}。
/// 返回堆上 C 字符串，调用方用 gx_window_debug_free 释放；空场景返回 "boxes":[]。
#[no_mangle]
pub extern "C" fn gx_window_debug_info(handle: *mut c_void) -> *mut c_char {
    if handle.is_null() {
        return CString::default().into_raw();
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let json = match backend.renderer.lock() {
        Ok(mut slot) => match slot.as_ref() {
            Some(r) => r.debug_boxes_json(),
            None => r#"{"win_w":0,"win_h":0,"boxes":[]}"#.into(),
        },
        Err(_) => r#"{"win_w":0,"win_h":0,"boxes":[]}"#.into(),
    };
    match CString::new(json) {
        Ok(c) => c.into_raw(),
        Err(_) => CString::default().into_raw(),
    }
}

/// 释放 gx_window_debug_info 返回的堆串。
#[no_mangle]
pub unsafe extern "C" fn gx_window_debug_free(ptr: *mut c_char) {
    if !ptr.is_null() {
        drop(CString::from_raw(ptr));
    }
}

// ---- 剪贴板 C ABI（阶段 4 输入面：翻译层 Ctrl+C/V/X 消费）----
//
// arboard 进程级实例惰性创建（首次读写才建，开销可忽略）；feature 关闭时
// （wasm 构建用 --no-default-features）get 返回空串 / set no-op。

/// 进程级剪贴板实例。初始化失败（无显示服务 / Wayland 无 data-control）存 None，
/// 读写按"失败"处理而非 panic——剪贴板不可用不应拖垮渲染进程。
#[cfg(feature = "clipboard")]
fn clipboard() -> Option<&'static Mutex<arboard::Clipboard>> {
    static CLIP: std::sync::OnceLock<Option<Mutex<arboard::Clipboard>>> =
        std::sync::OnceLock::new();
    CLIP.get_or_init(|| arboard::Clipboard::new().ok().map(Mutex::new))
        .as_ref()
}

/// 读取剪贴板文本。返回堆上 C 字符串（NUL 结尾，UTF-8；失败/空返回 ""），
/// 调用方必须用 gx_clipboard_free 释放（非借用，避免栈帧生命周期纠缠）。
#[no_mangle]
pub extern "C" fn gx_clipboard_get() -> *mut c_char {
    let text: String = {
        #[cfg(feature = "clipboard")]
        {
            clipboard()
                .and_then(|c| c.lock().ok())
                .and_then(|mut c| c.get_text().ok())
                .unwrap_or_default()
        }
        #[cfg(not(feature = "clipboard"))]
        String::new()
    };
    match CString::new(text) {
        Ok(c) => c.into_raw(),
        Err(_) => CString::default().into_raw(),
    }
}

/// 释放 gx_clipboard_get 返回的堆串。
#[no_mangle]
pub unsafe extern "C" fn gx_clipboard_free(ptr: *mut c_char) {
    if !ptr.is_null() {
        drop(CString::from_raw(ptr));
    }
}

/// 写剪贴板文本（UTF-8，NUL 结尾）。返回 0 成功 / -1 失败（feature 关闭恒 -1）。
#[no_mangle]
pub extern "C" fn gx_clipboard_set(text: *const c_char) -> i32 {
    if text.is_null() {
        return -1;
    }
    let s = unsafe { CStr::from_ptr(text) };
    let text: &str = match s.to_str() {
        Ok(t) => t,
        Err(_) => return -1,
    };
    #[cfg(feature = "clipboard")]
    {
        match clipboard()
            .and_then(|c| c.lock().ok())
            .map(|mut c| c.set_text(text.to_string()))
        {
            Some(Ok(())) => 0,
            _ => -1,
        }
    }
    #[cfg(not(feature = "clipboard"))]
    {
        let _ = text;
        -1
    }
}

#[no_mangle]
pub extern "C" fn gx_window_version() -> *const c_char {
    b"gxbackend-rust 0.3.0 (GRX3 binary protocol)\0".as_ptr() as *const c_char
}

/// 编辑器字符 advance 实测（Go 侧 MeasureEditorAdvances 的底层）。
/// family 是 CSS font-family 栈文本（如 "Consolas, monospace"），size 是字号 px。
/// 结果写入 narrow/wide 两个出参：ASCII 样本与 CJK 样本的平均字符 advance。
/// 返回 0=成功；出参为空返回 1。与 skia 后端 MeasureEditorAdvances 语义对齐，
/// 供桌面装配层启动时注入 ui.SetCharWidth/SetWideWidth（点击换算与光标渲染依赖）。
#[no_mangle]
pub extern "C" fn gx_measure_text_advances(
    family: *const c_char,
    size: c_int,
    narrow: *mut f32,
    wide: *mut f32,
) -> c_int {
    if narrow.is_null() || wide.is_null() {
        return 1;
    }
    let stack = unsafe { cstr(family) };
    let (n, w) = crate::util::measure_advances(&stack, size.max(1) as f32);
    unsafe {
        *narrow = n;
        *wide = w;
    }
    0
}

/// 纯 CPU 管线基准：buf 是 GRX1 帧，重复 iters 次「解析→建树→Taffy 布局→指令生成」。
/// 返回累计纳秒；解析失败返回 0。不含任何 GPU/窗口工作，供 cross-backend benchmark 调用。
#[no_mangle]
pub extern "C" fn gx_bench_frame(
    buf: *const c_uchar,
    len: c_int,
    width: f32,
    height: f32,
    iters: c_int,
) -> u64 {
    if buf.is_null() || len <= 0 || iters <= 0 {
        return 0;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    render::bench_frame(slice, width, height, iters as u32).unwrap_or(0)
}

/// 缓存命中路径基准：同一棵树重复提交（签名不变 → 布局复用，只跑 emit）。
/// 与 gx_bench_frame 的比值 = 纯绘制属性变化帧相对全量帧的加速比
/// （BACKEND_ARCHITECTURE_V2 阶段 3 布局缓存验收）。
#[no_mangle]
pub extern "C" fn gx_bench_frame_cached(
    buf: *const c_uchar,
    len: c_int,
    width: f32,
    height: f32,
    iters: c_int,
) -> u64 {
    if buf.is_null() || len <= 0 || iters <= 0 {
        return 0;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    render::bench_frame_cached(slice, width, height, iters as u32).unwrap_or(0)
}

// ---- 镜像一致性探针（纯 CPU，不依赖窗口/GPU）----
//
// 供 Go 侧集成测试验证「Go Scene → 二进制帧 → Rust 镜像」端到端一致性：
// 复用与 Renderer::apply_frame 完全相同的 GRX1/GRX2 消费逻辑，喂一串帧后
// 可查询镜像节点数与规范化摘要，从而对 1000 节点增删/条件切换做断言。

struct MirrorSession {
    scene: Option<crate::scene::SceneNode>,
    applied: u64,
    failed: u64,
}

static MIRROR: std::sync::OnceLock<Mutex<MirrorSession>> = std::sync::OnceLock::new();

fn mirror() -> &'static Mutex<MirrorSession> {
    MIRROR.get_or_init(|| {
        Mutex::new(MirrorSession {
            scene: None,
            applied: 0,
            failed: 0,
        })
    })
}

/// 重置镜像会话（清空基线树与计数）。
#[no_mangle]
pub extern "C" fn gx_mirror_reset() {
    let mut m = mirror().lock().unwrap();
    m.scene = None;
    m.applied = 0;
    m.failed = 0;
}

/// 喂一帧到镜像：GRX1 全量重建，GRX2 增量打补丁（与 Renderer::apply_frame 同源）。
/// 返回 0=成功应用，1=无基线时收到增量，2=无法解析。
#[no_mangle]
pub extern "C" fn gx_mirror_apply(buf: *const c_uchar, len: c_int) -> c_int {
    if buf.is_null() || len <= 0 {
        return 2;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    let mut m = mirror().lock().unwrap();
    if let Some(view) = crate::binprotocol::FrameView::parse(slice) {
        if let Some(root) = crate::scene::SceneNode::from_frame(&view) {
            m.scene = Some(root);
            m.applied += 1;
            return 0;
        }
        m.failed += 1;
        return 2;
    }
    if let Some(delta) = crate::scene::DeltaView::parse(slice) {
        if let Some(root) = m.scene.as_mut() {
            delta.apply(root);
            m.applied += 1;
            return 0;
        }
        // 无基线收到增量：与真实 Renderer 一致，拒绝（应由 FullSync 全量恢复）。
        m.failed += 1;
        return 1;
    }
    m.failed += 1;
    2
}

/// 返回镜像节点总数（含 root）。无镜像返回 0。
#[no_mangle]
pub extern "C" fn gx_mirror_node_count() -> c_int {
    fn count(n: &crate::scene::SceneNode) -> i32 {
        1 + n.children.iter().map(count).sum::<i32>()
    }
    let m = mirror().lock().unwrap();
    m.scene.as_ref().map(count).unwrap_or(0)
}

/// 把镜像扁平化为 "id:text\n..." 写入调用方缓冲，返回写入字节数（不含结尾 0）。
/// 缓冲不足时返回 -1。供 Go 侧做内容级一致性断言。
#[no_mangle]
pub extern "C" fn gx_mirror_dump(out: *mut c_char, cap: c_int) -> c_int {
    if out.is_null() || cap <= 0 {
        return -1;
    }
    let m = mirror().lock().unwrap();
    let mut s = String::new();
    fn walk(n: &crate::scene::SceneNode, s: &mut String) {
        // 身份摘要：id 字符串优先（GRX1/测试），GRX3 回退 nid 十进制。
        if !n.id.is_empty() {
            s.push_str(&n.id);
        } else if n.nid != 0 {
            s.push_str(&n.nid.to_string());
        }
        s.push(':');
        s.push_str(&n.text);
        s.push('\n');
        for c in &n.children {
            walk(c, s);
        }
    }
    if let Some(root) = m.scene.as_ref() {
        walk(root, &mut s);
    }
    let bytes = s.as_bytes();
    if bytes.len() + 1 > cap as usize {
        return -1;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(bytes.as_ptr(), out as *mut u8, bytes.len());
        *out.add(bytes.len()) = 0;
    }
    bytes.len() as c_int
}

/// 返回成功/失败应用次数（编码为 applied<<32 | failed），供测试断言恢复路径。
#[no_mangle]
pub extern "C" fn gx_mirror_counts() -> u64 {
    let m = mirror().lock().unwrap();
    ((m.applied as u64) << 32) | (m.failed & 0xFFFF_FFFF)
}

#[cfg(test)]
mod frame_queue_tests {
    use super::*;

    #[test]
    fn queue_backpressure_drops_stale_and_requests_fullsync() {
        // 背压语义（BACKEND_ARCHITECTURE_V2 §六.2）：pending 达阈值后有界回压——
        // 丢弃全部未消费增量帧 + needs_fullsync，Go Host 下一帧全量重建镜像。
        // 高刷目标 = 及时显示最新的正确状态，不是把中间画面排队播完。
        let mut q = FrameQueue::new();
        let total = MAX_PENDING_FRAMES + 7;
        for i in 0..total {
            q.enqueue(vec![i as u8]);
        }
        assert_eq!(q.submitted, total as u64);
        // 过载触发一次背压：丢弃 120 帧积压，之后 121..127 逐帧入队 → 7 帧。
        assert_eq!(q.pending.len(), 7);
        assert_eq!(q.peak_pending, MAX_PENDING_FRAMES);
        assert_eq!(q.overload_events, 1);
        // FullSync 请求置位且读后清零。
        assert!(q.take_fullsync());
        assert!(!q.take_fullsync(), "FullSync 请求应读后清零");

        let batch = q.drain_all();
        assert_eq!(batch.len(), 7);
        assert!(q.pending.is_empty());
        assert!(q.first_enqueue.is_none());
        assert!(q.stats()[3] >= 1, "过载事件应上报");
    }

    #[test]
    fn queue_backpressure_repeats_on_sustained_overload() {
        // 持续过载（Go 侧迟迟不消费）：每次越过阈值都再次背压，不无限堆积。
        let mut q = FrameQueue::new();
        for _ in 0..(MAX_PENDING_FRAMES * 2 + 5) {
            q.enqueue(vec![0]);
        }
        assert_eq!(q.overload_events, 2);
        assert!(q.pending.len() <= MAX_PENDING_FRAMES);
        assert!(q.take_fullsync());
    }

    #[test]
    fn queue_stats_reset_latency_on_new_batch() {
        let mut q = FrameQueue::new();
        q.enqueue(vec![1, 2, 3]);
        let _ = q.drain_all();
        q.enqueue(vec![4, 5, 6]);
        assert!(q.first_enqueue.is_some());
        let _ = q.drain_all();
        assert!(q.first_enqueue.is_none());
        assert_eq!(q.submitted, 2);
        assert_eq!(q.applied, 0);
    }
}

#[cfg(test)]
mod input_tests {
    use super::*;

    /// CSS cursor → winit 光标映射：与旧 skia glwindow setHoverCursor 同一张表。
    #[test]
    fn cursor_icon_mapping() {
        use winit::window::CursorIcon;
        assert_eq!(cursor_icon_for("pointer"), CursorIcon::Pointer);
        assert_eq!(cursor_icon_for("hand"), CursorIcon::Pointer); // 别名
        assert_eq!(cursor_icon_for("text"), CursorIcon::Text);
        assert_eq!(cursor_icon_for("crosshair"), CursorIcon::Crosshair);
        assert_eq!(cursor_icon_for("ew-resize"), CursorIcon::EwResize);
        assert_eq!(cursor_icon_for("col-resize"), CursorIcon::EwResize); // 别名
        assert_eq!(cursor_icon_for("ns-resize"), CursorIcon::NsResize);
        assert_eq!(cursor_icon_for("row-resize"), CursorIcon::NsResize); // 别名
                                                                         // default/auto/空/未识别 → Default 箭头
        assert_eq!(cursor_icon_for(""), CursorIcon::Default);
        assert_eq!(cursor_icon_for("default"), CursorIcon::Default);
        assert_eq!(cursor_icon_for("auto"), CursorIcon::Default);
        assert_eq!(cursor_icon_for("wait"), CursorIcon::Default);
        assert_eq!(cursor_icon_for("  Text  "), CursorIcon::Text); // 大小写+空白归一
    }

    /// 修饰键固定顺序串：翻译层按 contains 判断，顺序稳定可读。
    #[test]
    fn mods_string_order() {
        use winit::keyboard::ModifiersState;
        assert_eq!(mods_str(ModifiersState::empty()), "");
        assert_eq!(mods_str(ModifiersState::CONTROL), "ctrl");
        assert_eq!(mods_str(ModifiersState::SHIFT), "shift");
        assert_eq!(
            mods_str(ModifiersState::CONTROL | ModifiersState::SHIFT),
            "ctrl+shift"
        );
        assert_eq!(mods_str(ModifiersState::all()), "ctrl+shift+alt+super");
    }

    /// 键名提取：Named 键给变体名，字符键给字符，未识别空串。
    #[test]
    fn key_name_extraction() {
        use winit::keyboard::{Key, NamedKey};
        assert_eq!(key_name(&Key::Named(NamedKey::ArrowLeft)), "ArrowLeft");
        assert_eq!(key_name(&Key::Named(NamedKey::Home)), "Home");
        assert_eq!(key_name(&Key::Named(NamedKey::Backspace)), "Backspace");
        assert_eq!(key_name(&Key::Character("a".into())), "a");
        assert_eq!(key_name(&Key::Character("中".into())), "中");
    }

    /// 原始输入事件 JSON：mods 为空省略语义（serde default），键序稳定可解析。
    #[test]
    fn input_json_shape() {
        let j = input_json("wheel", "", "", "", 1.0, -3.0, "");
        let v: serde_json::Value = serde_json::from_str(&j).unwrap();
        assert_eq!(v["kind"], "wheel");
        assert_eq!(v["x"], 1.0);
        assert_eq!(v["y"], -3.0);
        assert_eq!(v["mods"], "");
        let j2 = input_json("key", "", "down", "Left", 0.0, 0.0, "ctrl+shift");
        let v2: serde_json::Value = serde_json::from_str(&j2).unwrap();
        assert_eq!(v2["kind"], "key");
        assert_eq!(v2["event"], "down");
        assert_eq!(v2["text"], "Left");
        assert_eq!(v2["mods"], "ctrl+shift");
    }

    /// 剪贴板 C ABI（feature 默认开启）：get 返回堆串可 free；set 往返成功。
    /// 无头环境 arboard 可能因无显示服务失败——get 应返回合法串（失败时空），
    /// set 失败返回 -1 不 panic。
    #[test]
    fn clipboard_abi_roundtrip() {
        let ptr = gx_clipboard_get();
        assert!(!ptr.is_null());
        unsafe { gx_clipboard_free(ptr) };
        let text = CString::new("gx-clipboard-test").unwrap();
        let _ = gx_clipboard_set(text.as_ptr()); // 成功 0 / 无显示环境 -1，均不 panic
    }

    /// VSyncControl（P4.5）：模式码映射正确（0=Fifo 1=Mailbox 2=Immediate，
    /// 越界回退 Fifo）；gx_window_get_vsync 对空句柄返回 -1。
    #[test]
    fn vsync_mode_mapping() {
        assert!(matches!(vsync_mode(0), wgpu::PresentMode::Fifo));
        assert!(matches!(vsync_mode(1), wgpu::PresentMode::Mailbox));
        assert!(matches!(vsync_mode(2), wgpu::PresentMode::Immediate));
        assert!(matches!(vsync_mode(9), wgpu::PresentMode::Fifo));
        assert_eq!(gx_window_get_vsync(std::ptr::null_mut()), -1);
    }
}
