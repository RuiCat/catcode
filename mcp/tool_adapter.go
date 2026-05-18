package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cerr "catcode/core/errors"

	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工具适配器 — MCP Tool → catcode Tool
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AdaptTool 将 MCP 工具适配为 catcode Tool
func AdaptTool(mcpTool ToolDef, serverName string, client *Client) *tool.Tool {
	toolName := fmt.Sprintf("mcp__%s__%s", sanitizeName(serverName), sanitizeName(mcpTool.Name))

	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        toolName,
			Description: fmt.Sprintf("[MCP:%s] %s", serverName, mcpTool.Description),
			Parameters:  convertInputSchema(mcpTool.InputSchema),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			result, err := client.CallTool(mcpTool.Name, args)
			if err != nil {
				return "", cerr.Wrapf(err, "MCP %s/%s", serverName, mcpTool.Name)
			}
			return formatToolResult(result), nil
		},
	}
}

// convertInputSchema 将 MCP JSON Schema 转为 catcode Schema
func convertInputSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return tool.MustMarshalSchema(tool.Schema{Type: "object", Properties: map[string]tool.Property{}})
	}
	return raw
}

// formatToolResult 格式化工具执行结果
func formatToolResult(result *CallToolResult) string {
	if result == nil {
		return ""
	}
	var sb strings.Builder
	for _, item := range result.Content {
		if item.Type == "text" {
			sb.WriteString(item.Text)
		} else {
			sb.WriteString(fmt.Sprintf("[%s: %s]", item.Type, item.Data))
		}
	}
	return sb.String()
}

// sanitizeName 清理名称中的特殊字符
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return strings.ToLower(name)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MCP 管理器 — 多服务器连接管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ServerConfig MCP 服务器配置
type ServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "http"
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Enabled   bool              `json:"enabled"`
}

// Manager MCP 多服务器连接管理器
type Manager struct {
	servers map[string]*Client
	tools   []*tool.Tool
}

// NewManager 创建 MCP 管理器
func NewManager() *Manager {
	return &Manager{servers: make(map[string]*Client)}
}

// ConnectServer 连接 MCP 服务器并注册工具
func (m *Manager) ConnectServer(cfg ServerConfig) ([]*tool.Tool, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	var transport Transport
	var err error

	switch cfg.Transport {
	case "stdio":
		transport, err = NewStdioTransport(cfg.Command, cfg.Args, cfg.Env)
	case "http", "sse":
		transport, err = NewHTTPTransport(cfg.URL, cfg.Env)
	default:
		return nil, cerr.Newf("mcp: 不支持的传输类型: %s", cfg.Transport)
	}

	if err != nil {
		return nil, cerr.Wrapf(err, "mcp: 创建传输失败 [%s]", cfg.Name)
	}

	client := NewClient(transport)
	_, err = client.Initialize(context.Background()) // 初始化阶段无上层 context，使用 Background
	if err != nil {
		transport.Close()
		return nil, cerr.Wrapf(err, "mcp: 初始化失败 [%s]", cfg.Name)
	}

	mcpTools, err := client.ListTools()
	if err != nil {
		client.Close()
		return nil, cerr.Wrapf(err, "mcp: 获取工具列表失败 [%s]", cfg.Name)
	}

	var tools []*tool.Tool
	for _, mt := range mcpTools {
		t := AdaptTool(mt, cfg.Name, client)
		tools = append(tools, t)
	}

	m.servers[cfg.Name] = client
	m.tools = append(m.tools, tools...)

	return tools, nil
}

// DisconnectAll 断开所有服务器
func (m *Manager) DisconnectAll() {
	for name, client := range m.servers {
		client.Close()
		delete(m.servers, name)
	}
	m.tools = nil
}

// ServerCount 返回已连接服务器数
func (m *Manager) ServerCount() int {
	return len(m.servers)
}

// ToolCount 返回已注册工具数
func (m *Manager) ToolCount() int {
	return len(m.tools)
}
