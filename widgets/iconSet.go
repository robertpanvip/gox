// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 iconSet.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
//
// 具名矢量图标组件集：把内置 SVG 图标用 GX 语言预制为强类型组件。
// 每个图标 = 一个 SVG 源码常量 + 一个 IconXxx 具名组件，业务侧可直接
//   <IconHouse size="24" tone="blue"/>  或  IconSearch(gxscene.Props{...})
// 复用 iconFromSVG：SVG 在 host 层解析成 canvas 矢量指令，跨后端一致、放大不失真。
package widgets

import gxscene "github.com/robertpanvip/gox/runtime/scene"

// 内置图标 SVG 源（24×24 网格，Apple SF Symbols 线性风格）。
const (
	// ---- 导航 ----
	svgHouse = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10.5 L12 3 L21 10.5 M5.5 9 L5.5 20 L18.5 20 L18.5 9 M10 20 L10 14 L14 14 L14 20"/></svg>`
	svgChevronLeft = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5 L8 12 L15 19"/></svg>`
	svgChevronRight = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5 L16 12 L9 19"/></svg>`
	svgChevronUp = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 15 L12 8 L19 15"/></svg>`
	svgChevronDown = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 9 L12 16 L19 9"/></svg>`
	svgArrowLeft = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 12 L4 12 M10 6 L4 12 L10 18"/></svg>`
	svgArrowRight = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12 L20 12 M14 6 L20 12 L14 18"/></svg>`
	svgArrowUp = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20 L12 4 M6 10 L12 4 L18 10"/></svg>`
	svgArrowDown = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4 L12 20 M6 14 L12 20 L18 14"/></svg>`
	// ---- 操作 ----
	svgPlus = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M12 4 L12 20 M4 12 L20 12"/></svg>`
	svgMinus = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 12 L20 12"/></svg>`
	svgClose = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M5 5 L19 19 M19 5 L5 19"/></svg>`
	svgCheck = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12 L9 17 L20 6"/></svg>`
	svgEdit = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 16 L16 4 L20 8 L8 20 L4 20 Z M14 6 L18 10"/></svg>`
	svgTrash = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6 L20 6 M9 6 L9 4 L15 4 L15 6 M6 6 L7 21 L17 21 L18 6 M10 10 L10 17 M14 10 L14 17"/></svg>`
	svgSearch = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21"/></svg>`
	svgShare = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16 L12 3 M8 7 L12 3 L16 7 M5 13 L5 20 L19 20 L19 13"/></svg>`
	svgDownload = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 L12 16 M7 11 L12 16 L17 11 M4 20 L20 20"/></svg>`
	svgUpload = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16 L12 3 M7 8 L12 3 L17 8 M4 20 L20 20"/></svg>`
	svgLink = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M10 14 A5 5 0 0 0 17 14 L19 12 A5 5 0 0 0 12 5 L10 7 M14 10 A5 5 0 0 0 7 10 L5 12 A5 5 0 0 0 12 19 L14 17"/></svg>`
	// ---- 媒体 ----
	svgPlay = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M7 4 L20 12 L7 20 Z"/></svg>`
	svgPause = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="3"><path d="M8 4 L8 20 M16 4 L16 20"/></svg>`
	svgCamera = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8 L7 8 L9 5 L15 5 L17 8 L21 8 L21 19 L3 19 Z"/><circle cx="12" cy="13" r="4"/></svg>`
	svgPhoto = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="M21 16 L16 11 L5 21"/></svg>`
	// ---- 对象 ----
	svgPerson = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8" r="4"/><path d="M4 21 C4 16 8 14 12 14 C16 14 20 16 20 21"/></svg>`
	svgHeart = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 21 C12 21 3 15 3 9 C3 6 5 4 8 4 C10 4 11.5 5.5 12 6.5 C12.5 5.5 14 4 16 4 C19 4 21 6 21 9 C21 15 12 21 12 21 Z"/></svg>`
	svgStar = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 2 L15 9 L22 9.5 L17 14 L18.5 21 L12 17.5 L5.5 21 L7 14 L2 9.5 L9 9 Z"/></svg>`
	svgBell = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8 A6 6 0 0 0 6 8 C6 15 3 17 3 17 L21 17 C21 17 18 15 18 8 Z M10 20 A2 2 0 0 0 14 20"/></svg>`
	svgGear = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 2 L12 5 M12 19 L12 22 M4.2 4.2 L6.4 6.4 M17.6 17.6 L19.8 19.8 M2 12 L5 12 M19 12 L22 12 M4.2 19.8 L6.4 17.6 M17.6 6.4 L19.8 4.2"/></svg>`
	svgFolder = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6 L9 6 L11 8 L21 8 L21 19 L3 19 Z"/></svg>`
	svgBookmark = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3 L18 3 L18 21 L12 16 L6 21 Z"/></svg>`
	svgMail = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7 L12 13 L21 7"/></svg>`
	svgLock = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10 L8 7 A4 4 0 0 1 16 7 L16 10"/></svg>`
	svgCalendar = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10 L21 10 M8 3 L8 7 M16 3 L16 7"/></svg>`
	svgClock = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7 L12 12 L16 14"/></svg>`
	svgLocation = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22 C12 22 5 15 5 10 A7 7 0 0 1 19 10 C19 15 12 22 12 22 Z"/><circle cx="12" cy="10" r="2.5"/></svg>`
	// ---- 状态 ----
	svgInfo = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11 L12 17"/><circle cx="12" cy="7.5" r="0.6" fill="currentColor" stroke="none"/></svg>`
	svgWarning = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 L22 20 L2 20 Z M12 9 L12 14 M12 17 L12 17.2"/></svg>`
	svgCloud = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M7 18 A4 4 0 0 1 7 10 A5 5 0 0 1 17 11 A3.5 3.5 0 0 1 17 18 Z"/></svg>`
	svgSun = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2 L12 5 M12 19 L12 22 M2 12 L5 12 M19 12 L22 12 M4.9 4.9 L7 7 M17 17 L19.1 19.1 M4.9 19.1 L7 17 M17 7 L19.1 4.9"/></svg>`
	svgMoon = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M20 14 A8 8 0 1 1 10 4 A6 6 0 0 0 20 14 Z"/></svg>`
	// ---- 视图 ----
	svgGrid = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg>`
	svgList = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M9 6 L20 6 M9 12 L20 12 M9 18 L20 18"/><circle cx="4.5" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="4.5" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="4.5" cy="18" r="1.2" fill="currentColor" stroke="none"/></svg>`
	svgMenu = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M3 6 L21 6 M3 12 L21 12 M3 18 L21 18"/></svg>`
	svgMore = `<svg viewBox="0 0 24 24"><circle cx="5" cy="12" r="1.8" fill="currentColor"/><circle cx="12" cy="12" r="1.8" fill="currentColor"/><circle cx="19" cy="12" r="1.8" fill="currentColor"/></svg>`
	// ---- 可见性 ----
	svgEye = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12 C2 12 5.5 5 12 5 C18.5 5 22 12 22 12 C22 12 18.5 19 12 19 C5.5 19 2 12 2 12 Z"/><circle cx="12" cy="12" r="3"/></svg>`
	svgEyeOff = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4 L20 20"/><path d="M9.9 5.3 A10 10 0 0 1 12 5 C18.5 5 22 12 22 12 C22 12 20.8 14 18.9 15.7 M6.1 6.6 C3.6 8.3 2 12 2 12 C2 12 5.5 19 12 19 C13.6 19 15 18.6 16.2 18.1"/><path d="M10 10 A3 3 0 0 0 14 14"/></svg>`
	// ---- 更多操作 ----
	svgRefresh = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12 A9 9 0 0 1 12 21 A9 9 0 0 1 3 12 A9 9 0 0 1 12 3 C16 3 19 5 20.5 8"/><path d="M20.5 3 L20.5 8 L15.5 8"/></svg>`
	svgFilter = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 5 L21 5 L14 13 L14 20 L10 18 L10 13 Z"/></svg>`
	svgSliders = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 6 L20 6 M4 12 L20 12 M4 18 L20 18"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="8" cy="18" r="2"/></svg>`
	// ---- 通信 ----
	svgPaperclip = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11 L11 20 A5 5 0 0 1 4 13 L14 3 A3.5 3.5 0 0 1 19 8 L9 18 A2 2 0 0 1 6 15"/></svg>`
	svgSend = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2 L11 13 M22 2 L15 22 L11 13 L2 9 Z"/></svg>`
	svgReply = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 14 L4 9 L9 4 M4 9 L14 9 C18 9 20 11 20 15 L20 20"/></svg>`
	svgVolume = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 9 L8 9 L13 4 L13 20 L8 15 L4 15 Z"/><path d="M17 9 A4 4 0 0 1 17 15 M19.5 6.5 A7.5 7.5 0 0 1 19.5 17.5"/></svg>`
	svgMic = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 11 A7 7 0 0 0 19 11 M12 18 L12 22 M8 22 L16 22"/></svg>`
	// ---- 设备与状态 ----
	svgWifi = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M2 8 A15 15 0 0 1 22 8 M5.5 12 A10 10 0 0 1 18.5 12 M9 16 A5 5 0 0 1 15 16"/><circle cx="12" cy="20" r="1.2" fill="currentColor" stroke="none"/></svg>`
	svgBattery = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="7" width="18" height="10" rx="2"/><path d="M22 10 L22 14"/><path d="M6 10 L6 14 M10 10 L10 14 M14 10 L14 14"/></svg>`
	svgBolt = `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M13 2 L4 14 L11 14 L10 22 L20 9 L13 9 Z"/></svg>`
	svgPower = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M12 3 L12 12 M6.5 6 A8 8 0 1 0 17.5 6"/></svg>`
	svgLogout = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4 L5 4 A2 2 0 0 0 3 6 L3 18 A2 2 0 0 0 5 20 L10 20 M14 16 L18 12 L14 8 M18 12 L8 12"/></svg>`
	svgLogin = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M14 4 L19 4 A2 2 0 0 1 21 6 L21 18 A2 2 0 0 1 19 20 L14 20 M10 8 L6 12 L10 16 M6 12 L16 12"/></svg>`
	// ---- 对象与数据 ----
	svgTag = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 13 L13 20 A2 2 0 0 1 10 20 L4 14 A2 2 0 0 1 4 11 L11 4 A2 2 0 0 1 14 4 L20 10 A2 2 0 0 1 20 13 Z"/><circle cx="16" cy="8" r="1.3"/></svg>`
	svgFlag = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21 L5 4 M5 4 L19 4 L16 9 L19 14 L5 14"/></svg>`
	svgDoc = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M6 2 L14 2 L19 7 L19 21 A1 1 0 0 1 18 22 L6 22 A1 1 0 0 1 5 21 L5 3 A1 1 0 0 1 6 2 Z"/><path d="M14 2 L14 7 L19 7 M9 12 L15 12 M9 16 L15 16"/></svg>`
	svgCart = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="20" r="1.5"/><circle cx="18" cy="20" r="1.5"/><path d="M2 3 L5 3 L8 16 L20 16 L20 6 L6 6"/></svg>`
	svgCard = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="5" width="20" height="14" rx="2.5"/><path d="M2 10 L22 10 M6 15 L10 15"/></svg>`
	svgChart = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 4 M4 20 L20 20"/><path d="M8 20 L8 12 M13 20 L13 7 M18 20 L18 14"/></svg>`
	svgZoomIn = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21 M10.5 7.5 L10.5 13.5 M7.5 10.5 L13.5 10.5"/></svg>`
	svgZoomOut = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21 M7.5 10.5 L13.5 10.5"/></svg>`
	// ---- 表单与编辑 ----
	svgTextField = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M7 10 L7 14 M7 12 L11 12"/></svg>`
	svgTextarea = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9 L17 9 M7 13 L17 13 M7 17 L13 17"/></svg>`
	svgCheckbox = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12 L11 15 L16 9"/></svg>`
	svgCheckSquare = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12 L11 15 L16 9"/></svg>`
	svgRadio = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="4" fill="currentColor" stroke="none"/></svg>`
	svgSelect = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M15 11 L17.5 13.5 L20 11"/></svg>`
	svgStepper = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="7" width="16" height="10" rx="2"/><path d="M14 9.5 L16.5 9.5 M15.25 8.25 L15.25 10.75 M14 14.5 L16.5 14.5"/></svg>`
	svgToggle = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="2" y="7" width="20" height="10" rx="5"/><circle cx="16" cy="12" r="3" fill="currentColor" stroke="none"/></svg>`
	svgSlider = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 12 L20 12"/><circle cx="9" cy="12" r="3"/><path d="M9 4 L9 7 M9 17 L9 20"/></svg>`
	svgSidebar = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4 L9 20 M5.5 8 L6.5 8 M5.5 12 L6.5 12"/></svg>`
	svgTab = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8 A2 2 0 0 1 5 6 L10 6 L12 9 L20 9 A2 2 0 0 1 22 11 L22 18 A2 2 0 0 1 20 20 L5 20 A2 2 0 0 1 3 18 Z"/></svg>`
	svgToolbar = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="5" width="20" height="6" rx="1.5"/><path d="M6 8 L8 8 M11 8 L13 8 M16 8 L18 8"/></svg>`
	svgPencil = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 16 L16 4 L20 8 L8 20 L4 20 Z M14 6 L18 10"/></svg>`
	svgEraser = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M8 20 L4 16 A2 2 0 0 1 4 13 L13 4 A2 2 0 0 1 16 4 L20 8 A2 2 0 0 1 20 11 L12 20 Z M8 20 L20 20 M9 9 L15 15"/></svg>`
	svgCursor = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3 L5 19 L10 14 L13.5 21 L16.5 19.5 L13 13 L20 12 Z"/></svg>`
	svgCommand = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 7 A2 2 0 1 0 7 5 L7 19 A2 2 0 1 0 9 17 L15 17 A2 2 0 1 0 17 19 L17 5 A2 2 0 1 0 15 7 Z"/></svg>`
)

