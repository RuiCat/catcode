package errors

import (
	"fmt"
	"io"
)

// CatError 统一错误类型，包含堆栈跟踪和错误分类
type CatError struct {
	Message  string // 错误描述消息
	Cause    error  // 原始错误（支持 errors.Unwrap）
	Category string // 错误类别（API/工具/权限等）
	stack    *stack // 调用堆栈
}

// New 创建带堆栈跟踪的新错误
func New(message string) *CatError {
	return &CatError{
		Message:  message,
		Category: CategoryInternal,
		stack:    callers(2), // 跳过 New + 调用者
	}
}

// Newf 创建带堆栈跟踪的格式化错误
func Newf(format string, args ...any) *CatError {
	return New(fmt.Sprintf(format, args...))
}

// Wrap 包装现有错误并添加堆栈跟踪和消息
// 如果 err 已经是 *CatError，保留其原始堆栈
func Wrap(err error, message string) *CatError {
	if err == nil {
		return nil
	}
	ce := &CatError{
		Message:  message,
		Cause:    err,
		Category: categoryFromError(err),
	}
	// 如果原始错误已有堆栈，不重复捕获
	if existing, ok := err.(*CatError); ok && existing.stack != nil {
		ce.stack = existing.stack
	} else {
		ce.stack = callers(2)
	}
	return ce
}

// Wrapf 包装错误并格式化消息
func Wrapf(err error, format string, args ...any) *CatError {
	return Wrap(err, fmt.Sprintf(format, args...))
}

// WithCategory 为错误添加类别标签
func (e *CatError) WithCategory(category string) *CatError {
	e.Category = category
	return e
}

// Error 实现 error 接口，返回简要错误信息
func (e *CatError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap 实现 errors.Unwrap 接口，返回原始错误
func (e *CatError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Format 实现 fmt.Formatter 接口
// %s/%v: 仅显示错误消息链（与 Error() 相同）
// %+v: 显示错误消息链 + 完整堆栈跟踪
func (e *CatError) Format(s fmt.State, verb rune) {
	if e == nil {
		return
	}
	switch verb {
	case 'v':
		if s.Flag('+') {
			// %+v: 详细格式
			io.WriteString(s, e.Error())
			if e.stack != nil {
				io.WriteString(s, "\n堆栈跟踪:\n")
				io.WriteString(s, e.stack.Format())
			}
			// 如果 Cause 也是 CatError，也显示其堆栈
			if cause, ok := e.Cause.(*CatError); ok {
				io.WriteString(s, "由以下错误引起:\n")
				cause.Format(s, verb)
			}
			return
		}
		fallthrough
	case 's':
		io.WriteString(s, e.Error())
	}
}

// StackTrace 返回堆栈跟踪字符串
func (e *CatError) StackTrace() string {
	if e == nil || e.stack == nil {
		return ""
	}
	return e.stack.Format()
}

// categoryFromError 尝试从已有错误中提取类别
func categoryFromError(err error) string {
	if ce, ok := err.(*CatError); ok {
		return ce.Category
	}
	// 根据错误消息关键词猜测类别
	msg := err.Error()
	if containsAny(msg, "400", "401", "403", "404", "429", "500", "502", "503", "API", "api") {
		return CategoryAPI
	}
	if containsAny(msg, "timeout", "connection", "network", "TLS", "refused", "EOF", "no such host") {
		return CategoryNetwork
	}
	if containsAny(msg, "permission", "denied", "权限", "拒绝") {
		return CategoryPermission
	}
	return CategoryInternal
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
