// Package mcp 实现 MCP (Model Context Protocol) 客户端，支持通过 stdio 传输层连接和管理外部工具服务器。
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 传输层接口
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Transport MCP 传输层接口
type Transport interface {
	// Send 发送 JSON-RPC 请求/通知
	Send(msg *JSONRPCMessage) error
	// Receive 接收 JSON-RPC 响应/通知
	Receive() (*JSONRPCMessage, error)
	// Close 关闭传输
	Close() error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stdio 传输实现（子进程 stdin/stdout）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// StdioTransport 通过子进程 stdio 通信
type StdioTransport struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	stderrBuf *bytes.Buffer
	mu        sync.Mutex
}

// NewStdioTransport 创建 stdio 传输
func NewStdioTransport(command string, args []string, env map[string]string) (Transport, error) {
	cmd := exec.Command(command, args...)
	if env != nil {
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, cerr.Wrap(err, "mcp: stdin pipe 失败")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, cerr.Wrap(err, "mcp: stdout pipe 失败")
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	startDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[PANIC] mcp start goroutine: %v\n%s\n", r, debug.Stack())
			}
		}()
		startDone <- cmd.Start()
	}()

	select {
	case err := <-startDone:
		if err != nil {
			stdin.Close()
			if stderrBuf.Len() > 0 {
				return nil, cerr.Wrapf(err, "mcp: 子进程启动失败 (stderr: %s)", stderrBuf.String())
			}
			return nil, cerr.Wrap(err, "mcp: 子进程启动失败")
		}
	case <-time.After(30 * time.Second):
		stdin.Close()
		return nil, cerr.New("mcp: 子进程启动超时 (30s)")
	}

	return &StdioTransport{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewScanner(stdout),
		stderrBuf: &stderrBuf,
	}, nil
}

// Send 发送消息（写 stdin）
func (t *StdioTransport) Send(msg *JSONRPCMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = t.stdin.Write(data)
	return err
}

// Receive 接收消息（读 stdout）
func (t *StdioTransport) Receive() (*JSONRPCMessage, error) {
	if !t.stdout.Scan() {
		if err := t.stdout.Err(); err != nil {
			return nil, cerr.Wrap(err, "mcp: 读取失败")
		}
		return nil, io.EOF
	}

	var msg JSONRPCMessage
	if err := json.Unmarshal(t.stdout.Bytes(), &msg); err != nil {
		return nil, cerr.Wrap(err, "mcp: JSON 解析失败")
	}
	return &msg, nil
}

// Close 关闭传输（带超时强制终止）
func (t *StdioTransport) Close() error {
	t.stdin.Close()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[PANIC] mcp cmd.Wait goroutine: %v\n%s\n", r, debug.Stack())
			}
		}()
		done <- t.cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return cerr.Newf("mcp: 子进程退出码 %d: %s", exitErr.ExitCode(), t.stderrBuf.String())
			}
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		t.cmd.Process.Kill()
		<-done // 回收进程资源
		return cerr.New("mcp: 子进程未在5秒内退出，已强制终止")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 客户端
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Client MCP 客户端
type Client struct {
	transport    Transport
	serverInfo   ServerInfo
	capabilities ServerCapabilities
	reqID        atomic.Int64
	mu           sync.Mutex
	initialized  bool
}

// NewClient 创建 MCP 客户端
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

// Initialize 初始化 MCP 连接
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Roots: &RootsCapability{ListChanged: true},
		},
		ClientInfo: Implementation{
			Name:    "catcode",
			Version: "0.5.0",
		},
	}

	paramsJSON, _ := json.Marshal(params)
	id := c.nextID()
	resp, err := c.call(ctx, id, "initialize", paramsJSON)
	if err != nil {
		return nil, cerr.Wrap(err, "mcp: initialize 失败")
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, cerr.Wrap(err, "mcp: 解析 initialize 结果失败")
	}

	c.serverInfo = result.ServerInfo
	c.capabilities = result.Capabilities
	c.initialized = true

	// 发送 initialized 通知
	c.notify("notifications/initialized", nil)

	return &result, nil
}

// ListTools 获取服务器工具列表
func (c *Client) ListTools() ([]ToolDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.call(ctx, id, "tools/list", nil)
	if err != nil {
		return nil, cerr.Wrap(err, "mcp: tools/list 失败")
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, cerr.Wrap(err, "mcp: 解析工具列表失败")
	}
	return result.Tools, nil
}

// CallTool 调用 MCP 工具
func (c *Client) CallTool(name string, args map[string]any) (*CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	params := CallToolParams{Name: name, Arguments: args}
	paramsJSON, _ := json.Marshal(params)
	id := c.nextID()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := c.call(ctx, id, "tools/call", paramsJSON)
	if err != nil {
		return nil, cerr.Wrapf(err, "mcp: tools/call %s 失败", name)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, cerr.Wrap(err, "mcp: 解析工具结果失败")
	}
	return &result, nil
}

// call 发送请求并等待响应
func (c *Client) call(ctx context.Context, id int64, method string, params json.RawMessage) (*JSONRPCMessage, error) {
	req := &JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.transport.Send(req); err != nil {
		return nil, err
	}

	const maxRecvLoops = 100
	for i := 0; i < maxRecvLoops; i++ {
		select {
		case <-ctx.Done():
			return nil, cerr.Wrap(ctx.Err(), "mcp: 调用被取消")
		default:
		}
		resp, err := c.transport.Receive()
		if err != nil {
			return nil, err
		}
		if resp.ID == id {
			if resp.Error != nil {
				return nil, cerr.Newf("mcp: 服务器错误 %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp, nil
		}
		// 非匹配 ID 的消息（可能是通知），忽略
	}
	return nil, cerr.Newf("mcp: 超过最大接收循环次数 %d，未收到请求 %d 的响应", maxRecvLoops, id)
}

// notify 发送通知（无 ID，不等待响应）
func (c *Client) notify(method string, params json.RawMessage) error {
	msg := &JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.transport.Send(msg)
}

func (c *Client) nextID() int64 {
	return c.reqID.Add(1)
}

// Close 关闭客户端
func (c *Client) Close() error {
	return c.transport.Close()
}

// ServerName 返回服务器名称
func (c *Client) ServerName() string {
	return c.serverInfo.Name
}