// ---- 导航 ----

// IconHouse house 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconHouse(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgHouse, props)
}

// IconChevronLeft chevron-left 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconChevronLeft(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgChevronLeft, props)
}

// IconChevronRight chevron-right 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconChevronRight(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgChevronRight, props)
}

// IconChevronUp chevron-up 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconChevronUp(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgChevronUp, props)
}

// IconChevronDown chevron-down 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconChevronDown(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgChevronDown, props)
}

// IconArrowLeft arrow-left 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconArrowLeft(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgArrowLeft, props)
}

// IconArrowRight arrow-right 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconArrowRight(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgArrowRight, props)
}

// IconArrowUp arrow-up 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconArrowUp(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgArrowUp, props)
}

// IconArrowDown arrow-down 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconArrowDown(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgArrowDown, props)
}
// ---- 操作 ----

// IconPlus plus 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPlus(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPlus, props)
}

// IconMinus minus 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMinus(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMinus, props)
}

// IconClose close 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconClose(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgClose, props)
}

// IconCheck check 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCheck(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCheck, props)
}

// IconEdit edit 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconEdit(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgEdit, props)
}

// IconTrash trash 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconTrash(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgTrash, props)
}

// IconSearch search 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSearch(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSearch, props)
}

// IconShare share 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconShare(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgShare, props)
}

