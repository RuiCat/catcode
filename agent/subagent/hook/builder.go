package hook

import (
	"context"
	"reflect"
	"time"

	"catcode/agent/subagent"
	"catcode/ai/session"
	"catcode/core/errors"
)

// YaegiContextBuilder 将 Hook 脚本适配为 ContextBuilder 接口
type YaegiContextBuilder struct {
	engine         *HookEngine
	agentType      string
	defaultBuilder subagent.ContextBuilder
}

// NewYaegiContextBuilder 创建适配器
func NewYaegiContextBuilder(engine *HookEngine, agentType string, defaultBuilder subagent.ContextBuilder) *YaegiContextBuilder {
	return &YaegiContextBuilder{
		engine:         engine,
		agentType:      agentType,
		defaultBuilder: defaultBuilder,
	}
}

// Name 返回构建器名称
func (b *YaegiContextBuilder) Name() string {
	return "hook-" + b.agentType
}

// BuildContext 通过 yaegi 解释器执行 Hook 脚本
func (b *YaegiContextBuilder) BuildContext(ctx context.Context, sa *session.Session, input *subagent.ContextBuildInput) (*subagent.ContextBuildResult, error) {
	yaegiInterp := b.engine.GetHook(b.agentType)
	if yaegiInterp == nil {
		// 无 hook，回退到默认构建器
		if b.defaultBuilder != nil {
			return b.defaultBuilder.BuildContext(ctx, sa, input)
		}
		return nil, nil
	}

	// 设置超时
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 调用 before_context 钩子
	if fn, err := yaegiInterp.Eval("hooks.BeforeContext"); err == nil && fn.IsValid() {
		fn.Call([]reflect.Value{
			reflect.ValueOf(sa),
			reflect.ValueOf(input),
		})
	}

	// 调用 build_context 钩子
	if fn, err := yaegiInterp.Eval("hooks.BuildContext"); err == nil && fn.IsValid() {
		results := fn.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(sa),
			reflect.ValueOf(input),
		})
		if len(results) > 0 && results[0].IsValid() {
			if result, ok := results[0].Interface().(*ContextBuildResult); ok {
				return &subagent.ContextBuildResult{
					SystemPrompt:        result.SystemPrompt,
					MemoryIndex:         result.MemoryIndex,
					ExtraSystemMessages: result.ExtraSystemMessages,
				}, nil
			}
		}
	}

	return nil, errors.New("hook: BuildContext 未返回有效结果")
}
