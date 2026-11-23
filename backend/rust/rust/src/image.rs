//! img 原语的 Rust 侧解码与缓存。
//!
//! src 语义与 Go 侧 runtime/imgutil 一致：data URI（base64 PNG）、本地文件路径。
//! HTTP 在 Rust 端不支持（无同步 HTTP 栈），由 Go adapter 预先转成 data URI。
//! 解码复用 tiny-skia 的 Pixmap::decode_png（已含 png-format feature），按 src 缓存。

use std::collections::HashMap;
use std::fs;
use std::sync::Mutex;

/// 解码后的图片：tiny-skia Pixmap 可直接 draw_pixmap 合成。
pub type DecodedImage = tiny_skia::Pixmap;

static CACHE: Mutex<Option<HashMap<String, Option<DecodedImage>>>> = Mutex::new(None);

/// 按 src 取图片。None 表示解码失败或没有该图（调用方安全跳过绘制）。
/// 缓存也记录失败（值为 None），避免每帧重试坏图。
pub fn load(src: &str) -> Option<DecodedImage> {
    let src = src.trim();
    if src.is_empty() {
        return None;
    }
    let mut guard = CACHE.lock().ok()?;
    let map = guard.get_or_insert_with(HashMap::new);
    if let Some(hit) = map.get(src) {
        return hit.clone();
    }
    let decoded = decode(src);
    map.insert(src.to_string(), decoded.clone());
    decoded
}

fn decode(src: &str) -> Option<DecodedImage> {
    let bytes: Vec<u8> = if let Some(payload) = src.strip_prefix("data:") {
        let comma = payload.find(',')?;
        let meta = &payload[..comma];
        let body = &payload[comma + 1..];
        if meta.to_ascii_lowercase().contains("base64") {
            base64_decode(body.trim())?
        } else {
            percent_decode(body)
        }
    } else if src.starts_with("http://") || src.starts_with("https://") {
        // Rust 端无同步 HTTP 栈；Go adapter 应已把远程图转成 data URI。
        return None;
    } else {
        fs::read(src).ok()?
    };
    tiny_skia::Pixmap::decode_png(&bytes).ok()
}

/// 最小 base64 解码（标准 alphabet + URL-safe 变体，跳过空白）。
fn base64_decode(s: &str) -> Option<Vec<u8>> {
    fn val(c: u8) -> Option<u8> {
        match c {
            b'A'..=b'Z' => Some(c - b'A'),
            b'a'..=b'z' => Some(c - b'a' + 26),
            b'0'..=b'9' => Some(c - b'0' + 52),
            b'+' | b'-' => Some(62),
            b'/' | b'_' => Some(63),
            b'=' => None,
            b'\n' | b'\r' | b' ' | b'\t' => None,
            _ => return None,
        }
    }
    let mut out = Vec::new();
    let mut acc: u32 = 0;
    let mut bits = 0u32;
    for &c in s.as_bytes() {
        if c == b'=' {
            break;
        }
        if matches!(c, b'\n' | b'\r' | b' ' | b'\t') {
            continue;
        }
        let v = val(c)?;
        acc = (acc << 6) | v as u32;
        bits += 6;
        if bits >= 8 {
            bits -= 8;
            out.push((acc >> bits) as u8);
        }
    }
    Some(out)
}

fn percent_decode(s: &str) -> Vec<u8> {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'%' && i + 2 < b.len() {
            if let (Some(h), Some(l)) = (hex(b[i + 1]), hex(b[i + 2])) {
                out.push((h << 4) | l);
                i += 3;
                continue;
            }
        }
        out.push(b[i]);
        i += 1;
    }
    out
}

fn hex(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        b'A'..=b'F' => Some(c - b'A' + 10),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn base64_basic() {
        assert_eq!(base64_decode("SGk=").unwrap(), b"Hi");
        assert_eq!(base64_decode("S G\nk=").unwrap(), b"Hi");
    }

    #[test]
    fn base64_urlsafe() {
        assert_eq!(base64_decode("-_8=").unwrap(), vec![0xfb, 0xff]);
    }

    #[test]
    fn decode_data_uri_png() {
        // 2x2 红色 PNG 的 data URI（与 Go 侧 imgutil 测试同语义）。
        const RED_PNG: &str = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAEUlEQVR4nGP4z8DwH4QZYAwAR8oH+WdZbrcAAAAASUVORK5CYII=";
        let pix = load(RED_PNG).expect("应解码成功");
        assert_eq!((pix.width(), pix.height()), (2, 2));
        let c = pix.pixel(0, 0).unwrap();
        assert_eq!((c.red(), c.green(), c.blue(), c.alpha()), (255, 0, 0, 255));
    }

    #[test]
    fn decode_bad_src_is_cached_failure() {
        let bad = "data:image/png;base64,bm90LWFuLWltYWdl";
        assert!(load(bad).is_none());
        assert!(load(bad).is_none()); // 第二次走缓存，仍 None，不 panic
    }
}
