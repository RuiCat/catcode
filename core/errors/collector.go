package errors

import (
	"fmt"
	"strings"
)

// ErrorCollector 延迟错误收集器
// 在工具执行循环中收集多个错误，统一注入而不破坏 tool 消息配对。
// 替代 architect.go 和 pool.go 中重复的 collectToolError 逻辑。
type ErrorCollector struct {
	maxErrors  int      // 最大错误数（超过后停止自纠正）
	errorCount int      // 当前错误计数
	errors     []string // 收集的错误消息
}

// NewCollector 创建错误收集器
func NewCollector(maxErrors int) *ErrorCollector {
	return &ErrorCollector{
		maxErrors: maxErrors,
		errors:    make([]string, 0),
	}
}

// Add 添加一个错误。返回 false 表示已达到上限应停止自纠正。
// result 返回错误描述字符串供统一注入（空字符串表示已停止）
func (ec *ErrorCollector) Add(category string, err error, context string) (string, bool) {
	if ec.errorCount >= ec.maxErrors {
		return "", false // 停止自纠正
	}
	ec.errorCount++

	errMsg := fmt.Sprintf("[%s] %v", category, err)
	if context != "" {
		errMsg += "\n" + context
	}
	ec.errors = append(ec.errors, errMsg)
	return errMsg, true
}

// Count 返回当前错误计数
func (ec *ErrorCollector) Count() int {
	return len(ec.errors)
}

// IsEmpty 返回是否没有收集到错误
func (ec *ErrorCollector) IsEmpty() bool {
	return len(ec.errors) == 0
}

// HasReachedLimit 返回是否已达到错误上限
func (ec *ErrorCollector) HasReachedLimit() bool {
	return ec.errorCount >= ec.maxErrors
}

// FormatFeedback 格式化错误反馈消息（供注入 LLM session 使用）
// 返回空字符串表示无错误需要反馈
func (ec *ErrorCollector) FormatFeedback() string {
	if ec.IsEmpty() {
		return ""
	}
	return fmt.Sprintf("【错误反馈 #%d】%s。请分析错误原因，调整策略并尝试其他方法完成任务。",
		len(ec.errors), strings.Join(ec.errors, "; "))
}

// MaxErrors 返回最大允许的错误数
func (ec *ErrorCollector) MaxErrors() int {
	return ec.maxErrors
}

// Reset 重置收集器（供新请求开始时调用）
func (ec *ErrorCollector) Reset() {
	ec.errorCount = 0
	ec.errors = ec.errors[:0]
}