// IconDownload download 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconDownload(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgDownload, props)
}

// IconUpload upload 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconUpload(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgUpload, props)
}

// IconLink link 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconLink(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgLink, props)
}
// ---- 媒体 ----

// IconPlay play 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPlay(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPlay, props)
}

// IconPause pause 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPause(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPause, props)
}

// IconCamera camera 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCamera(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCamera, props)
}

// IconPhoto photo 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPhoto(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPhoto, props)
}
// ---- 对象 ----

// IconPerson person 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPerson(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPerson, props)
}

// IconHeart heart 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconHeart(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgHeart, props)
}

// IconStar star 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconStar(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgStar, props)
}

// IconBell bell 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconBell(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgBell, props)
}

// IconGear gear 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconGear(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgGear, props)
}

// IconFolder folder 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconFolder(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgFolder, props)
}

// IconBookmark bookmark 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconBookmark(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgBookmark, props)
}

// IconMail mail 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMail(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMail, props)
}

// IconLock lock 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconLock(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgLock, props)
}

// IconCalendar calendar 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCalendar(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCalendar, props)
}

// IconClock clock 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconClock(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgClock, props)
}

// IconLocation location 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconLocation(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgLocation, props)
}
// ---- 状态 ----

// IconInfo info 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconInfo(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgInfo, props)
}

