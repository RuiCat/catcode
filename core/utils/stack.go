// Package utils 提供项目内通用的工具函数
package utils

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
)

// SafeGo 在 goroutine 中安全执行函数，自动 recover panic 并记录堆栈到 stderr
func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[PANIC] goroutine %s: %v\n%s\n", name, r, debug.Stack())
			}
		}()
		fn()
	}()
}

// GetStack 获取当前调用堆栈的字符串表示（最多 4096 字节）
func GetStack() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
