package errors

import (
	"fmt"
	"runtime"
	"strings"
)

// Frame 单个堆栈帧
type Frame struct {
	File     string // 文件名（不含路径）
	Line     int    // 行号
	Function string // 函数名（简短形式）
	FullFile string // 完整文件路径
}

// stack 堆栈跟踪（存储原始 PC 值，格式化时懒解析）
type stack struct {
	pcs    []uintptr // 程序计数器数组
	frames []Frame   // 懒加载的格式化帧
}

// callers 捕获当前调用堆栈
// skip 指定跳过的帧数（0=callers自身, 1=New/Wrap, 2=调用者）
func callers(skip int) *stack {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(skip+1, pcs[:])
	if n == 0 {
		return nil
	}
	return &stack{pcs: pcs[:n]}
}

// Frames 懒加载并返回格式化的堆栈帧
func (s *stack) Frames() []Frame {
	if s == nil {
		return nil
	}
	if s.frames != nil {
		return s.frames
	}

	frames := runtime.CallersFrames(s.pcs)
	s.frames = make([]Frame, 0, len(s.pcs))
	for {
		f, more := frames.Next()
		// 跳过运行时和 errors 包自身的帧
		if skipFrame(f.Function) {
			if !more {
				break
			}
			continue
		}
		s.frames = append(s.frames, Frame{
			File:     trimPath(f.File),
			Line:     f.Line,
			Function: shortFuncName(f.Function),
			FullFile: f.File,
		})
		if !more {
			break
		}
	}
	return s.frames
}

// Format 将堆栈格式化为字符串
func (s *stack) Format() string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	for _, f := range s.Frames() {
		sb.WriteString(fmt.Sprintf("  at %s (%s:%d)\n", f.Function, f.File, f.Line))
	}
	return sb.String()
}

// skipFrame 判断是否应跳过该帧（内部运行时/errors包帧）
func skipFrame(function string) bool {
	// 跳过 runtime 包
	if strings.Contains(function, "runtime.") {
		return true
	}
	// 跳过 errors 包自身
	if strings.Contains(function, "catcode/core/errors.") {
		return true
	}
	return false
}

// shortFuncName 提取简短函数名（去掉包路径前缀）
func shortFuncName(full string) string {
	// 格式: catcode/agent/orchestrator.(*Architect).processStream
	// → (*Architect).processStream
	if idx := strings.LastIndex(full, "/"); idx >= 0 {
		full = full[idx+1:]
	}
	// 去掉包名，保留类型和方法
	if idx := strings.Index(full, "."); idx >= 0 {
		return full[idx+1:]
	}
	return full
}

// trimPath 从完整路径中提取文件名（含上一级目录）
func trimPath(full string) string {
	// /home/user/project/agent/orchestrator/architect.go
	// → orchestrator/architect.go
	idx := strings.LastIndex(full, "/")
	if idx < 0 {
		return full
	}
	prev := strings.LastIndex(full[:idx], "/")
	if prev < 0 {
		return full[prev+1:]
	}
	return full[prev+1:]
}