// IconWarning warning 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconWarning(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgWarning, props)
}

// IconCloud cloud 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCloud(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCloud, props)
}

// IconSun sun 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSun(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSun, props)
}

// IconMoon moon 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMoon(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMoon, props)
}
// ---- 视图 ----

// IconGrid grid 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconGrid(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgGrid, props)
}

// IconList list 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconList(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgList, props)
}

// IconMenu menu 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMenu(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMenu, props)
}

// IconMore more 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMore(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMore, props)
}
// ---- 可见性 ----

// IconEye eye 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconEye(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgEye, props)
}

// IconEyeOff eye-off 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconEyeOff(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgEyeOff, props)
}
// ---- 更多操作 ----

// IconRefresh refresh 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconRefresh(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgRefresh, props)
}

// IconFilter filter 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconFilter(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgFilter, props)
}

// IconSliders sliders 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSliders(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSliders, props)
}
// ---- 通信 ----

// IconPaperclip paperclip 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPaperclip(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPaperclip, props)
}

// IconSend send 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSend(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSend, props)
}

// IconReply reply 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconReply(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgReply, props)
}

// IconVolume volume 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconVolume(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgVolume, props)
}

// IconMic mic 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconMic(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgMic, props)
}
// ---- 设备与状态 ----

