//! GX 的 Rust 渲染后端 —— C ABI 入口。
//!
//! Go 侧通过 cgo 调用：
//!   - gx_window_new(w, h, title)        创建窗口句柄
//!   - gx_window_draw(handle, buf, len)  传入二进制场景帧（零拷贝解析）
//!   - gx_window_run(handle)             跑 winit 事件循环（阻塞）
//!   - gx_window_close(handle)           关闭
//!   - gx_window_set_event_cb(cb, ctx)   注册事件回调（click/IME/backspace）
//!
//! 线程模型：wgpu surface 与事件循环绑定在 gx_window_run() 所在线程。
//! 该线程由 Go 侧 goroutine 经 cgo 进入，并非进程主线程，故 Linux 下显式开启
//! winit 的 any_thread（官方嵌入宿主 API）；Windows 无主线程限制。
//! draw() 只写共享 frame（Arc<Mutex>），线程安全。

mod binprotocol;
mod protocol;
mod render;
mod scene;

use render::Renderer;
use std::ffi::CString;
use std::os::raw::{c_char, c_int, c_uchar, c_void};
use std::sync::{Arc, Mutex};
use winit::{
    dpi::LogicalSize,
    event::{ElementState, Ime, MouseButton, WindowEvent},
    event_loop::EventLoop,
    keyboard::{Key, NamedKey},
    window::Window,
};

pub type EventCallback = Option<extern "C" fn(*const c_char, *mut c_void)>;

struct Backend {
    /// 共享二进制帧；Some(Vec<u8>) 表示有最新帧待渲染
    frame: Arc<Mutex<Option<Vec<u8>>>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
    width: u32,
    height: u32,
}

unsafe impl Send for Backend {}
unsafe impl Sync for Backend {}

#[no_mangle]
pub extern "C" fn gx_window_new(width: c_int, height: c_int, _title: *const c_char) -> *mut c_void {
    let backend = Backend {
        frame: Arc::new(Mutex::new(None)),
        cb: Arc::new(Mutex::new(None)),
        cb_ctx: Arc::new(Mutex::new(std::ptr::null_mut())),
        width: width.max(1) as u32,
        height: height.max(1) as u32,
    };
    Box::into_raw(Box::new(backend)) as *mut c_void
}

/// 传入二进制帧。一次 memcpy 到共享区，事件循环线程下次 redraw 时零拷贝解析。
/// 返回 0=ok, 1=参数错。
#[no_mangle]
pub extern "C" fn gx_window_draw(handle: *mut c_void, buf: *const c_uchar, len: c_int) -> c_int {
    if handle.is_null() || buf.is_null() || len <= 0 {
        return 1;
    }
    let slice = unsafe { std::slice::from_raw_parts(buf, len as usize) };
    let owned = slice.to_vec(); // 唯一一次拷贝
    let backend = unsafe { &*(handle as *const Backend) };
    *backend.frame.lock().unwrap() = Some(owned);
    0
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

#[no_mangle]
pub extern "C" fn gx_window_run(handle: *mut c_void) -> c_int {
    if handle.is_null() {
        return 1;
    }
    let backend = unsafe { &*(handle as *const Backend) };
    let (w, h) = (backend.width, backend.height);
    let result = std::panic::catch_unwind(|| {
        let el = new_event_loop();
        let mut state: Option<Renderer> = None;
        el.run_app(&mut WinitApp {
            state: &mut state,
            width: w,
            height: h,
            frame: backend.frame.clone(),
            cb: backend.cb.clone(),
            cb_ctx: backend.cb_ctx.clone(),
        })
        .expect("run_app");
    });
    if result.is_ok() { 0 } else { 2 }
}

/// 构造事件循环。Linux 下事件循环跑在 Go goroutine 经 cgo 进入的线程（非主线程），
/// 必须显式开启 any_thread；X11/Wayland 两条后端都要开，避免运行环境选 Wayland 时踩坑。
fn new_event_loop() -> EventLoop<()> {
    #[cfg(target_os = "linux")]
    {
        use winit::event_loop::EventLoopBuilder;
        use winit::platform::wayland::EventLoopBuilderExtWayland;
        use winit::platform::x11::EventLoopBuilderExtX11;
        let mut builder: EventLoopBuilder<()> = EventLoopBuilder::new();
        // 两条后端都开，完全限定调用避免 with_any_thread 歧义。
        EventLoopBuilderExtX11::with_any_thread(&mut builder, true);
        EventLoopBuilderExtWayland::with_any_thread(&mut builder, true);
        return builder.build().expect("event loop");
    }
    #[cfg(not(target_os = "linux"))]
    {
        return EventLoop::new().expect("event loop");
    }
}

struct WinitApp<'a> {
    state: &'a mut Option<Renderer>,
    width: u32,
    height: u32,
    frame: Arc<Mutex<Option<Vec<u8>>>>,
    cb: Arc<Mutex<EventCallback>>,
    cb_ctx: Arc<Mutex<*mut c_void>>,
}

