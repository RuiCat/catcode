package builtin

import (
	cerr "catcode/core/errors"
	"catcode/data/storage"
	"catcode/tool"
	"fmt"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// memory_set — 创建或更新记忆
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemorySetTool 创建或更新记忆条目
func MemorySetTool(ms storage.MemoryService) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "memory_set",
			Description: "创建或更新记忆条目。全局记忆(scope=global)跨工作区共享，独立记忆(scope=workspace)仅当前工作区可见。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"scope":       {Type: "string", Description: "范围: global=跨工作区共享, workspace=仅当前工作区", Enum: []string{"global", "workspace"}},
					"key":         {Type: "string", Description: "记忆唯一标识（英文+连字符，如 go-style-guide）"},
					"content":     {Type: "string", Description: "记忆完整内容"},
					"description": {Type: "string", Description: "简短描述（用于索引显示，可选）"},
					"type":        {Type: "string", Description: "记忆类型: user=用户偏好, feedback=反馈经验, project=项目约定, reference=参考资料", Enum: []string{"user", "feedback", "project", "reference"}},
					"tags":        {Type: "string", Description: "逗号分隔标签（可选）"},
					"importance":  {Type: "integer", Description: "重要性 0-10，默认 5"},
				},
				Required: []string{"scope", "key", "content"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			scope, _ := args["scope"].(string)
			key, _ := args["key"].(string)
			content, _ := args["content"].(string)
			description, _ := args["description"].(string)
			memType, _ := args["type"].(string)
			tags, _ := args["tags"].(string)
			importance := 5
			if v, ok := args["importance"].(float64); ok {
				importance = int(v)
			}
			if memType == "" {
				memType = "project"
			}

			if err := ms.SetMemory(storage.MemoryScope(scope), key, content, description, memType, tags, importance); err != nil {
				return "", cerr.Wrap(err, "保存记忆失败")
			}
			return fmt.Sprintf("✓ 记忆已保存: %s/%s (%s)", scope, key, memType), nil
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// memory_get — 获取完整记忆
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryGetTool 根据 key 获取完整记忆内容
func MemoryGetTool(ms storage.MemoryService) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "memory_get",
			Description: "获取指定记忆的完整内容。从索引中选择需要的 key 后调用。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"scope": {Type: "string", Description: "记忆所在范围", Enum: []string{"global", "workspace"}},
					"key":   {Type: "string", Description: "记忆 key"},
				},
				Required: []string{"scope", "key"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			scope, _ := args["scope"].(string)
			key, _ := args["key"].(string)

			mem, err := ms.GetMemory(storage.MemoryScope(scope), key)
			if err != nil {
				return "", cerr.Wrap(err, "获取记忆失败")
			}

			return fmt.Sprintf("[记忆: %s/%s | %s | ★%d | 访问 %d次]\n%s",
				scope, mem.Key, mem.MemoryType, mem.Importance, mem.AccessCount, mem.Content), nil
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// memory_search — 搜索记忆
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemorySearchTool 搜索记忆（关键词匹配 content/description/tags）
func MemorySearchTool(ms storage.MemoryService) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "memory_search",
			Description: "搜索记忆（按关键词匹配内容、描述和标签）。返回匹配的完整内容。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"query": {Type: "string", Description: "搜索关键词"},
					"scope": {Type: "string", Description: "搜索范围，默认 all（全部）", Enum: []string{"global", "workspace", "all"}},
				},
				Required: []string{"query"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			scope, _ := args["scope"].(string)
			if scope == "" {
				scope = "all"
			}

			results := ms.Search(query, storage.MemoryScope(scope), 10)
			if len(results) == 0 {
				return "未找到匹配的记忆", nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("搜索 \"%s\" 找到 %d 条记忆:\n", query, len(results)))
			for _, sm := range results {
				sb.WriteString(fmt.Sprintf("\n[%s/%s | %s | ★%d]\n%s\n---\n",
					sm.Scope, sm.Memory.Key, sm.Memory.MemoryType, sm.Memory.Importance, sm.Memory.Content))
			}
			return sb.String(), nil
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// memory_list — 列出记忆索引
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryListTool 列出记忆索引（仅 header 级别）
func MemoryListTool(ms storage.MemoryService) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "memory_list",
			Description: "列出记忆索引（仅显示 key、描述、类型、重要性，不含完整内容）。用于查看所有可用记忆。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"scope": {Type: "string", Description: "列出范围，默认 all（全部）", Enum: []string{"global", "workspace", "all"}},
				},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			scope, _ := args["scope"].(string)
			if scope == "" {
				scope = "all"
			}

			headers := ms.ListHeaders(storage.MemoryScope(scope))
			if len(headers) == 0 {
				return "暂无记忆条目", nil
			}

			return ms.BuildIndex(""), nil // 复用索引构建方法
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// memory_delete — 删除记忆
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryDeleteTool 删除记忆条目
func MemoryDeleteTool(ms storage.MemoryService) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "memory_delete",
			Description: "删除记忆条目。删除后不可恢复。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"scope": {Type: "string", Description: "记忆所在范围", Enum: []string{"global", "workspace"}},
					"key":   {Type: "string", Description: "要删除的记忆 key"},
				},
				Required: []string{"scope", "key"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			scope, _ := args["scope"].(string)
			key, _ := args["key"].(string)

			if err := ms.DeleteMemory(storage.MemoryScope(scope), key); err != nil {
				return "", cerr.Wrap(err, "删除记忆失败")
			}
			return fmt.Sprintf("✓ 记忆已删除: %s/%s", scope, key), nil
		},
	}
}
