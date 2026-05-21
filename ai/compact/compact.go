// Package compact 实现上下文压缩与多级记忆索引
// 借鉴 claude-code 的 compaction 系统与 opencode 的 overflow 检测机制
package compact

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"catcode/ai/llm"
	"catcode/ai/session"
	"catcode/core/utils"
	"catcode/data/embed"
	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 压缩配置常量
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const (
	// AutoCompactBufferRatio 自动压缩缓冲比例（超过50%占用时触发）
	AutoCompactBufferRatio = 0.50

	// MinMessagesForCompact 最小消息数才触发完整压缩
	MinMessagesForCompact = 10

	// PreserveTurns 压缩时保留最近 N 轮用户对话
	PreserveTurns = 2

	// PreserveTokenFraction 保留 token 预算占可用上下文的比率
	PreserveTokenFraction = 0.25

	// MaxPreserveTokens 最多保留 token 数
	MaxPreserveTokens = 8000

	// MinPreserveTokens 最少保留 token 数
	MinPreserveTokens = 2000

	// MicroKeepTools 微压缩保留的工具输出条数
	MicroKeepTools = 6
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 类型定义
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// CompactDecision 压缩决策
type CompactDecision struct {
	Needed   bool
	Level    string // "none" / "micro" / "full"
	Reason   string
	TokenCnt int
}

// CompactResult 压缩结果（借鉴 buildPostCompactMessages）
type CompactResult struct {
	BoundaryMsg    *session.Message // 压缩边界标记
	SummaryContent string           // 结构化摘要内容
	TailStartIndex int              // tail 起始索引（保留的消息从此开始）
	TokensBefore   int              // 压缩前 token 数
}

// SplitResult Head+Tail 分割结果（借鉴 opencode select）
type SplitResult struct {
	HeadStart  int // head 起始索引（待压缩）
	HeadEnd    int // head 结束索引
	TailStart  int // tail 起始索引（保留）
	TailTokens int // tail 估计 token 数
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 压缩决策
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ShouldCompact 检查是否需要压缩
func ShouldCompact(sess *session.Session, contextWindow int) CompactDecision {
	if contextWindow <= 0 {
		contextWindow = 65536
	}
	tokenCnt := sess.TokenCount()
	threshold := int(float64(contextWindow) * (1.0 - AutoCompactBufferRatio))

	if tokenCnt < threshold {
		return CompactDecision{Needed: false, Level: "none", TokenCnt: tokenCnt}
	}

	msgCount := sess.MessageCount()
	if msgCount < MinMessagesForCompact {
		return CompactDecision{
			Needed:   true,
			Level:    "micro",
			Reason:   fmt.Sprintf("token=%d > threshold=%d (但消息数=%d)", tokenCnt, threshold, msgCount),
			TokenCnt: tokenCnt,
		}
	}

	return CompactDecision{
		Needed:   true,
		Level:    "full",
		Reason:   fmt.Sprintf("token=%d > threshold=%d, messages=%d", tokenCnt, threshold, msgCount),
		TokenCnt: tokenCnt,
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Head+Tail 分割
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SelectCompactRange 选择压缩范围（借鉴 opencode select()）
// 保留最近 PreserveTurns 轮用户对话 + tokenBudget 的上下文
func SelectCompactRange(messages []*session.Message, contextWindow int) SplitResult {
	// 计算保留 token 预算
	budget := int(float64(contextWindow) * PreserveTokenFraction)
	if budget > MaxPreserveTokens {
		budget = MaxPreserveTokens
	}
	if budget < MinPreserveTokens {
		budget = MinPreserveTokens
	}

	var result SplitResult
	userTurns := 0
	tokenAccum := 0

	// 从后向前遍历，确定 tail 起始位置
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !msg.Enable {
			continue
		}

		// 计数用户轮次
		if msg.Role == "user" {
			userTurns++
		}

		tokenAccum += llm.EstimateTokens(msg.Content)

		// 保留最近 PreserveTurns 轮，或 token 预算内
		if userTurns >= PreserveTurns || tokenAccum >= budget {
			result.TailStart = i
			result.TailTokens = tokenAccum
			break
		}
	}

	// 如果 tail 覆盖了所有消息，不需要压缩
	if result.TailStart <= 0 || tokenAccum < budget/2 {
		result.TailStart = 0
	}

	result.HeadStart = 0
	result.HeadEnd = result.TailStart
	return result
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 压缩执行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BuildCompactionPrompt 构建压缩提示词
// previousSummary: 前一次摘要，非空时增量更新
// split: 消息分割结果
func BuildCompactionPrompt(messages []*session.Message, previousSummary string, split SplitResult) string {
	if split.HeadEnd <= split.HeadStart {
		return ""
	}

	var sb strings.Builder
	maxChars := 4000
	chars := 0

	// 从 head 范围内提取消息（从旧到新）
	for i := split.HeadStart; i < split.HeadEnd && chars < maxChars; i++ {
		m := messages[i]
		if !m.Enable {
			continue
		}
		prefix := rolePrefix(m)
		line := prefix + utils.TruncateStr(m.Content, 200)
		sb.WriteString(line)
		sb.WriteString("\n")
		chars += len(line)
	}

	history := sb.String()
	if history == "" {
		return ""
	}

	if previousSummary != "" {
		if tmpl, err := embed.GetPrompt("compaction_incremental"); err == nil {
			return fmt.Sprintf(tmpl, previousSummary, history)
		}
	} else {
		if tmpl, err := embed.GetPrompt("compaction"); err == nil {
			return fmt.Sprintf(tmpl, history)
		}
	}
	return ""
}

func rolePrefix(m *session.Message) string {
	switch m.Role {
	case "user":
		return "用户: "
	case "assistant":
		return "助手: "
	case "tool":
		return fmt.Sprintf("[工具 %s]: ", m.Name)
	default:
		return m.Role + ": "
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 消息重建（借鉴 buildPostCompactMessages）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BuildCompactResult 执行完整压缩并返回结果
func BuildCompactResult(messages []*session.Message, previousSummary string,
	contextWindow int, tokenCnt int) *CompactResult {

	split := SelectCompactRange(messages, contextWindow)
	prompt := BuildCompactionPrompt(messages, previousSummary, split)

	// 创建边界标记（借鉴 SystemCompactBoundaryMessage）
	boundary := &session.Message{
		Role: "system",
		Content: fmt.Sprintf("[压缩边界] 此消息之前的对话已被压缩为上下文索引。"+
			"压缩时间: %s, 压缩前 token: %d, 保留消息从索引 %d 开始",
			time.Now().Format("2006-01-02 15:04:05"), tokenCnt, split.TailStart),
	}

	return &CompactResult{
		BoundaryMsg:    boundary,
		SummaryContent: prompt,
		TailStartIndex: split.TailStart,
		TokensBefore:   tokenCnt,
	}
}

// ApplyCompactResult 将压缩结果应用到会话（禁用旧消息、注入边界和摘要）
func ApplyCompactResult(sess *session.Session, result *CompactResult) {
	// 1. 禁用 head 范围内的消息
	for i := 0; i < result.TailStartIndex && i < len(sess.Messages); i++ {
		sess.Messages[i].Enable = false
	}

	// 2. 注入边界标记
	sess.Messages = append(sess.Messages, result.BoundaryMsg)
	result.BoundaryMsg.Update()

	// 3. 注入摘要（以 system 消息形式）
	summaryMsg := &session.Message{
		Role:    "system",
		Content: "[上下文索引]\n" + result.SummaryContent,
		Enable:  true,
	}
	summaryMsg.Update()
	sess.Messages = append(sess.Messages, summaryMsg)

	// 4. 更新 Session.Summary 持久化字段
	sess.SetSummary(result.SummaryContent)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 微压缩
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TrimOldToolOutputs 微压缩：清理旧的工具输出（保留最近 N 条）
func TrimOldToolOutputs(sess *session.Session) {
	count := 0
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		if sess.Messages[i].Role == "tool" {
			count++
			if count > MicroKeepTools {
				sess.Messages[i].Enable = false
			}
		}
	}
}

// SessionMessagesToJSON 序列化会话消息为 JSON（供快照使用）
func SessionMessagesToJSON(messages []*session.Message) string {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var list []msg
	for _, m := range messages {
		if m.Enable {
			list = append(list, msg{Role: m.Role, Content: utils.TruncateStr(m.Content, 500)})
		}
	}
	data, _ := json.Marshal(list)
	return string(data)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 记忆选择器 — 多级索引相关性评分
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SelectRelevantMemories 基于多级索引评分选择与当前上下文相关的记忆条目
func SelectRelevantMemories(wdb storage.WorkspaceDB, context string, maxResults int) ([]*storage.MemoryEntry, error) {
	var allHeaders []scopedHeader
	for _, scope := range []string{"global", "workspace"} {
		headers, err := wdb.ScanMemoryHeaders(scope)
		if err != nil {
			continue
		}
		for _, h := range headers {
			allHeaders = append(allHeaders, scopedHeader{Scope: scope, Header: h})
		}
	}

	type scored struct {
		scope string
		key   string
		score float64
	}
	var scores []scored
	keywords := extractKeywords(context)
	tfidfScores := scoreByTFIDFBoost(convertToHeaders(allHeaders), keywords)

	for _, sh := range allHeaders {
		h := sh.Header
		score := float64(h.Importance) * 0.4
		// TF-IDF 增强加分
		if boost, ok := tfidfScores[h.Key]; ok {
			score += boost
		}
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(h.Description), kw) {
				score += 20
			}
		}
		if h.AgeDays <= 1 {
			score += 10
		} else if h.AgeDays <= 3 {
			score += 5
		} else if h.AgeDays > 30 {
			score -= 10
		}
		if score > 0 {
			scores = append(scores, scored{scope: sh.Scope, key: h.Key, score: score})
		}
	}

	for i := 0; i < len(scores); i++ {
		best := i
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[best].score {
				best = j
			}
		}
		scores[i], scores[best] = scores[best], scores[i]
	}

	limit := maxResults
	if limit > len(scores) {
		limit = len(scores)
	}

	var result []*storage.MemoryEntry
	for i := 0; i < limit; i++ {
		if scores[i].score <= 0 {
			break
		}
		m, err := wdb.GetMemory(scores[i].scope, scores[i].key)
		if err == nil && m != nil {
			result = append(result, m)
		}
	}
	return result, nil
}

type scopedHeader struct {
	Scope  string
	Header *storage.MemoryHeader
}

func extractKeywords(text string) []string {
	text = strings.ToLower(text)
	var result []string
	seen := make(map[string]bool)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == '\n' || r == ':' || r == ';' || r == '!'
	})
	stopWords := map[string]bool{
		"the": true, "a": true, "is": true, "to": true, "of": true, "and": true,
		"in": true, "for": true, "on": true, "with": true, "this": true, "that": true,
		"的": true, "是": true, "了": true, "在": true, "和": true, "有": true,
	}
	for _, w := range words {
		if len(w) < 3 || stopWords[w] {
			continue
		}
		if !seen[w] {
			seen[w] = true
			result = append(result, w)
		}
	}
	runes := []rune(text)
	for i := 0; i < len(runes)-1; i++ {
		if isCJK(runes[i]) && isCJK(runes[i+1]) {
			bigram := string(runes[i : i+2])
			if !seen[bigram] {
				seen[bigram] = true
				result = append(result, bigram)
			}
		}
	}
	return result
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3000 && r <= 0x303F)
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 智能工具输出裁剪
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const (
	// PruneMaxKeep 裁剪后最多保留的工具输出条数
	PruneMaxKeep = 12
	// PruneLongOutputThreshold 长输出阈值（字符）
	PruneLongOutputThreshold = 3000
)

// PruneToolOutputs 智能裁剪工具输出
// 保留最近的工具输出 + 包含关键信息的输出（错误/警告等）
// 注意：调用方必须持有 Session 的写锁（mu.Lock）
func PruneToolOutputs(sess *session.Session) {
	if sess == nil {
		return
	}

	msgs := sess.Messages
	if len(msgs) == 0 {
		return
	}

	// 从后向前收集要保留的工具输出
	keep := PruneMaxKeep
	keptCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role != "tool" || !msg.Enable {
			continue
		}
		content := msg.Content

		// 关键信息检测：保留包含错误/警告的输出
		isCritical := len(content) > 0 && (strings.Contains(content, "error") ||
			strings.Contains(content, "Error") ||
			strings.Contains(content, "失败") ||
			strings.Contains(content, "拒绝") ||
			strings.Contains(content, "denied") ||
			strings.Contains(content, "permission"))

		if keptCount < keep || isCritical {
			keptCount++
			continue
		}

		// 裁剪：用摘要替换长输出，或直接禁用
		if len(content) > PruneLongOutputThreshold {
			msg.Content = summarizeToolOutput(content)
		} else {
			msg.Enable = false
		}
	}
}

// summarizeToolOutput 对长工具输出进行简短摘要
func summarizeToolOutput(content string) string {
	runes := []rune(content)
	if len(runes) <= 400 {
		return content
	}
	return string(runes[:400]) + "\n\n...(工具输出已被裁剪)"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TF-IDF 增强记忆评分（轻量纯Go实现）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// scoreByTFIDFBoost 基于 TF-IDF 对记忆头部进行增强评分
// 返回每条记忆的额外加分（0-15分范围）
func scoreByTFIDFBoost(headers []*storage.MemoryHeader, keywords []string) map[string]float64 {
	if len(headers) == 0 || len(keywords) == 0 {
		return nil
	}

	// 计算每个关键词的 IDF
	totalDocs := float64(len(headers))
	idf := make(map[string]float64)
	for _, kw := range keywords {
		if len(kw) < 2 {
			continue
		}
		docsWithTerm := 0
		for _, h := range headers {
			if strings.Contains(strings.ToLower(h.Description), kw) {
				docsWithTerm++
			}
		}
		if docsWithTerm > 0 {
			idf[kw] = 1.0 + log2(totalDocs/float64(docsWithTerm))
		} else {
			idf[kw] = 1.0
		}
	}

	// 计算每个头部的 TF-IDF 加分
	scores := make(map[string]float64)
	for _, h := range headers {
		desc := strings.ToLower(h.Description)
		var boost float64
		for _, kw := range keywords {
			if len(kw) < 2 {
				continue
			}
			tf := float64(strings.Count(desc, kw))
			if tf > 0 {
				boost += tf * idf[kw] * 3.0 // 权重 3x
			}
		}
		if boost > 0 {
			scores[h.Key] = boost
		}
	}
	return scores
}

// log2 计算 log2(n)，用于 IDF
func log2(n float64) float64 {
	if n <= 0 {
		return 0
	}
	result := 0.0
	for n > 1 {
		n /= 2
		result++
	}
	frac := 0.0
	if n > 0 {
		frac = (n - 1.0) * 1.442695 // 1/ln(2) 近似
	}
	return result + frac
}

// convertToHeaders 将 scopedHeader 切片转换为 MemoryHeader 指针切片
func convertToHeaders(scoped []scopedHeader) []*storage.MemoryHeader {
	headers := make([]*storage.MemoryHeader, len(scoped))
	for i, sh := range scoped {
		headers[i] = sh.Header
	}
	return headers
}
