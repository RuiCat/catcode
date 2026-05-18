// Package storage — 记忆聚合服务
// MemoryService 基于 WorkspaceDB 的 memory 表，按 scope 字段区分全局和工作区记忆
// scope='global': 项目全局记忆（同一项目共享）
// scope='workspace': 工作区独立记忆
package storage

import (
	"fmt"
	"strings"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MemoryService — 统一记忆服务
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryScope 记忆范围
type MemoryScope string

const (
	ScopeGlobal    MemoryScope = "global"
	ScopeWorkspace MemoryScope = "workspace"
)

// MemorySelector 智能记忆选择函数类型
// 根据上下文筛选最相关的记忆，避免 import cycle（由上层注入 compact 实现）
type MemorySelector func(wdb WorkspaceDB, context string, maxResults int) ([]*MemoryEntry, error)

// MemoryService 统一记忆管理服务
// MemoryService 接口 — 记忆管理
type MemoryService interface {
	SetMemory(scope MemoryScope, key, content, description, memoryType, tags string, importance int) error
	GetMemory(scope MemoryScope, key string) (*MemoryEntry, error)
	DeleteMemory(scope MemoryScope, key string) error
	BuildIndex(contextHint string) string
	Search(query string, scope MemoryScope, limit int) []ScopedMemory
	ListHeaders(scope MemoryScope) []ScopeHeader
	SetMemorySelector(sel MemorySelector)
	InvalidateCache()
}

type memoryServiceImpl struct {
	workspace WorkspaceDB // 工作区数据库（含所有 scope 的记忆）

	// 内存缓存：避免每次请求都扫描 DB
	cachedIndex string // 缓存的索引文本
	cacheDirty  bool   // 缓存是否失效（写入新记忆后设为 true）

	// 智能记忆选择器（由上层注入以打破 import cycle）
	selector MemorySelector
}

// ScopedMemory 带范围的记忆条目
type ScopedMemory struct {
	Scope  MemoryScope
	Memory *MemoryEntry
}

// ScopeHeader 带范围的索引头部
type ScopeHeader struct {
	Scope   MemoryScope
	Headers []*MemoryHeader
}

// maxIndexPerScope 每个 scope 在索引中最多显示的条目数
const maxIndexPerScope = 25

// maxDescriptionLen 索引中 description 最大长度（字符）
const maxDescriptionLen = 80

// NewMemoryService 创建记忆服务
// workspace 可为 nil（未初始化工作区时）
func NewMemoryService(workspace WorkspaceDB) MemoryService {
	return &memoryServiceImpl{
		workspace: workspace,
	}
}

// SetMemory 统一写入记忆（根据 scope 路由）
func (ms *memoryServiceImpl) SetMemory(scope MemoryScope, key, content, description, memoryType, tags string, importance int) error {
	if ms.workspace == nil {
		return cerr.New("storage: 工作区数据库未初始化")
	}
	err := ms.workspace.SetMemory(string(scope), key, content, description, memoryType, tags, importance)
	if err == nil {
		ms.cacheDirty = true // 新记忆写入，缓存失效
	}
	return err
}

// GetMemory 统一获取记忆
func (ms *memoryServiceImpl) GetMemory(scope MemoryScope, key string) (*MemoryEntry, error) {
	if ms.workspace == nil {
		return nil, cerr.New("storage: 工作区数据库未初始化")
	}
	return ms.workspace.GetMemory(string(scope), key)
}

// DeleteMemory 统一删除记忆
func (ms *memoryServiceImpl) DeleteMemory(scope MemoryScope, key string) error {
	if ms.workspace == nil {
		return cerr.New("storage: 工作区数据库未初始化")
	}
	err := ms.workspace.DeleteMemory(string(scope), key)
	if err == nil {
		ms.cacheDirty = true // 记忆删除，缓存失效
	}
	return err
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 索引构建（核心功能）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BuildIndex 构建紧凑的记忆索引文本
// contextHint: 可选的上下文提示（如最近对话摘要），用于智能筛选相关记忆。
// 传入空字符串时，使用简单头部扫描（按重要性排序）。
func (ms *memoryServiceImpl) BuildIndex(contextHint string) string {
	if ms.workspace == nil {
		return ""
	}

	// 使用缓存（避免频繁 DB 扫描）
	if !ms.cacheDirty && ms.cachedIndex != "" {
		return ms.cachedIndex
	}

	var globalHeaders, workspaceHeaders []*MemoryHeader

	if contextHint != "" {
		// 智能选择：根据上下文相关性评分筛选最相关的记忆
		if ms.selector != nil {
			relevant, _ := ms.selector(ms.workspace, contextHint, 30)
			for _, m := range relevant {
				h := &MemoryHeader{
					Key:         m.Key,
					Description: m.Description,
					MemoryType:  m.MemoryType,
					Importance:  m.Importance,
				}
				if m.Scope == "global" {
					globalHeaders = append(globalHeaders, h)
				} else {
					workspaceHeaders = append(workspaceHeaders, h)
				}
			}
		}
	} else {
		// 简单扫描：按重要性排序
		globalHeaders, _ = ms.workspace.ScanMemoryHeaders("global")
		workspaceHeaders, _ = ms.workspace.ScanMemoryHeaders("workspace")
	}

	if len(globalHeaders) == 0 && len(workspaceHeaders) == 0 {
		ms.cachedIndex = ""
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[记忆索引]\n")

	// 全局记忆部分
	if len(globalHeaders) > 0 {
		sb.WriteString("🌐 项目全局记忆:\n")
		limit := maxIndexPerScope
		if len(globalHeaders) < limit {
			limit = len(globalHeaders)
		}
		for _, h := range globalHeaders[:limit] {
			sb.WriteString(formatIndexLine(h))
		}
	}

	// 工作区记忆部分
	if len(workspaceHeaders) > 0 {
		sb.WriteString("📁 工作区记忆:\n")
		limit := maxIndexPerScope
		if len(workspaceHeaders) < limit {
			limit = len(workspaceHeaders)
		}
		for _, h := range workspaceHeaders[:limit] {
			sb.WriteString(formatIndexLine(h))
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("使用 memory_get(scope, key) 获取完整记忆，memory_search(query) 搜索记忆\n")

	// 更新缓存
	ms.cachedIndex = sb.String()
	ms.cacheDirty = false

	return ms.cachedIndex
}

// formatIndexLine 格式化单条索引行
func formatIndexLine(h *MemoryHeader) string {
	desc := h.Description
	runes := []rune(desc)
	if len(runes) > maxDescriptionLen {
		desc = string(runes[:maxDescriptionLen]) + "…"
	}

	typeAbbr := typeEmoji(h.MemoryType)
	return fmt.Sprintf("  • %s | %s | %s ★%d\n",
		h.Key, desc, typeAbbr, h.Importance)
}

// typeEmoji 记忆类型表情
func typeEmoji(t string) string {
	switch t {
	case "user":
		return "👤"
	case "feedback":
		return "💬"
	case "project":
		return "📋"
	case "reference":
		return "📖"
	default:
		return t
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 搜索
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Search 搜索记忆
// scope=all 时搜索所有范围；否则只搜索指定 scope
func (ms *memoryServiceImpl) Search(query string, scope MemoryScope, limit int) []ScopedMemory {
	if ms.workspace == nil {
		return nil
	}

	scopeFilter := string(scope)
	if scope == "all" {
		scopeFilter = "" // 空字符串表示不按 scope 过滤
	}

	entries, err := ms.workspace.FindRelevantMemories(scopeFilter, query, limit)
	if err != nil {
		return nil
	}

	var results []ScopedMemory
	for _, e := range entries {
		results = append(results, ScopedMemory{Scope: MemoryScope(e.Scope), Memory: e})
		if len(results) >= limit {
			break
		}
	}
	return results
}

// ListHeaders 按范围列出记忆头部
func (ms *memoryServiceImpl) ListHeaders(scope MemoryScope) []ScopeHeader {
	if ms.workspace == nil {
		return nil
	}

	var result []ScopeHeader
	scopeFilter := string(scope)
	if scope == "all" {
		scopeFilter = ""
	}

	// 如果查全部，分别获取
	if scopeFilter == "" {
		for _, s := range []string{"global", "workspace"} {
			headers, _ := ms.workspace.ScanMemoryHeaders(s)
			result = append(result, ScopeHeader{Scope: MemoryScope(s), Headers: headers})
		}
	} else {
		headers, _ := ms.workspace.ScanMemoryHeaders(scopeFilter)
		result = append(result, ScopeHeader{Scope: MemoryScope(scopeFilter), Headers: headers})
	}

	return result
}

// InvalidateCache 强制失效缓存（供外部调用）
func (ms *memoryServiceImpl) InvalidateCache() {
	ms.cacheDirty = true
}

// SetMemorySelector 注入智能记忆选择器（打破 import cycle）
func (ms *memoryServiceImpl) SetMemorySelector(sel MemorySelector) {
	ms.selector = sel
}
