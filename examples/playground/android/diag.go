//go:build android

package main

// 诊断版辅助：把启动日志和崩溃原因写到外部存储，绕开 logcat（用户抓不到）。
// 文件落在 /sdcard/Download/ 下，小米文件管理器「下载」目录可见。

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

var (
	diagFile *os.File
	diagPath string
)

// diagInit 初始化诊断文件，把 log 同时输出到 logcat 和文件。
func diagInit() {
	candidates := []string{
		"/sdcard/Download/gx-diag.log",
		"/storage/emulated/0/Download/gx-diag.log",
		"/storage/emulated/0/Android/data/org.golang.todo.android/files/gx-diag.log",
		"/data/data/org.golang.todo.android/files/gx-diag.log",
	}
	for _, p := range candidates {
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			diagFile = f
			diagPath = p
			break
		}
	}
	if diagFile != nil {
		log.SetOutput(io.MultiWriter(os.Stderr, diagFile))
	}
	log.Printf("===== GX 诊断启动 %s =====", time.Now().Format("2006-01-02 15:04:05"))
	if diagPath != "" {
		log.Printf("诊断文件: %s", diagPath)
	} else {
		log.Printf("警告: 无法创建诊断文件，仅 logcat 可用")
	}
	diagFlush()
}

func diagFlush() {
	if diagFile != nil {
		_ = diagFile.Sync()
	}
}

// diagRecover 在 defer 中捕获 Go 层 panic 并写堆栈。
func diagRecover(where string) {
	if r := recover(); r != nil {
		log.Printf("!!! PANIC in %s: %v", where, r)
		log.Printf("堆栈:\n%s", debug.Stack())
		diagFlush()
	}
}
