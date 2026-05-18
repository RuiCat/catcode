// Package plugin 提供基于 yaegi 解释器的 Go 插件加载系统，支持运行时动态注册插件提供的工具和角色定义。
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"catcode/agent/role"
	"catcode/ai/compact"
	"catcode/ai/llm"
	"catcode/ai/session"
	"catcode/core/buffer"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 插件加载器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Loader 基于 yaegi 的 Go 插件加载器
type Loader struct {
	pluginsDir string
	ctx        *PluginContext
}

// NewLoader 创建插件加载器
func NewLoader(pluginsDir string, ctx *PluginContext) *Loader {
	return &Loader{pluginsDir: pluginsDir, ctx: ctx}
}

// Discover 扫描插件目录返回所有 .go 文件
func (l *Loader) Discover() ([]string, error) {
	entries, err := os.ReadDir(l.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, cerr.Wrap(err, "plugin: 扫描目录失败")
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		files = append(files, filepath.Join(l.pluginsDir, entry.Name()))
	}
	return files, nil
}

// Load 加载单个插件文件
// 通过预注册的符号表注入 catcode 内部包，无需 GOPATH 或源码树依赖
func (l *Loader) Load(path string) (Plugin, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, cerr.Wrapf(err, "plugin: 读取文件失败 %s", path)
	}

	i := interp.New(interp.Options{
		Unrestricted: false,
	})
	i.Use(stdlib.Symbols)

	// 注入 catcode 内部包符号表（无需源码树或符号链接）
	i.Use(catcodeSymbols)

	// 注入插件上下文变量
	if _, err := i.Eval(fmt.Sprintf(`var PluginWorkDir = %q`, l.ctx.WorkDir)); err != nil {
		return nil, cerr.Wrap(err, "plugin: 注入上下文失败")
	}

	// 编译插件源码
	if _, err := i.Eval(string(src)); err != nil {
		return nil, cerr.Wrapf(err, "plugin: 编译失败 %s", path)
	}

	// 验证 Plugin 变量存在
	if _, err := i.Eval("Plugin"); err != nil {
		return nil, cerr.Wrap(err, "plugin: 未找到 Plugin 变量 (需要插件中定义 var Plugin 变量)")
	}

	// 获取插件基本信息
	name, err := callStringMethod(i, "Plugin.Name")
	if err != nil {
		return nil, cerr.Wrap(err, "plugin: Name() 调用失败")
	}
	version, err := callStringMethod(i, "Plugin.Version")
	if err != nil {
		return nil, cerr.Wrap(err, "plugin: Version() 调用失败")
	}

	// 判断插件类型
	hasTools := hasMethod(i, "Plugin.Tools")
	hasRole := hasMethod(i, "Plugin.RoleDef")

	// 提取 Tools 和 RoleDef 方法的 reflect.Value，用于后续通过反射调用
	var toolsFunc, roleDefFunc reflect.Value
	if hasTools {
		v, err := i.Eval("Plugin.Tools")
		if err == nil && v.IsValid() {
			toolsFunc = v
		}
	}
	if hasRole {
		v, err := i.Eval("Plugin.RoleDef")
		if err == nil && v.IsValid() {
			roleDefFunc = v
		}
	}

	var infoType string
	switch {
	case hasTools:
		infoType = "tool"
	case hasRole:
		infoType = "role"
	default:
		infoType = "unknown"
	}

	return &pluginWrapper{
		name:        name,
		version:     version,
		infoType:    infoType,
		interp:      i,
		hasTools:    hasTools,
		hasRole:     hasRole,
		ctx:         l.ctx,
		toolsFunc:   toolsFunc,
		roleDefFunc: roleDefFunc,
	}, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// catcode 符号表 — 预注册内部包符号给 yaegi
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// catcodeSymbols 返回 catcode 内部包的符号表
// 插件中的 import "catcode/tool" 等语句通过此符号表解析，
// 无需 GOPATH 或 src/catcode 符号链接
var catcodeSymbols = buildCatcodeSymbols()

