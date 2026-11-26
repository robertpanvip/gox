// ANativeWindow 的 JNI 反射链（仅供 rustffi_android.go 的 cgo 使用）。
//
// 链路：GoNativeActivity（ctx）→ getWindow() → getDecorView() → 深度遍历
// 视图树找 android.view.SurfaceView → getHolder().getSurface() →
// ANativeWindow_fromSurface(env, surface)。
//
// 全部 JNI 调用带异常检查：任何一步失败/异常即返回 NULL，由 Go 侧重试
// （surface 生命周期由系统管理，CrossOn 前可能尚未挂载）。
#include <jni.h>
#include <android/native_window.h>
#include <android/native_window_jni.h>
#include <stdlib.h>

// findSurfaceView：深度优先遍历 ViewGroup 找 SurfaceView，返回 Surface 对象
// 或 NULL。持有局部引用由调用方 DeleteLocalRef。
static jobject find_surface_view(JNIEnv* env, jobject view, int depth) {
    if (view == NULL || depth > 6) return NULL;
    jclass viewCls = (*env)->GetObjectClass(env, view);
    // instanceof SurfaceView？
    jclass svCls = (*env)->FindClass(env, "android/view/SurfaceView");
    if ((*env)->IsInstanceOf(env, view, svCls)) {
        (*env)->DeleteLocalRef(env, viewCls);
        (*env)->DeleteLocalRef(env, svCls);
        return view;
    }
    // 若是 ViewGroup：遍历 children
    jmethodID getChildAt = (*env)->GetMethodID(env, viewCls, "getChildAt",
                                               "(I)Landroid/view/View;");
    jmethodID getChildCount = (*env)->GetMethodID(env, viewCls, "getChildCount", "()I");
    if (getChildAt != NULL && getChildCount != NULL) {
        // ViewGroup 检查：IsInstanceOf ViewGroup
        jclass vgCls = (*env)->FindClass(env, "android/view/ViewGroup");
        if ((*env)->IsInstanceOf(env, view, vgCls)) {
            jint n = (*env)->CallIntMethod(env, view, getChildCount);
            for (jint i = 0; i < n; i++) {
                jobject child = (*env)->CallObjectMethod(env, view, getChildAt, i);
                if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); continue; }
                jobject hit = find_surface_view(env, child, depth + 1);
                if (hit != NULL) {
                    (*env)->DeleteLocalRef(env, child);
                    (*env)->DeleteLocalRef(env, vgCls);
                    (*env)->DeleteLocalRef(env, viewCls);
                    (*env)->DeleteLocalRef(env, svCls);
                    return hit;
                }
                if (child != NULL) (*env)->DeleteLocalRef(env, child);
            }
        }
        (*env)->DeleteLocalRef(env, vgCls);
    }
    (*env)->DeleteLocalRef(env, viewCls);
    (*env)->DeleteLocalRef(env, svCls);
    return NULL;
}

void* gx_go_native_window_from_activity(JNIEnv* env, jobject activity) {
    if (env == NULL || activity == NULL) return NULL;

    // activity.getWindow()
    jclass actCls = (*env)->GetObjectClass(env, activity);
    jmethodID getWindow = (*env)->GetMethodID(env, actCls, "getWindow",
                                              "()Landroid/view/Window;");
    if ((*env)->ExceptionCheck(env) || getWindow == NULL) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }
    jobject window = (*env)->CallObjectMethod(env, activity, getWindow);
    if ((*env)->ExceptionCheck(env) || window == NULL) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }

    // window.getDecorView()
    jclass winCls = (*env)->GetObjectClass(env, window);
    jmethodID getDecor = (*env)->GetMethodID(env, winCls, "getDecorView",
                                             "()Landroid/view/View;");
    (*env)->DeleteLocalRef(env, winCls);
    jobject decor = (*env)->CallObjectMethod(env, window, getDecor);
    (*env)->DeleteLocalRef(env, window);
    if ((*env)->ExceptionCheck(env) || decor == NULL) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }

    // 深度遍历找 SurfaceView
    jobject sv = find_surface_view(env, decor, 0);
    (*env)->DeleteLocalRef(env, decor);
    if (sv == NULL) {
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }

    // SurfaceView.getHolder().getSurface()
    jclass svCls = (*env)->GetObjectClass(env, sv);
    jmethodID getHolder = (*env)->GetMethodID(env, svCls, "getHolder",
                                              "()Landroid/view/SurfaceHolder;");
    (*env)->DeleteLocalRef(env, svCls);
    jobject holder = (*env)->CallObjectMethod(env, sv, getHolder);
    (*env)->DeleteLocalRef(env, sv); // find_surface_view 返回的引用
    if ((*env)->ExceptionCheck(env) || holder == NULL) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }
    jclass holderCls = (*env)->GetObjectClass(env, holder);
    jmethodID getSurface = (*env)->GetMethodID(env, holderCls, "getSurface",
                                               "()Landroid/view/Surface;");
    (*env)->DeleteLocalRef(env, holderCls);
    jobject surface = (*env)->CallObjectMethod(env, holder, getSurface);
    (*env)->DeleteLocalRef(env, holder);
    if ((*env)->ExceptionCheck(env) || surface == NULL) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, actCls);
        return NULL;
    }

    ANativeWindow* win = ANativeWindow_fromSurface(env, surface);
    (*env)->DeleteLocalRef(env, surface);
    (*env)->DeleteLocalRef(env, actCls);
    // ANativeWindow 引用归 Go 侧持有（gx_android_* 内部转发给 wgpu；宿主
    // 释放走 ANativeWindow_release——这里交给 Rust Renderer 生命周期，
    // 为简化宿主先不 release（Activity 级单例，泄漏一次可接受）。
    return (void*)win;
}
