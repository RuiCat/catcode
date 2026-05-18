package errors

import (
	"errors"
	"strings"
)

// IsRetryable 判断错误是否可重试（统一版本，替代分散的 isRetryable/isRetryableError）
// 检查错误链中的所有错误，匹配常见的可重试错误模式。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 遍历错误链
	for {
		msg := err.Error()
		if matchRetryablePattern(msg) {
			return true
		}

		// 检查是否是 CatError，通过类别判断
		var ce *CatError
		if errors.As(err, &ce) {
			if ce.Category == CategoryNetwork || ce.Category == CategoryAPI {
				return true
			}
		}

		// 继续解包
		err = errors.Unwrap(err)
		if err == nil {
			break
		}
	}
	return false
}

// matchRetryablePattern 检查错误消息是否匹配可重试模式
func matchRetryablePattern(msg string) bool {
	patterns := []string{
		"429", "500", "502", "503",
		"timeout", "connection",
		"TLS", "reset", "refused",
		"EOF", "no such host",
		"network",
		"重试", // 来自 llm 包的重试提示
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
