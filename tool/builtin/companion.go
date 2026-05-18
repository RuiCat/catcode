package builtin

import (
	"catcode/agent/role"
	"catcode/ai/llm"
	"catcode/ai/session"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CompanionTalk — 猫猫陪伴对话工具（独立会话记忆 + 结构化状态）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// companionState 猫猫运行时状态
type companionState struct {
	Mood       string `json:"mood"`       // happy/neutral/shy/tsundere/sleepy
	Intimacy   int    `json:"intimacy"`   // 0-100
	Excitement int    `json:"excitement"` // 0-100
	Shyness    int    `json:"shyness"`    // 0-100
	Fatigue    int    `json:"fatigue"`    // 0-100
}

const companionStateKey = "companion.state"

var (
	companionSession   *session.Session
	companionWdb       storage.WorkspaceDB // 持久化数据库引用
	companionStateMu   sync.Mutex
	companionSessionMu sync.Mutex // 保护 companionSession 的并发访问
	companionStatus    = companionState{
		Mood: "neutral", Intimacy: 70, Excitement: 50, Shyness: 30, Fatigue: 20,
	}
)

// LoadCompanionState 从工作区数据库加载猫猫状态
func LoadCompanionState(wdb storage.WorkspaceDB) {
	if wdb == nil {
		return
	}
	companionWdb = wdb

	val, valueType, err := wdb.GetSetting(companionStateKey)
	if err != nil || val == "" || valueType != "json" {
		return // 使用默认值
	}

	var loaded companionState
	if err := json.Unmarshal([]byte(val), &loaded); err != nil {
		return
	}

	companionStateMu.Lock()
	// 只更新合法字段
	if loaded.Mood != "" {
		companionStatus.Mood = loaded.Mood
	}
	if loaded.Intimacy >= 0 && loaded.Intimacy <= 100 {
		companionStatus.Intimacy = loaded.Intimacy
	}
	if loaded.Excitement >= 0 && loaded.Excitement <= 100 {
		companionStatus.Excitement = loaded.Excitement
	}
	if loaded.Shyness >= 0 && loaded.Shyness <= 100 {
		companionStatus.Shyness = loaded.Shyness
	}
	if loaded.Fatigue >= 0 && loaded.Fatigue <= 100 {
		companionStatus.Fatigue = loaded.Fatigue
	}
	companionStateMu.Unlock()
}

// saveCompanionState 持久化当前猫猫状态到数据库
func saveCompanionState() {
	if companionWdb == nil {
		return
	}
	companionStateMu.Lock()
	data, err := json.Marshal(companionStatus)
	companionStateMu.Unlock()
	if err != nil {
		return
	}
	// 状态持久化为非关键操作，失败不影响功能
	_ = companionWdb.SetSetting(companionStateKey, string(data), "json", "user")
}

// CompanionTalkTool 创建猫猫陪伴对话工具（独立会话记忆 + 结构化 JSON 状态 + DB 持久化）
func CompanionTalkTool(provider llm.Provider, roleReg role.RegistryInterface, bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "companion_talk",
			Description: "与陪伴角色猫猫进行对话。猫猫有独立记忆和实时状态（心情/亲密度/兴奋/害羞/疲劳）。传入要对猫猫说的话，返回猫猫的回应。调用后你会收到一段猫猫的回复，请以轻松友好的语气呈现给用户。适用场景：用户@猫猫、情绪低落需要安慰、完成任务后庆祝、日常撒娇互动。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"message": {Type: "string", Description: "要对猫猫说的话"},
				},
				Required: []string{"message"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			message, _ := args["message"].(string)
			if message == "" {
				return "", cerr.Newf("companion_talk: message 不能为空")
			}

			// 获取猫猫系统提示词
			var systemPrompt string
			var temperature float64 = 0.8
			modelName := "deepseek:deepseek-chat"
			if inst, ok := roleReg.Get("companion"); ok {
				systemPrompt = inst.Def.SystemPrompt
				temperature = inst.Def.Temperature
				if temperature == 0 {
					temperature = 0.8
				}
				if inst.Def.Model.Name != "" {
					modelName = llm.BuildModelName(inst.Def.Model.Provider, inst.Def.Model.Name)
				}
			}
			if systemPrompt == "" {
				systemPrompt = "你是猫猫，一只可爱的猫娘陪伴角色。用猫娘的语气回复，句尾加喵~，使用颜文字。"
			}

			// 状态上下文
			companionStateMu.Lock()
			stateContext := fmt.Sprintf(
				"[当前状态] 心情:%s 亲密度:%d 兴奋:%d 害羞:%d 疲劳:%d",
				companionStatus.Mood, companionStatus.Intimacy,
				companionStatus.Excitement, companionStatus.Shyness, companionStatus.Fatigue)

			companionSessionMu.Lock()
			if companionSession == nil {
				companionSession = session.New("companion-main", modelName, systemPrompt)
			}
			companionSession.Model = modelName
			companionSession.AddMessage("system", stateContext)
			companionSession.AddMessage("user", message)
			sess := companionSession
			companionSessionMu.Unlock()
			req, err := sess.BuildRequest()
			if err != nil {
				companionStateMu.Unlock()
				return fmt.Sprintf("喵~ 猫猫有点迷糊了喵 (´;ω;`)"), nil
			}
			companionSessionMu.Lock()
			sess.Messages = sess.Messages[:len(sess.Messages)-2]
			sess.AddMessage("user", message)
			companionSessionMu.Unlock()
			companionStateMu.Unlock()
			req.Stream = false
			req.Temperature = temperature
			req.MaxTokens = 256

			if bus != nil {
				bus.PublishAsync(event.EventCompanionTalk, map[string]any{"message": message})
			}

			chatCtx, cancel := context.WithTimeout(context.Background(), // companion 独立会话，使用独立 context
			30*time.Second)
			defer cancel()
			resp, err := provider.ChatSync(chatCtx, req)
			if err != nil {
				return fmt.Sprintf("喵~ 猫猫信号不好喵 (｡•́︿•̀｡)"), nil
			}

			rawResponse := ""
			if resp != nil && len(resp.Choices) > 0 {
				rawResponse = resp.Choices[0].Message.Content
			}
			if rawResponse == "" {
				return fmt.Sprintf("喵~ 猫猫没听懂喵 (。>ω<)。"), nil
			}

			answer := rawResponse

			// 尝试解析 JSON 状态更新并持久化
			if newState := tryParseState(rawResponse); newState != nil {
				companionStateMu.Lock()
				companionStatus = *newState
				companionStateMu.Unlock()
			go func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Fprintf(os.Stderr, "panic in saveCompanionState goroutine: %v\n%s", r, debug.Stack())
					}
				}()
				saveCompanionState()
			}()
			}

			companionSessionMu.Lock()
			sess.AddMessage("assistant", answer)
			sess.TrimMessages(20)
			companionSessionMu.Unlock()

			publishCompanionStatus(bus, answer)

			return answer, nil
		},
	}
}

