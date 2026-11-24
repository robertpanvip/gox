package icon

// 本文件预置一组苹果 SF Symbols 风格的矢量图标（24×24 网格）。
//
// 设计约定：
//   - 图标以 stroke 描边为主（线性风格），少数实心图标用 fill；
//   - 颜色统一写 currentColor，由 Icon 组件注入 tone 色；
//   - 不写 stroke-width，由组件/调用方按 size 注入（默认 1.8）；
//   - 每个图标是完整 SVG 字符串，走与外部 SVG 相同的 Parse 路径，两端一致。
//
// 图标数据即矢量路径本身，放大不失真、零位图。

// Builtin 是内置图标注册表：name → SVG 文本。
var Builtin = map[string]string{
	// ---- 导航 ----
	"house": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10.5 L12 3 L21 10.5 M5.5 9 L5.5 20 L18.5 20 L18.5 9 M10 20 L10 14 L14 14 L14 20"/></svg>`,
	"chevron-left":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5 L8 12 L15 19"/></svg>`,
	"chevron-right": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5 L16 12 L9 19"/></svg>`,
	"chevron-up":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 15 L12 8 L19 15"/></svg>`,
	"chevron-down":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 9 L12 16 L19 9"/></svg>`,
	"arrow-left":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 12 L4 12 M10 6 L4 12 L10 18"/></svg>`,
	"arrow-right": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12 L20 12 M14 6 L20 12 L14 18"/></svg>`,
	"arrow-up":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20 L12 4 M6 10 L12 4 L18 10"/></svg>`,
	"arrow-down":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4 L12 20 M6 14 L12 20 L18 14"/></svg>`,

	// ---- 操作 ----
	"plus":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M12 4 L12 20 M4 12 L20 12"/></svg>`,
	"minus":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 12 L20 12"/></svg>`,
	"close":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M5 5 L19 19 M19 5 L5 19"/></svg>`,
	"check":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12 L9 17 L20 6"/></svg>`,
	"edit":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 16 L16 4 L20 8 L8 20 L4 20 Z M14 6 L18 10"/></svg>`,
	"trash":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6 L20 6 M9 6 L9 4 L15 4 L15 6 M6 6 L7 21 L17 21 L18 6 M10 10 L10 17 M14 10 L14 17"/></svg>`,
	"search": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21"/></svg>`,
	"share":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16 L12 3 M8 7 L12 3 L16 7 M5 13 L5 20 L19 20 L19 13"/></svg>`,
	"download": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 L12 16 M7 11 L12 16 L17 11 M4 20 L20 20"/></svg>`,
	"upload":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16 L12 3 M7 8 L12 3 L17 8 M4 20 L20 20"/></svg>`,
	"link": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M10 14 A5 5 0 0 0 17 14 L19 12 A5 5 0 0 0 12 5 L10 7 M14 10 A5 5 0 0 0 7 10 L5 12 A5 5 0 0 0 12 19 L14 17"/></svg>`,

	// ---- 媒体 ----
	"play":  `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M7 4 L20 12 L7 20 Z"/></svg>`,
	"pause": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="3"><path d="M8 4 L8 20 M16 4 L16 20"/></svg>`,
	"camera": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8 L7 8 L9 5 L15 5 L17 8 L21 8 L21 19 L3 19 Z"/><circle cx="12" cy="13" r="4"/></svg>`,
	"photo":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="M21 16 L16 11 L5 21"/></svg>`,

	// ---- 对象 ----
	"person":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8" r="4"/><path d="M4 21 C4 16 8 14 12 14 C16 14 20 16 20 21"/></svg>`,
	"heart":    `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 21 C12 21 3 15 3 9 C3 6 5 4 8 4 C10 4 11.5 5.5 12 6.5 C12.5 5.5 14 4 16 4 C19 4 21 6 21 9 C21 15 12 21 12 21 Z"/></svg>`,
	"star":     `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 2 L15 9 L22 9.5 L17 14 L18.5 21 L12 17.5 L5.5 21 L7 14 L2 9.5 L9 9 Z"/></svg>`,
	"bell":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8 A6 6 0 0 0 6 8 C6 15 3 17 3 17 L21 17 C21 17 18 15 18 8 Z M10 20 A2 2 0 0 0 14 20"/></svg>`,
	"gear":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 2 L12 5 M12 19 L12 22 M4.2 4.2 L6.4 6.4 M17.6 17.6 L19.8 19.8 M2 12 L5 12 M19 12 L22 12 M4.2 19.8 L6.4 17.6 M17.6 6.4 L19.8 4.2"/></svg>`,
	"folder":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6 L9 6 L11 8 L21 8 L21 19 L3 19 Z"/></svg>`,
	"bookmark": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3 L18 3 L18 21 L12 16 L6 21 Z"/></svg>`,
	"mail":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7 L12 13 L21 7"/></svg>`,
	"lock":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10 L8 7 A4 4 0 0 1 16 7 L16 10"/></svg>`,
	"calendar": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10 L21 10 M8 3 L8 7 M16 3 L16 7"/></svg>`,
	"clock":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 7 L12 12 L16 14"/></svg>`,
	"location": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22 C12 22 5 15 5 10 A7 7 0 0 1 19 10 C19 15 12 22 12 22 Z"/><circle cx="12" cy="10" r="2.5"/></svg>`,

	// ---- 状态 ----
	"info":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11 L12 17"/><circle cx="12" cy="7.5" r="0.6" fill="currentColor" stroke="none"/></svg>`,
	"warning":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 L22 20 L2 20 Z M12 9 L12 14 M12 17 L12 17.2"/></svg>`,
	"cloud":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M7 18 A4 4 0 0 1 7 10 A5 5 0 0 1 17 11 A3.5 3.5 0 0 1 17 18 Z"/></svg>`,
	"sun":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2 L12 5 M12 19 L12 22 M2 12 L5 12 M19 12 L22 12 M4.9 4.9 L7 7 M17 17 L19.1 19.1 M4.9 19.1 L7 17 M17 7 L19.1 4.9"/></svg>`,
	"moon":     `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M20 14 A8 8 0 1 1 10 4 A6 6 0 0 0 20 14 Z"/></svg>`,

	// ---- 视图 ----
	"grid": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg>`,
	"list": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M9 6 L20 6 M9 12 L20 12 M9 18 L20 18"/><circle cx="4.5" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="4.5" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="4.5" cy="18" r="1.2" fill="currentColor" stroke="none"/></svg>`,
	"menu": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M3 6 L21 6 M3 12 L21 12 M3 18 L21 18"/></svg>`,
	"more": `<svg viewBox="0 0 24 24"><circle cx="5" cy="12" r="1.8" fill="currentColor"/><circle cx="12" cy="12" r="1.8" fill="currentColor"/><circle cx="19" cy="12" r="1.8" fill="currentColor"/></svg>`,

	// ---- 可见性 ----
	"eye":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12 C2 12 5.5 5 12 5 C18.5 5 22 12 22 12 C22 12 18.5 19 12 19 C5.5 19 2 12 2 12 Z"/><circle cx="12" cy="12" r="3"/></svg>`,
	"eye-off": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4 L20 20"/><path d="M9.9 5.3 A10 10 0 0 1 12 5 C18.5 5 22 12 22 12 C22 12 20.8 14 18.9 15.7 M6.1 6.6 C3.6 8.3 2 12 2 12 C2 12 5.5 19 12 19 C13.6 19 15 18.6 16.2 18.1"/><path d="M10 10 A3 3 0 0 0 14 14"/></svg>`,

	// ---- 更多操作 ----
	"refresh": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12 A9 9 0 0 1 12 21 A9 9 0 0 1 3 12 A9 9 0 0 1 12 3 C16 3 19 5 20.5 8"/><path d="M20.5 3 L20.5 8 L15.5 8"/></svg>`,
	"filter":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 5 L21 5 L14 13 L14 20 L10 18 L10 13 Z"/></svg>`,
	"sliders": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 6 L20 6 M4 12 L20 12 M4 18 L20 18"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="8" cy="18" r="2"/></svg>`,

	// ---- 通信 ----
	"paperclip": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 11 L11 20 A5 5 0 0 1 4 13 L14 3 A3.5 3.5 0 0 1 19 8 L9 18 A2 2 0 0 1 6 15"/></svg>`,
	"send":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2 L11 13 M22 2 L15 22 L11 13 L2 9 Z"/></svg>`,
	"reply":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 14 L4 9 L9 4 M4 9 L14 9 C18 9 20 11 20 15 L20 20"/></svg>`,
	"volume":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 9 L8 9 L13 4 L13 20 L8 15 L4 15 Z"/><path d="M17 9 A4 4 0 0 1 17 15 M19.5 6.5 A7.5 7.5 0 0 1 19.5 17.5"/></svg>`,
	"mic":       `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 11 A7 7 0 0 0 19 11 M12 18 L12 22 M8 22 L16 22"/></svg>`,

	// ---- 设备与状态 ----
	"wifi":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M2 8 A15 15 0 0 1 22 8 M5.5 12 A10 10 0 0 1 18.5 12 M9 16 A5 5 0 0 1 15 16"/><circle cx="12" cy="20" r="1.2" fill="currentColor" stroke="none"/></svg>`,
	"battery":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="7" width="18" height="10" rx="2"/><path d="M22 10 L22 14"/><path d="M6 10 L6 14 M10 10 L10 14 M14 10 L14 14"/></svg>`,
	"bolt":     `<svg viewBox="0 0 24 24"><path fill="currentColor" d="M13 2 L4 14 L11 14 L10 22 L20 9 L13 9 Z"/></svg>`,
	"power":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M12 3 L12 12 M6.5 6 A8 8 0 1 0 17.5 6"/></svg>`,
	"logout":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M10 4 L5 4 A2 2 0 0 0 3 6 L3 18 A2 2 0 0 0 5 20 L10 20 M14 16 L18 12 L14 8 M18 12 L8 12"/></svg>`,
	"login":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M14 4 L19 4 A2 2 0 0 1 21 6 L21 18 A2 2 0 0 1 19 20 L14 20 M10 8 L6 12 L10 16 M6 12 L16 12"/></svg>`,

	// ---- 对象与数据 ----
	"tag":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M20 13 L13 20 A2 2 0 0 1 10 20 L4 14 A2 2 0 0 1 4 11 L11 4 A2 2 0 0 1 14 4 L20 10 A2 2 0 0 1 20 13 Z"/><circle cx="16" cy="8" r="1.3"/></svg>`,
	"flag":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21 L5 4 M5 4 L19 4 L16 9 L19 14 L5 14"/></svg>`,
	"doc":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M6 2 L14 2 L19 7 L19 21 A1 1 0 0 1 18 22 L6 22 A1 1 0 0 1 5 21 L5 3 A1 1 0 0 1 6 2 Z"/><path d="M14 2 L14 7 L19 7 M9 12 L15 12 M9 16 L15 16"/></svg>`,
	"cart":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="20" r="1.5"/><circle cx="18" cy="20" r="1.5"/><path d="M2 3 L5 3 L8 16 L20 16 L20 6 L6 6"/></svg>`,
	"card":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="5" width="20" height="14" rx="2.5"/><path d="M2 10 L22 10 M6 15 L10 15"/></svg>`,
	"chart":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 4 M4 20 L20 20"/><path d="M8 20 L8 12 M13 20 L13 7 M18 20 L18 14"/></svg>`,
	"zoom-in":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21 M10.5 7.5 L10.5 13.5 M7.5 10.5 L13.5 10.5"/></svg>`,
	"zoom-out": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="M15.5 15.5 L21 21 M7.5 10.5 L13.5 10.5"/></svg>`,

	// ---- 表单与编辑 ----
	"text-field": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M7 10 L7 14 M7 12 L11 12"/></svg>`,
	"textarea":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9 L17 9 M7 13 L17 13 M7 17 L13 17"/></svg>`,
	"checkbox":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12 L11 15 L16 9"/></svg>`,
	"check-square": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12 L11 15 L16 9"/></svg>`,
	"radio":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="4" fill="currentColor" stroke="none"/></svg>`,
	"select":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6" width="18" height="12" rx="2"/><path d="M15 11 L17.5 13.5 L20 11"/></svg>`,
	"stepper":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="7" width="16" height="10" rx="2"/><path d="M14 9.5 L16.5 9.5 M15.25 8.25 L15.25 10.75 M14 14.5 L16.5 14.5"/></svg>`,
	"toggle":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="2" y="7" width="20" height="10" rx="5"/><circle cx="16" cy="12" r="3" fill="currentColor" stroke="none"/></svg>`,
	"slider":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><path d="M4 12 L20 12"/><circle cx="9" cy="12" r="3"/><path d="M9 4 L9 7 M9 17 L9 20"/></svg>`,
	"sidebar":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4 L9 20 M5.5 8 L6.5 8 M5.5 12 L6.5 12"/></svg>`,
	"tab":        `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8 A2 2 0 0 1 5 6 L10 6 L12 9 L20 9 A2 2 0 0 1 22 11 L22 18 A2 2 0 0 1 20 20 L5 20 A2 2 0 0 1 3 18 Z"/></svg>`,
	"toolbar":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="5" width="20" height="6" rx="1.5"/><path d="M6 8 L8 8 M11 8 L13 8 M16 8 L18 8"/></svg>`,
	"pencil":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20 L4 16 L16 4 L20 8 L8 20 L4 20 Z M14 6 L18 10"/></svg>`,
	"eraser":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M8 20 L4 16 A2 2 0 0 1 4 13 L13 4 A2 2 0 0 1 16 4 L20 8 A2 2 0 0 1 20 11 L12 20 Z M8 20 L20 20 M9 9 L15 15"/></svg>`,
	"cursor":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M5 3 L5 19 L10 14 L13.5 21 L16.5 19.5 L13 13 L20 12 Z"/></svg>`,
	"command":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9 7 A2 2 0 1 0 7 5 L7 19 A2 2 0 1 0 9 17 L15 17 A2 2 0 1 0 17 19 L17 5 A2 2 0 1 0 15 7 Z"/></svg>`,
}