impl winit::application::ApplicationHandler for WinitApp<'_> {
    fn resumed(&mut self, el: &winit::event_loop::ActiveEventLoop) {
        if self.state.is_some() {
            return;
        }
        let attrs = Window::default_attributes()
            .with_inner_size(LogicalSize::new(self.width as f64, self.height as f64))
            .with_title("PULSE - GX Rust Backend (binary protocol / cgo)")
            .with_resizable(false);
        let window = Arc::new(el.create_window(attrs).expect("create window"));
        window.set_ime_allowed(true);
        *self.state = Some(pollster::block_on(Renderer::new(window)));
    }

    fn window_event(
        &mut self,
        el: &winit::event_loop::ActiveEventLoop,
        _id: winit::window::WindowId,
        event: WindowEvent,
    ) {
        let Some(renderer) = self.state.as_mut() else { return; };
        match event {
            WindowEvent::Resized(size) => {
                renderer.resize(size.width.max(1), size.height.max(1));
                renderer.window().request_redraw();
            }
            WindowEvent::RedrawRequested => {
                let newest = self.frame.lock().unwrap().take();
                if let Some(buf) = newest {
                    renderer.apply_frame(&buf);
                }
                renderer.render();
                renderer.window().request_redraw();
            }
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::CursorMoved { position, .. } => {
                renderer.set_cursor(position.x as f32, position.y as f32);
            }
            WindowEvent::MouseInput {
                state: ElementState::Pressed,
                button: MouseButton::Left,
                ..
            } => {
                let (x, y) = renderer.cursor();
                if let Some(ev) = renderer.hit_click(x, y) {
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                } else if let Some(ev) = renderer.hit_input(x, y) {
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            }
            WindowEvent::Ime(ime) => match ime {
                Ime::Enabled => fire(&self.cb, &self.cb_ctx, ev_json("ime_enabled")),
                Ime::Disabled => {}
                Ime::Preedit(s, _) => {
                    let ev = protocol::InputEvent { kind: "ime_preedit".into(), node_id: String::new(), event: String::new(), text: s, x: 0.0, y: 0.0 };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
                Ime::Commit(s) => {
                    let ev = protocol::InputEvent { kind: "ime_commit".into(), node_id: String::new(), event: String::new(), text: s, x: 0.0, y: 0.0 };
                    fire(&self.cb, &self.cb_ctx, &ev.to_json());
                }
            },
            WindowEvent::KeyboardInput {
                event: winit::event::KeyEvent { state: ElementState::Pressed, logical_key, .. },
                ..
            } => {
                if logical_key == Key::Named(NamedKey::Backspace) {
                    fire(&self.cb, &self.cb_ctx, ev_json("backspace"));
                } else if logical_key == Key::Named(NamedKey::Escape) {
                    el.exit();
                } else if let Some(text) = logical_key.to_text() {
                    // IME 未激活时（如纯 ASCII / XTEST 注入），字符走 KeyboardInput 而非 Ime::Commit。
                    // 过滤控制字符，其余按 commit 回写文本框。
                    if !text.chars().any(|c| c.is_control()) {
                        let ev = protocol::InputEvent {
                            kind: "ime_commit".into(), node_id: String::new(),
                            event: String::new(), text: text.to_string(), x: 0.0, y: 0.0,
                        };
                        fire(&self.cb, &self.cb_ctx, &ev.to_json());
                    }
                }
            }
            _ => {}
        }
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

#[no_mangle]
pub extern "C" fn gx_window_close(handle: *mut c_void) {
    if !handle.is_null() {
        unsafe { drop(Box::from_raw(handle as *mut Backend)); }
    }
}

#[no_mangle]
pub extern "C" fn gx_window_version() -> *const c_char {
    b"gxbackend-rust 0.2.0 (binary protocol)\0".as_ptr() as *const c_char
}
