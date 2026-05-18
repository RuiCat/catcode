package builtin

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	cerr "catcode/core/errors"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// WebFetch — URL 内容获取工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// WebFetchTool 创建 URL 内容获取工具
// 支持将 HTML 转换为文本/markdown 格式，或返回原始 HTML。
// 内置 SSRF 防护（禁止重定向到内网地址）和内容大小限制。
func WebFetchTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "webfetch",
			Description: "从指定 URL 获取内容。支持将 HTML 转换为文本/markdown 格式，或返回原始 HTML。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"url":    {Type: "string", Description: "要获取的 URL"},
					"format": {Type: "string", Description: "返回格式：markdown（默认）、text 或 html", Enum: []string{"markdown", "text", "html"}},
				},
				Required: []string{"url"},
			}),
		},
		Call: webfetchCall,
	}
}

func webfetchCall(ctx *tool.Context, args map[string]any) (string, error) {
	rawURL, _ := args["url"].(string)
	format, _ := args["format"].(string)
	if format == "" {
		format = "markdown"
	}

	// 验证 URL 格式
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", cerr.Newf("webfetch: URL 必须以 http:// 或 https:// 开头: %s", rawURL)
	}
	if _, err := url.Parse(rawURL); err != nil {
		return "", cerr.Wrapf(err, "webfetch: 无法解析 URL: %s", rawURL)
	}

	// 构建 HTTP 客户端：30s 超时，限制重定向次数并检查目标是否为内网地址
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return cerr.Newf("webfetch: 重定向次数超过上限 (5)")
			}
			host := req.URL.Hostname()
			if host == "" {
				return cerr.Newf("webfetch: 重定向目标主机为空")
			}
			if isPrivateHost(host) {
				return cerr.Newf("webfetch: 禁止重定向到内网地址: %s", host)
			}
			return nil
		},
	}

	// 发起请求
	req, err := http.NewRequestWithContext(
		ctx.Ctx,
		http.MethodGet,
		rawURL,
		nil,
	)
	if err != nil {
		return "", cerr.Wrapf(err, "webfetch: 创建请求失败: %s", rawURL)
	}
	req.Header.Set("User-Agent", "catcode/0.9.2")

	// SSRF protection: check initial URL's host as well (not just redirects)
	host := req.URL.Hostname()
	if host == "" {
		return "", cerr.Newf("webfetch: 无法解析主机名: %s", rawURL)
	}
	if isPrivateHost(host) {
		return "", cerr.Newf("webfetch: 禁止访问内网地址: %s", host)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", cerr.Wrapf(err, "webfetch: 请求失败: %s", rawURL)
	}
	defer resp.Body.Close()

	// 检查 Content-Type 是否为文本类型
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !isTextContentType(contentType) {
		return "", cerr.Newf("webfetch: 不支持的内容类型: %s（仅支持 text/html、text/plain、application/json、application/xml）", contentType)
	}

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return "", cerr.Newf("webfetch: 请求返回非 200 状态码: %d", resp.StatusCode)
	}

	// 读取响应体，限制最大 500KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
	if err != nil {
		return "", cerr.Wrap(err, "webfetch: 读取响应体失败")
	}
	bodyStr := string(body)

	// 根据 format 处理内容
	var processed string
	switch format {
	case "html":
		// 原始 HTML 限制 100KB（安全截断，避免切断多字节 UTF-8 字符）
		if len(bodyStr) > 100*1024 {
			runes := []rune(bodyStr)
			if len(runes) > 100*1024 {
				bodyStr = string(runes[:100*1024])
			}
		}
		processed = bodyStr
	case "markdown":
		processed = htmlToMarkdown(bodyStr)
		processed = decodeHTMLEntities(processed)
		processed = collapseBlankLines(processed)
	case "text":
		processed = htmlToText(bodyStr)
		processed = decodeHTMLEntities(processed)
		processed = collapseBlankLines(processed)
	default:
		return "", cerr.Newf("webfetch: 不支持的格式: %s", format)
	}

	return fmt.Sprintf("URL: %s\n状态码: %d\n内容类型: %s\n内容长度: %d 字节\n\n%s",
		rawURL, resp.StatusCode, contentType, len(processed), processed), nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// HTML 转换辅助函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

