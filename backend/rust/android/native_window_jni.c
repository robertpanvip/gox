// ANativeWindow 的 JNI 提取（仅供 rustffi_android.go 的 cgo 使用）。
//
// 两条路径：
//  1) 主路径（NativeActivity）：读 android.app.NativeActivity 的私有字段
//     mCurSurfaceHolder（AOSP frameworks/base NativeActivity.java，API 9+
//     稳定存在）→ holder.getSurface() → ANativeWindow_fromSurface。
//     GoNativeActivity 继承 NativeActivity，系统 surfaceCreated 时把 holder
//     存入该字段；视图树里没有 SurfaceView，旧链在此必然失败——这是根因。
//  2) fallback（常规 Activity）：DecorView 深度遍历找 SurfaceView →
//     getHolder().getSurface()。兼容未来换成含 SurfaceView 的宿主。
//
// JNI 的 GetFieldID 可读私有字段（不受 Java 层 setAccessible 限制）；
// 每步带异常检查，失败即清异常并返回 NULL，由 Go 侧重试。
#include <jni.h>
#include <android/native_window.h>
#include <android/native_window_jni.h>
#include <stdlib.h>

// find_surface_view：深度优先遍历 ViewGroup 找 SurfaceView（fallback 路径）。
// 返回的是局部引用，调用方负责 DeleteLocalRef。
static jobject find_surface_view(JNIEnv* env, jobject view, int depth) {
    if (view == NULL || depth > 6) return NULL;
    jclass viewCls = (*env)->GetObjectClass(env, view);
    jclass svCls = (*env)->FindClass(env, "android/view/SurfaceView");
    if (svCls == NULL) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, viewCls);
        return NULL;
    }
    if ((*env)->IsInstanceOf(env, view, svCls)) {
        (*env)->DeleteLocalRef(env, viewCls);
        (*env)->DeleteLocalRef(env, svCls);
        return view; // 调用方持有 view 的引用，直接返回
    }
    jclass vgCls = (*env)->FindClass(env, "android/view/ViewGroup");
    jobject found = NULL;
    if (vgCls != NULL && (*env)->IsInstanceOf(env, view, vgCls)) {
        jmethodID getChildAt = (*env)->GetMethodID(env, vgCls, "getChildAt",
                                                   "(I)Landroid/view/View;");
        jmethodID getChildCount = (*env)->GetMethodID(env, vgCls, "getChildCount", "()I");
        if (getChildAt != NULL && getChildCount != NULL) {
            jint n = (*env)->CallIntMethod(env, view, getChildCount);
            for (jint i = 0; i < n; i++) {
                jobject child = (*env)->CallObjectMethod(env, view, getChildAt, i);
                if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); break; }
                jobject hit = find_surface_view(env, child, depth + 1);
                (*env)->DeleteLocalRef(env, child);
                if (hit != NULL) { found = hit; break; }
            }
        } else if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
        }
    } else if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
    }
    if (vgCls != NULL) (*env)->DeleteLocalRef(env, vgCls);
    (*env)->DeleteLocalRef(env, viewCls);
    (*env)->DeleteLocalRef(env, svCls);
    return found;
}

// holder → Surface → ANativeWindow。holder 为局部引用，函数内消费后删除。
static void* window_from_holder(JNIEnv* env, jobject holder) {
    if (holder == NULL) return NULL;
    jclass holderCls = (*env)->GetObjectClass(env, holder);
    jmethodID getSurface = (*env)->GetMethodID(env, holderCls, "getSurface",
                                               "()Landroid/view/Surface;");
    (*env)->DeleteLocalRef(env, holderCls);
    if (getSurface == NULL || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        return NULL;
    }
    jobject surface = (*env)->CallObjectMethod(env, holder, getSurface);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); return NULL; }
    if (surface == NULL) return NULL;
    ANativeWindow* win = ANativeWindow_fromSurface(env, surface);
    (*env)->DeleteLocalRef(env, surface);
    // ANativeWindow 引用交给 native 宿主（gx_android_* / Rust Renderer）
    // 管理生命周期，此处不 release（Activity 级单例，进程退出统一回收）。
    return (void*)win;
}

void* gx_go_native_window_from_activity(JNIEnv* env, jobject activity) {
    if (env == NULL || activity == NULL) return NULL;
    void* result = NULL;

    // ---- 路径 1：NativeActivity.mCurSurfaceHolder ----
    jclass actCls = (*env)->GetObjectClass(env, activity);
    if (actCls != NULL) {
        jfieldID fid = (*env)->GetFieldID(env, actCls, "mCurSurfaceHolder",
                                          "Landroid/view/SurfaceHolder;");
        if (fid != NULL && !(*env)->ExceptionCheck(env)) {
            jobject holder = (*env)->GetObjectField(env, activity, fid);
            if (!(*env)->ExceptionCheck(env)) {
                result = window_from_holder(env, holder);
                if (holder != NULL) (*env)->DeleteLocalRef(env, holder);
            } else {
                (*env)->ExceptionClear(env);
            }
        } else if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
        }
        (*env)->DeleteLocalRef(env, actCls);
    }
    if (result != NULL) return result;

    // ---- 路径 2：DecorView → SurfaceView（fallback）----
    jmethodID getWindow = (*env)->GetMethodID(env,
        (*env)->GetObjectClass(env, activity), "getWindow", "()Landroid/view/Window;");
    if (getWindow == NULL || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        return NULL;
    }
    jobject window = (*env)->CallObjectMethod(env, activity, getWindow);
    if (window == NULL || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        return NULL;
    }
    jclass winCls = (*env)->GetObjectClass(env, window);
    jmethodID getDecor = (*env)->GetMethodID(env, winCls, "getDecorView",
                                             "()Landroid/view/View;");
    (*env)->DeleteLocalRef(env, winCls);
    jobject decor = (getDecor != NULL)
        ? (*env)->CallObjectMethod(env, window, getDecor) : NULL;
    (*env)->DeleteLocalRef(env, window);
    if (decor == NULL || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        return NULL;
    }
    jobject sv = find_surface_view(env, decor, 0);
    (*env)->DeleteLocalRef(env, decor);
    if (sv != NULL) {
        jclass svCls = (*env)->GetObjectClass(env, sv);
        jmethodID getHolder = (*env)->GetMethodID(env, svCls, "getHolder",
                                                  "()Landroid/view/SurfaceHolder;");
        (*env)->DeleteLocalRef(env, svCls);
        jobject holder = (getHolder != NULL)
            ? (*env)->CallObjectMethod(env, sv, getHolder) : NULL;
        (*env)->DeleteLocalRef(env, sv);
        if (!(*env)->ExceptionCheck(env)) {
            result = window_from_holder(env, holder);
        } else {
            (*env)->ExceptionClear(env);
        }
        if (holder != NULL) (*env)->DeleteLocalRef(env, holder);
    }
    return result;
}
