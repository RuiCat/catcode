package tool

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Question — 选项框工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// QuestionInfo 单个问题定义
type QuestionInfo struct {
	Question string           `json:"question"` // 完整问题
	Header   string           `json:"header"`   // 短标签
	Options  []QuestionOption `json:"options"`  // 选项列表
	Multiple bool             `json:"multiple"` // 是否多选
}

// QuestionOption 单个选项
type QuestionOption struct {
	Label       string `json:"label"`       // 显示文本
	Description string `json:"description"` // 选项说明
}

// QuestionRequest LLM 发起的提问请求
type QuestionRequest struct {
	Questions []QuestionInfo `json:"questions"` // 问题列表
}

// QuestionAnswer 用户回答
type QuestionAnswer struct {
	Answers [][]string `json:"answers"` // questions[i] → selected labels
}
