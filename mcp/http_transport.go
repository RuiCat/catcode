package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// HTTP/SSE 传输实现
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// HTTPTransport 通过 HTTP+SSE 通信
// POST /message 发送请求，GET /sse 接收事件
type HTTPTransport struct {
	baseURL   string
	headers   map[string]string
	client    *http.Client
	sessionID string

	// SSE 接收
	sseResp   *http.Response
	sseReader *bufio.Reader
	msgCh     chan *JSONRPCMessage
	done      chan struct{}
	mu        sync.Mutex
	closed    bool
}

// NewHTTPTransport 创建 HTTP+SSE 传输
func NewHTTPTransport(baseURL string, headers map[string]string) (Transport, error) {
	t := &HTTPTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		client:  &http.Client{},
		msgCh:   make(chan *JSONRPCMessage, 32),
		done:    make(chan struct{}),
	}

	// 连接 SSE 端点
	if err := t.connectSSE(); err != nil {
		return nil, cerr.Wrap(err, "mcp: SSE 连接失败")
	}

	return t, nil
}

// connectSSE 连接 SSE 端点获取 session
func (t *HTTPTransport) connectSSE() error {
	req, err := http.NewRequest("GET", t.baseURL+"/sse", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return cerr.Wrap(err, "mcp: SSE 请求失败")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return cerr.Newf("mcp: SSE 返回 %d", resp.StatusCode)
	}

	// 读取初始 endpoint 事件获取 session ID
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			resp.Body.Close()
			return cerr.Wrap(err, "mcp: 读取 SSE 初始事件失败")
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			// 可能是 endpoint 事件，包含 session ID
			if strings.Contains(data, "session") || strings.Contains(data, "message") {
				// 提取 session ID（通常格式: /message?sessionId=xxx）
				t.sessionID = extractSessionID(data)
			}
		}
		if line == "" {
			break // 空行表示事件结束
		}
	}

	t.sseResp = resp
	t.sseReader = reader

	// 启动 goroutine 持续读取 SSE 事件
	go t.readSSEEvents()

	return nil
}

// extractSessionID 从 SSE endpoint 数据中提取 session ID
func extractSessionID(data string) string {
	// 格式: /message?sessionId=abc123
	if idx := strings.Index(data, "sessionId="); idx >= 0 {
		sid := data[idx+10:]
		if end := strings.IndexAny(sid, " \t\n\r&"); end > 0 {
			return sid[:end]
		}
		return sid
	}
	return ""
}

// readSSEEvents 持续读取 SSE 事件流
func (t *HTTPTransport) readSSEEvents() {
	defer close(t.msgCh)

	var eventData strings.Builder
	for {
		select {
		case <-t.done:
			return
		default:
		}

		line, err := t.sseReader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "data: ") {
			eventData.WriteString(strings.TrimPrefix(line, "data: "))
		}
		if line == "" && eventData.Len() > 0 {
			// 完整事件
			var msg JSONRPCMessage
			if err := json.Unmarshal([]byte(eventData.String()), &msg); err == nil {
				select {
				case t.msgCh <- &msg:
				case <-t.done:
					return
				}
			}
			eventData.Reset()
		}
	}
}

// Send 发送 JSON-RPC 请求（HTTP POST）
func (t *HTTPTransport) Send(msg *JSONRPCMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return cerr.New("mcp: 传输已关闭")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	url := t.baseURL + "/message"
	if t.sessionID != "" {
		url += "?sessionId=" + t.sessionID
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return cerr.Wrap(err, "mcp: POST 失败")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return cerr.Newf("mcp: POST 返回 %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Receive 接收 JSON-RPC 响应/通知（从 SSE 通道）
func (t *HTTPTransport) Receive() (*JSONRPCMessage, error) {
	msg, ok := <-t.msgCh
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

// Close 关闭传输
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)

	if t.sseResp != nil {
		t.sseResp.Body.Close()
	}
	return nil
}