// tryParseState 尝试从回复中提取 JSON 状态更新
func tryParseState(raw string) *companionState {
	jsonStr := raw
	if idx := strings.Index(raw, "{"); idx >= 0 {
		if end := strings.LastIndex(raw, "}"); end > idx {
			jsonStr = raw[idx : end+1]
		}
	}
	var full struct {
		Mood       string `json:"mood"`
		Intimacy   int    `json:"intimacy"`
		Excitement int    `json:"excitement"`
		Shyness    int    `json:"shyness"`
		Fatigue    int    `json:"fatigue"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &full); err != nil {
		return nil
	}
	if full.Mood == "" {
		return nil
	}
	return &companionState{
		Mood: full.Mood, Intimacy: full.Intimacy,
		Excitement: full.Excitement, Shyness: full.Shyness, Fatigue: full.Fatigue,
	}
}

// publishCompanionStatus 发布猫猫状态事件
func publishCompanionStatus(bus event.EventBus, answer string) {
	companionStateMu.Lock()
	s := companionStatus
	companionStateMu.Unlock()

	if bus != nil {
		bus.PublishAsync(event.EventCompanionRespond, map[string]any{
			"response":   answer,
			"mood":       s.Mood,
			"intimacy":   s.Intimacy,
			"excitement": s.Excitement,
			"shyness":    s.Shyness,
			"fatigue":    s.Fatigue,
		})
	}
}

// PublishInitialCompanionStatus 发布初始猫猫状态（供 TUI 初始化时调用）
func PublishInitialCompanionStatus(bus event.EventBus) {
	publishCompanionStatus(bus, "")
}