// IconWifi wifi 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconWifi(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgWifi, props)
}

// IconBattery battery 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconBattery(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgBattery, props)
}

// IconBolt bolt 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconBolt(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgBolt, props)
}

// IconPower power 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPower(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPower, props)
}

// IconLogout logout 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconLogout(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgLogout, props)
}

// IconLogin login 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconLogin(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgLogin, props)
}
// ---- 对象与数据 ----

// IconTag tag 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconTag(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgTag, props)
}

// IconFlag flag 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconFlag(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgFlag, props)
}

// IconDoc doc 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconDoc(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgDoc, props)
}

// IconCart cart 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCart(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCart, props)
}

// IconCard card 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCard(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCard, props)
}

// IconChart chart 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconChart(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgChart, props)
}

// IconZoomIn zoom-in 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconZoomIn(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgZoomIn, props)
}

// IconZoomOut zoom-out 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconZoomOut(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgZoomOut, props)
}
// ---- 表单与编辑 ----

// IconTextField text-field 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconTextField(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgTextField, props)
}

// IconTextarea textarea 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconTextarea(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgTextarea, props)
}

// IconCheckbox checkbox 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCheckbox(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCheckbox, props)
}

// IconCheckSquare check-square 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCheckSquare(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCheckSquare, props)
}

// IconRadio radio 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconRadio(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgRadio, props)
}

// IconSelect select 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSelect(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSelect, props)
}

// IconStepper stepper 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconStepper(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgStepper, props)
}

// IconToggle toggle 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconToggle(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgToggle, props)
}

// IconSlider slider 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSlider(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSlider, props)
}

// IconSidebar sidebar 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconSidebar(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgSidebar, props)
}

// IconTab tab 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconTab(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgTab, props)
}

// IconToolbar toolbar 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconToolbar(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgToolbar, props)
}

// IconPencil pencil 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconPencil(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPencil, props)
}

// IconEraser eraser 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconEraser(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgEraser, props)
}

// IconCursor cursor 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCursor(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCursor, props)
}

// IconCommand command 图标。size 默认 20；color/tone 决定颜色；stroke-width 默认 1.8。
 func IconCommand(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgCommand, props)
}

// ---- 常用别名组件（复用规范图标 SVG）----

// IconHome 等价于内置图标 house 的具名组件。
 func IconHome(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgHouse, props)
}

// IconUser 等价于内置图标 person 的具名组件。
 func IconUser(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgPerson, props)
}

// IconSettings 等价于内置图标 gear 的具名组件。
 func IconSettings(props gxscene.Props) *gxscene.Instance {
	return iconFromSVG(svgGear, props)
}
