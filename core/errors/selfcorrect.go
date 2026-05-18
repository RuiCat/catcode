package errors

// SelfCorrect 自纠正计数器
// 跟踪连续错误次数，在超过上限时停止自动纠正。
type SelfCorrect struct {
	maxRetries int // 最大自纠正次数
	count      int // 当前连续错误计数
}

// NewSelfCorrect 创建自纠正计数器
func NewSelfCorrect(maxRetries int) *SelfCorrect {
	return &SelfCorrect{maxRetries: maxRetries}
}

// Record 记录一次错误，返回是否应继续自纠正
func (sc *SelfCorrect) Record() bool {
	sc.count++
	return sc.count <= sc.maxRetries
}

// CanContinue 返回是否可以继续自纠正
func (sc *SelfCorrect) CanContinue() bool {
	return sc.count <= sc.maxRetries
}

// Count 返回当前错误计数
func (sc *SelfCorrect) Count() int {
	return sc.count
}

// MaxRetries 返回最大自纠正次数
func (sc *SelfCorrect) MaxRetries() int {
	return sc.maxRetries
}

// Reset 重置计数器
func (sc *SelfCorrect) Reset() {
	sc.count = 0
}