// Aliases 是常见别名 → 规范名，方便业务侧用不同叫法。
var Aliases = map[string]string{
	"home": "house", "settings": "gear", "user": "person", "profile": "person",
	"xmark": "close", "x": "close", "checkmark": "check", "done": "check",
	"image": "photo", "picture": "photo", "envelope": "mail", "pin": "location",
	"alert": "warning", "exclamation": "warning", "trashcan": "trash", "delete": "trash",
	"cancel": "close", "add": "plus", "remove": "minus", "back": "arrow-left",
	"forward": "arrow-right", "caret-left": "chevron-left", "caret-right": "chevron-right",
	"cog": "gear", "lock-closed": "lock", "unlock": "lock",
	// 补充图标别名（视图/可见性/操作/通信/设备/对象）。
	"apps": "grid", "tiles": "grid", "grid-view": "grid",
	"bullets": "list", "hamburger": "menu", "ellipsis": "more", "dots": "more",
	"visible": "eye", "hidden": "eye-off", "eye-slash": "eye-off",
	"reload": "refresh", "sync": "refresh", "funnel": "filter",
	"tune": "sliders", "adjust": "sliders", "controls": "sliders",
	"attach": "paperclip", "attachment": "paperclip", "paper-clip": "paperclip",
	"volume-up": "volume", "sound": "volume", "speaker": "volume",
	"microphone": "mic", "mute": "mic",
	"wireless": "wifi", "battery-full": "battery",
	"flash": "bolt", "lightning": "bolt", "power-off": "power",
	"signout": "logout", "sign-out": "logout", "log-out": "logout",
	"signin": "login", "sign-in": "login", "log-in": "login",
	"label": "tag", "report": "flag",
	"file": "doc", "document": "doc", "files": "doc",
	"shopping-cart": "cart", "basket": "cart",
	"credit-card": "card", "payment": "card",
	"graph": "chart", "bar-chart": "chart", "analytics": "chart",
	"zoom": "zoom-in", "magnify": "zoom-in", "search-plus": "zoom-in",
	"search-minus": "zoom-out",
	// 表单与编辑别名。
	"input": "text-field", "entry": "text-field",
	"multiline": "textarea", "text-area": "textarea",
	"tick": "checkbox", "checked": "checkbox",
	"radio-button": "radio",
	"dropdown": "select", "combo": "select", "picker": "select",
	"switch": "toggle", "toggle-switch": "toggle",
	"range": "slider", "seek": "slider",
	"panel": "sidebar", "drawer": "sidebar",
	"tabbar": "tab", "tabs": "tab",
	"edit-pencil": "pencil", "write": "pencil",
	"cmd": "command",
}

// Names 返回所有可用的内置图标名（规范名，不含别名），已排序。
func Names() []string {
	out := make([]string, 0, len(Builtin))
	for k := range Builtin {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// Lookup 按名称查内置图标 SVG；支持别名。未找到返回 ok=false。
func Lookup(name string) (svg string, ok bool) {
	if canonical, isAlias := Aliases[name]; isAlias {
		name = canonical
	}
	svg, ok = Builtin[name]
	return
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