func buildCatcodeSymbols() interp.Exports {
	return interp.Exports{
		// ── catcode/tool ──
		"catcode/tool/tool": {
			"Tool":                 reflect.ValueOf((*tool.Tool)(nil)),
			"FuncDef":              reflect.ValueOf((*tool.FuncDef)(nil)),
			"Schema":               reflect.ValueOf((*tool.Schema)(nil)),
			"Property":             reflect.ValueOf((*tool.Property)(nil)),
			"Context":              reflect.ValueOf((*tool.Context)(nil)),
			"PermissionLevel":      reflect.ValueOf((*tool.PermissionLevel)(nil)),
			"PermissionRule":       reflect.ValueOf((*tool.PermissionRule)(nil)),
			"MustMarshalSchema":    reflect.ValueOf(tool.MustMarshalSchema),
			"NewPermissionChecker": reflect.ValueOf(tool.NewPermissionChecker),
			"PermissionFromMap":    reflect.ValueOf(tool.PermissionFromMap),
			"Allow":                reflect.ValueOf(tool.Allow),
			"Ask":                  reflect.ValueOf(tool.Ask),
			"Deny":                 reflect.ValueOf(tool.Deny),
		},

		// ── catcode/core/event ──
		"catcode/core/event/event": {
			"EventBus":          reflect.ValueOf((event.EventBus)(nil)),
			"Event":             reflect.ValueOf((*event.Event)(nil)),
			"Subscriber":        reflect.ValueOf((*event.Subscriber)(nil)),
			"Trigger":           reflect.ValueOf((*event.Trigger)(nil)),
			"TriggerManager":    reflect.ValueOf((*event.TriggerManager)(nil)),
			"NewBus":            reflect.ValueOf(event.NewBus),
			"NewEvent":          reflect.ValueOf(event.NewEvent),
			"NewTriggerManager": reflect.ValueOf(event.NewTriggerManager),
			"ValidateEventName": reflect.ValueOf(event.ValidateEventName),
			// 用户交互
			"EventUserRequestReceived":  reflect.ValueOf(event.EventUserRequestReceived),
			"EventUserRequestCompleted": reflect.ValueOf(event.EventUserRequestCompleted),
			// 角色生命周期
			"EventRoleLoaded":    reflect.ValueOf(event.EventRoleLoaded),
			"EventRoleActivated": reflect.ValueOf(event.EventRoleActivated),
			"EventRoleDispatch":  reflect.ValueOf(event.EventRoleDispatch),
			"EventRoleResult":    reflect.ValueOf(event.EventRoleResult),
			"EventRoleError":     reflect.ValueOf(event.EventRoleError),
			"EventRoleUpdated":   reflect.ValueOf(event.EventRoleUpdated),
			"EventRoleUnloaded":  reflect.ValueOf(event.EventRoleUnloaded),
			// 子智能体
			"EventSubAgentDispatch":   reflect.ValueOf(event.EventSubAgentDispatch),
			"EventSubAgentResult":     reflect.ValueOf(event.EventSubAgentResult),
			"EventSubAgentError":      reflect.ValueOf(event.EventSubAgentError),
			"EventAgentStatusChanged": reflect.ValueOf(event.EventAgentStatusChanged),
			// 规划引擎
			"EventPlanCreated":   reflect.ValueOf(event.EventPlanCreated),
			"EventPlanStepStart": reflect.ValueOf(event.EventPlanStepStart),
			"EventPlanStepDone":  reflect.ValueOf(event.EventPlanStepDone),
			"EventPlanCompleted": reflect.ValueOf(event.EventPlanCompleted),
			// 任务状态
			"EventTaskStarted":   reflect.ValueOf(event.EventTaskStarted),
			"EventTaskCompleted": reflect.ValueOf(event.EventTaskCompleted),
			"EventTaskFailed":    reflect.ValueOf(event.EventTaskFailed),
			// 工具调用
			"EventAgentToolStart": reflect.ValueOf(event.EventAgentToolStart),
			"EventAgentToolEnd":   reflect.ValueOf(event.EventAgentToolEnd),
			"EventToolCallStart":  reflect.ValueOf(event.EventToolCallStart),
			"EventToolCallEnd":    reflect.ValueOf(event.EventToolCallEnd),
			// Session
			"EventSessionCreated": reflect.ValueOf(event.EventSessionCreated),
			"EventSessionSaved":   reflect.ValueOf(event.EventSessionSaved),
			// 陪伴角色
			"EventCompanionTalk":    reflect.ValueOf(event.EventCompanionTalk),
			"EventCompanionRespond": reflect.ValueOf(event.EventCompanionRespond),
			"EventCompanionStatus":  reflect.ValueOf(event.EventCompanionStatus),
			// 对话框/选项
			"EventDialogSend":    reflect.ValueOf(event.EventDialogSend),
			"EventQuestionAsked": reflect.ValueOf(event.EventQuestionAsked),
		},

		// ── catcode/agent/role ──
		"catcode/agent/role/role": {
			"RoleDef":            reflect.ValueOf((*role.RoleDef)(nil)),
			"ModelConfig":        reflect.ValueOf((*role.ModelConfig)(nil)),
			"ThinkingConfig":     reflect.ValueOf((*role.ThinkingConfig)(nil)),
			"ModelLimit":         reflect.ValueOf((*role.ModelLimit)(nil)),
			"TriggerDef":         reflect.ValueOf((*role.TriggerDef)(nil)),
			"StateDef":           reflect.ValueOf((*role.StateDef)(nil)),
			"RoleType":           reflect.ValueOf((*role.RoleType)(nil)),
			"RoleMode":           reflect.ValueOf((*role.RoleMode)(nil)),
			"RoleAgent":          reflect.ValueOf(role.RoleAgent),
			"RoleCompanion":      reflect.ValueOf(role.RoleCompanion),
			"ModePrimary":        reflect.ValueOf(role.ModePrimary),
			"ModeSubAgent":       reflect.ValueOf(role.ModeSubAgent),
			"ModeBackground":     reflect.ValueOf(role.ModeBackground),
			"BuildFullModelName": reflect.ValueOf(role.BuildFullModelName),
			"ParseYAML":          reflect.ValueOf(role.ParseYAML),
		},

		// ── catcode/core/errors ──
		"catcode/core/errors/errors": {
			"CatError":           reflect.ValueOf((*cerr.CatError)(nil)),
			"ErrorCollector":     reflect.ValueOf((*cerr.ErrorCollector)(nil)),
			"SelfCorrect":        reflect.ValueOf((*cerr.SelfCorrect)(nil)),
			"New":                reflect.ValueOf(cerr.New),
			"Newf":               reflect.ValueOf(cerr.Newf),
			"Wrap":               reflect.ValueOf(cerr.Wrap),
			"Wrapf":              reflect.ValueOf(cerr.Wrapf),
			"IsRetryable":        reflect.ValueOf(cerr.IsRetryable),
			"NewCollector":       reflect.ValueOf(cerr.NewCollector),
			"NewSelfCorrect":     reflect.ValueOf(cerr.NewSelfCorrect),
			"CategoryAPI":        reflect.ValueOf(cerr.CategoryAPI),
			"CategoryTool":       reflect.ValueOf(cerr.CategoryTool),
			"CategoryPermission": reflect.ValueOf(cerr.CategoryPermission),
			"CategoryLLM":        reflect.ValueOf(cerr.CategoryLLM),
			"CategoryNetwork":    reflect.ValueOf(cerr.CategoryNetwork),
			"CategoryConfig":     reflect.ValueOf(cerr.CategoryConfig),
			"CategoryStorage":    reflect.ValueOf(cerr.CategoryStorage),
			"CategorySession":    reflect.ValueOf(cerr.CategorySession),
			"CategoryInternal":   reflect.ValueOf(cerr.CategoryInternal),
		},

		// ── catcode/core/buffer ──
		"catcode/core/buffer/buffer": {
			"Buffer": reflect.ValueOf((*buffer.Buffer)(nil)),
			"New":    reflect.ValueOf(buffer.New),
		},

		// ── catcode/ai/llm ──
		"catcode/ai/llm/llm": {
			"Provider":             reflect.ValueOf((*llm.Provider)(nil)),
			"ProviderRegistry":     reflect.ValueOf((*llm.ProviderRegistry)(nil)),
			"ChatRequest":          reflect.ValueOf((*llm.ChatRequest)(nil)),
			"Message":              reflect.ValueOf((*llm.Message)(nil)),
			"ToolDef":              reflect.ValueOf((*llm.ToolDef)(nil)),
			"FuncDef":              reflect.ValueOf((*llm.FuncDef)(nil)),
			"ToolCall":             reflect.ValueOf((*llm.ToolCall)(nil)),
			"ToolCallFunc":         reflect.ValueOf((*llm.ToolCallFunc)(nil)),
			"ChatResponse":         reflect.ValueOf((*llm.ChatResponse)(nil)),
			"Choice":               reflect.ValueOf((*llm.Choice)(nil)),
			"Usage":                reflect.ValueOf((*llm.Usage)(nil)),
			"StreamEvent":          reflect.ValueOf((*llm.StreamEvent)(nil)),
			"StreamEventType":      reflect.ValueOf((*llm.StreamEventType)(nil)),
			"ThinkingConfig":       reflect.ValueOf((*llm.ThinkingConfig)(nil)),
			"OpenAIClient":         reflect.ValueOf((*llm.OpenAIClient)(nil)),
			"NewOpenAI":            reflect.ValueOf(llm.NewOpenAI),
			"NewProviderRegistry":  reflect.ValueOf(llm.NewProviderRegistry),
			"ParseModelName":       reflect.ValueOf(llm.ParseModelName),
			"BuildModelName":       reflect.ValueOf(llm.BuildModelName),
			"EstimateTokens":       reflect.ValueOf(llm.EstimateTokens),
			"CollectStreamContent": reflect.ValueOf(llm.CollectStreamContent),
			"StreamTextDelta":      reflect.ValueOf(llm.StreamTextDelta),
			"StreamReasoning":      reflect.ValueOf(llm.StreamReasoning),
			"StreamToolCall":       reflect.ValueOf(llm.StreamToolCall),
			"StreamDone":           reflect.ValueOf(llm.StreamDone),
			"StreamError":          reflect.ValueOf(llm.StreamError),
		},

		// ── catcode/ai/session ──
		"catcode/ai/session/session": {
			"Session":             reflect.ValueOf((*session.Session)(nil)),
			"Message":             reflect.ValueOf((*session.Message)(nil)),
			"New":                 reflect.ValueOf(session.New),
			"FromConversationRow": reflect.ValueOf(session.FromConversationRow),
		},

		// ── catcode/ai/compact ──
		"catcode/ai/compact/compact": {
			"CompactDecision":        reflect.ValueOf((*compact.CompactDecision)(nil)),
			"ShouldCompact":          reflect.ValueOf(compact.ShouldCompact),
			"BuildCompactionPrompt":  reflect.ValueOf(compact.BuildCompactionPrompt),
			"AutoCompactBufferRatio": reflect.ValueOf(compact.AutoCompactBufferRatio),
			"MinMessagesForCompact":  reflect.ValueOf(compact.MinMessagesForCompact),
		},

		// ── catcode/data/storage (部分类型，不含 WorkspaceDB) ──
		"catcode/data/storage/storage": {
			"ConversationRow":  reflect.ValueOf((*storage.ConversationRow)(nil)),
			"ConversationInfo": reflect.ValueOf((*storage.ConversationInfo)(nil)),
			"MessageRow":       reflect.ValueOf((*storage.MessageRow)(nil)),
			"MemoryEntry":      reflect.ValueOf((*storage.MemoryEntry)(nil)),
			"MemoryHeader":     reflect.ValueOf((*storage.MemoryHeader)(nil)),
			"MemoryService":    reflect.ValueOf((storage.MemoryService)(nil)),
			"MemoryScope":      reflect.ValueOf((*storage.MemoryScope)(nil)),
			"ScopeGlobal":      reflect.ValueOf(storage.ScopeGlobal),
			"ScopeWorkspace":   reflect.ValueOf(storage.ScopeWorkspace),
			"ErrorLogEntry":    reflect.ValueOf((*storage.ErrorLogEntry)(nil)),
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 辅助函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// callStringMethod 在 yaegi 解释器中调用返回 string 的方法
func callStringMethod(i *interp.Interpreter, expr string) (string, error) {
	v, err := i.Eval(expr + "()")
	if err != nil {
		return "", err
	}
	s, ok := v.Interface().(string)
	if !ok {
		return "", cerr.New("返回值类型错误，期望 string")
	}
	return s, nil
}

// hasMethod 检查 yaegi 解释器中是否存在指定方法
func hasMethod(i *interp.Interpreter, expr string) bool {
	v, err := i.Eval(expr)
	if err != nil {
		return false
	}
	return v.IsValid() && v.Kind() == reflect.Func
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// pluginWrapper — yaegi 解释的插件包装器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type pluginWrapper struct {
	name        string
	version     string
	infoType    string
	interp      *interp.Interpreter
	hasTools    bool
	hasRole     bool
	ctx         *PluginContext
	toolsFunc   reflect.Value
	roleDefFunc reflect.Value
}

func (w *pluginWrapper) Name() string    { return w.name }
func (w *pluginWrapper) Version() string { return w.version }

// Tools 调用插件的 Tools(bus) 方法并返回工具列表
func (w *pluginWrapper) Tools(bus event.EventBus) (tools []*tool.Tool) {
	if !w.hasTools || !w.toolsFunc.IsValid() {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[plugin] %s.Tools() panic: %v\n", w.name, r)
			tools = nil
		}
	}()

	results := w.toolsFunc.Call([]reflect.Value{reflect.ValueOf(bus)})
	if len(results) != 1 {
		return nil
	}
	v := results[0]
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return nil
	}
	tools = make([]*tool.Tool, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if elem.Kind() == reflect.Ptr || elem.Kind() == reflect.Interface {
			if t, ok := elem.Interface().(*tool.Tool); ok {
				tools = append(tools, t)
			}
		}
	}
	return tools
}

// RoleDef 调用插件的 RoleDef() 方法并返回角色定义
func (w *pluginWrapper) RoleDef() (def role.RoleDef) {
	if !w.hasRole || !w.roleDefFunc.IsValid() {
		return role.RoleDef{}
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[plugin] %s.RoleDef() panic: %v\n", w.name, r)
			def = role.RoleDef{}
		}
	}()

	results := w.roleDefFunc.Call(nil)
	if len(results) != 1 {
		return role.RoleDef{}
	}
	v := results[0]
	if !v.IsValid() {
		return role.RoleDef{}
	}
	if v.Kind() == reflect.Ptr {
		if def, ok := v.Interface().(*role.RoleDef); ok {
			return *def
		}
	}
	if def, ok := v.Interface().(role.RoleDef); ok {
		return def
	}
	return role.RoleDef{}
}