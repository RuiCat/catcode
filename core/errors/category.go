// Package errors 实现统一错误处理，包含堆栈跟踪、错误分类、自纠正计数器
// 和延迟错误收集器。纯标准库实现，无外部依赖。
package errors

// Category 错误类别常量
const (
	CategoryAPI        = "API" // API 请求错误（400/500等）
	CategoryTool       = "工具"  // 工具执行错误
	CategoryPermission = "权限"  // 权限拒绝
	CategoryLLM        = "LLM" // LLM 提供商标识错误
	CategoryNetwork    = "网络"  // 网络/超时错误
	CategoryConfig     = "配置"  // 配置文件错误
	CategoryStorage    = "存储"  // 数据库/文件存储错误
	CategorySession    = "会话"  // 会话状态错误
	CategoryInternal   = "内部"  // 内部/非预期错误
)