var (
	// 用于移除 script/style/noscript 标签及其内容
	reScript   = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	reNoscript = regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript>`)
	// 块级标签 → 换行
	reBlockTag = regexp.MustCompile(`(?i)<(br\s*/?|p|div|li|tr)\b[^>]*>`)
	// 标题标签 <h1>~<h6>
	reH1 = regexp.MustCompile(`(?i)<h1\b[^>]*>(.*?)</h1>`)
	reH2 = regexp.MustCompile(`(?i)<h2\b[^>]*>(.*?)</h2>`)
	reH3 = regexp.MustCompile(`(?i)<h3\b[^>]*>(.*?)</h3>`)
	reH4 = regexp.MustCompile(`(?i)<h4\b[^>]*>(.*?)</h4>`)
	reH5 = regexp.MustCompile(`(?i)<h5\b[^>]*>(.*?)</h5>`)
	reH6 = regexp.MustCompile(`(?i)<h6\b[^>]*>(.*?)</h6>`)
	// 剩余 HTML 标签
	reHTMLTag = regexp.MustCompile(`<[^>]*>`)
	// 链接标签 <a href="url">text</a>
	reLinkTag = regexp.MustCompile(`(?i)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>\s*(.*?)\s*</a>`)
	// 图片标签 <img src="url" ...>
	reImgTag = regexp.MustCompile(`(?i)<img\b[^>]*src\s*=\s*["']([^"']+)["'][^>]*>`)
	// 连续空行（3 个及以上换行 → 2 个）
	reMultiBlank = regexp.MustCompile(`\n{3,}`)
)

// htmlToText 将 HTML 转换为纯文本
// 1. 移除 <script>、<style>、<noscript> 标签及其内容
// 2. 将块级标签 <br>、<p>、<div>、<li>、<tr> 替换为换行
// 3. 将标题 <h1>~<h6> 转为 "# " 前缀（级数 = 井号数）
// 4. 移除所有剩余 HTML 标签
func htmlToText(html string) string {
	// Step 1: 移除 script/style/noscript 及其内容
	s := reScript.ReplaceAllString(html, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reNoscript.ReplaceAllString(s, "")
	// Step 2: 块级标签 → 换行
	s = reBlockTag.ReplaceAllString(s, "\n")
	// Step 3: 标题转换
	s = reH1.ReplaceAllString(s, "\n# $1\n")
	s = reH2.ReplaceAllString(s, "\n## $1\n")
	s = reH3.ReplaceAllString(s, "\n### $1\n")
	s = reH4.ReplaceAllString(s, "\n#### $1\n")
	s = reH5.ReplaceAllString(s, "\n##### $1\n")
	s = reH6.ReplaceAllString(s, "\n###### $1\n")
	// Step 4: 移除剩余 HTML 标签
	s = reHTMLTag.ReplaceAllString(s, "")
	return s
}

// htmlToMarkdown 将 HTML 转换为 Markdown 格式
// 在 htmlToText 之前先将链接和图片转换为 Markdown 语法：
//   - <a href="url">text</a> → [text](url)
//   - <img src="url" ...> → ![image](url)
func htmlToMarkdown(html string) string {
	// Step 1: 转换链接
	s := reLinkTag.ReplaceAllString(html, "[$2]($1)")
	// Step 2: 转换图片
	s = reImgTag.ReplaceAllString(s, "![image]($1)")
	// Step 3: 剩余标签用 htmlToText 处理
	s = htmlToText(s)
	return s
}

// decodeHTMLEntities 解码基本 HTML 实体
func decodeHTMLEntities(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&nbsp;", " ",
	)
	return replacer.Replace(s)
}

// collapseBlankLines 折叠连续空行（最多保留 2 个连续换行）
func collapseBlankLines(s string) string {
	return reMultiBlank.ReplaceAllString(s, "\n\n")
}

// isTextContentType 检查 Content-Type 是否为文本类型
func isTextContentType(ct string) bool {
	return strings.HasPrefix(ct, "text/html") ||
		strings.HasPrefix(ct, "text/plain") ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 安全辅助函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// isPrivateHost 检查主机是否为内网地址（含主机名解析后的 IP 检查）
func isPrivateHost(host string) bool {
	// 分离主机和端口
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	// 检查是否为 IPv6 环回地址
	if hostname == "::1" {
		return true
	}

	// 尝试直接解析为 IP
	ip := net.ParseIP(hostname)
	if ip != nil {
		return isPrivateIP(ip)
	}

	// DNS 解析主机名
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// 解析失败保守处理：返回 false（允许继续）
		return false
	}
	for _, resolvedIP := range ips {
		if isPrivateIP(resolvedIP) {
			return true
		}
	}
	return false
}

// isPrivateIP 检查 IP 是否属于私有地址范围
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// IPv4 私有地址范围
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 127: // 127.0.0.0/8
			return true
		case ip4[0] == 10: // 10.0.0.0/8
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31: // 172.16.0.0/12
			return true
		case ip4[0] == 192 && ip4[1] == 168: // 192.168.0.0/16
			return true
		case ip4[0] == 169 && ip4[1] == 254: // 169.254.0.0/16 (AWS/cloud metadata)
			return true
		}
		return false
	}
	// IPv6 环回地址
	return ip.IsLoopback()
}
