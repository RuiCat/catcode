package utils

// TruncateStr 截断字符串到指定长度，超出部分用 "..." 表示
func TruncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
