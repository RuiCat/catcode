package builtin

import (
	"bytes"
	cerr "catcode/core/errors"
	"catcode/tool"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Bash — Shell 命令执行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BashTool 创建并返回 bash 命令执行工具实例，支持超时控制、环境变量安全过滤和守卫规则检查。
func BashTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "bash",
			Description: "执行 Shell 命令。默认超时 60 秒，返回 stdout+stderr 输出。危险命令需用户确认。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"command": {Type: "string", Description: "要执行的命令"},
					"workdir": {Type: "string", Description: "工作目录 (默认当前目录)"},
					"timeout": {Type: "integer", Description: "超时秒数 (默认60，最大300)"},
				},
				Required: []string{"command"},
			}),
		},
		Call: bashCall,
	}
}

// allowedShells 允许的 shell 路径白名单
var allowedShells = map[string]bool{
	"/bin/bash":     true,
	"/usr/bin/bash": true,
	"/bin/sh":       true,
	"/usr/bin/sh":   true,
	"/bin/zsh":      true,
	"/usr/bin/zsh":  true,
}

func resolveShell() string {
	// CATCODE_SHELL 优先
	if cs := os.Getenv("CATCODE_SHELL"); cs != "" {
		if allowedShells[cs] {
			return cs
		}
		// 也允许通过 LookPath 找到的 shell
		if absPath, err := exec.LookPath(cs); err == nil && allowedShells[absPath] {
			return absPath
		}
	}
	// SHELL 环境变量（需白名单验证）
	if s := os.Getenv("SHELL"); s != "" && allowedShells[s] {
		return s
	}
	// 自动检测
	for _, candidate := range []string{"bash", "zsh", "sh"} {
		if p, err := exec.LookPath(candidate); err == nil && allowedShells[p] {
			return p
		}
	}
	return "sh"
}

func bashCall(ctx *tool.Context, args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", cerr.Newf("bash: command 参数必填")
	}

	// 安全预检查
	if result := guardCheck(command); result.Denied {
		return fmt.Sprintf("🛡️ 命令被守卫拦截 [%s]: %s\n建议: %s",
			result.Level, result.Reason, result.Suggestion), nil
	}

	// LLM 级语义审查（如果上下文提供了审查器）
	if ctx.GuardReviewer != nil {
		approved, reason := ctx.GuardReviewer(command)
		if !approved {
			return fmt.Sprintf("🛡️ 命令被守卫子智能体拦截: %s\n命令: %s", reason, command), nil
		}
	}

	timeoutSec := 60
	if v, ok := args["timeout"].(float64); ok {
		timeoutSec = int(v)
		if timeoutSec > 300 {
			timeoutSec = 300
		}
	}
	workDir := ctx.WorkDir
	if workDir == "" {
		workDir = "."
	}

	// 选择 shell：使用白名单验证的 resolveShell()
	shell := resolveShell()
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = workDir
	cmd.Env = filterEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 启动命令
	if err := cmd.Start(); err != nil {
		return "", cerr.Wrap(err, "bash: 启动命令失败")
	}

	// 超时控制
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "panic in bash cmd.Wait goroutine: %v\n%s", r, debug.Stack())
			}
		}()
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		output := stdout.String()
		if stderr.Len() > 0 {
			output += "\n[stderr]\n" + stderr.String()
		}
		if err != nil {
			output += fmt.Sprintf("\n[exit code: %d]", cmd.ProcessState.ExitCode())
		}
		if output == "" {
			output = "[命令执行成功，无输出]"
		}
		return output, nil
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		cmd.Process.Kill()
		return stdout.String() + fmt.Sprintf("\n[超时: 命令在 %d 秒后被终止]", timeoutSec), nil
	}
}

// filterEnv 返回经过安全过滤的环境变量列表供子进程使用。
// 安全策略：
//  1. 仅允许已知安全的环境变量名（白名单）或以受信前缀开头的变量
//  2. 明确阻止变量名中包含敏感关键词（API_KEY, TOKEN, SECRET 等）的环境变量
//
// 目的：防止 LLM 通过 shell 命令（如 env | curl）窃取宿主机密凭据。
func filterEnv() []string {
	// 精确白名单：允许传递的安全环境变量
	exactAllow := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LANG": true,
		"PWD": true, "SHELL": true, "TERM": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
		"COLORTERM": true, "DISPLAY": true, "WAYLAND_DISPLAY": true,
		"EDITOR": true, "VISUAL": true, "PAGER": true, "BROWSER": true,
	}
	// 前缀白名单：以这些前缀开头的变量名被允许
	prefixAllow := []string{"XDG_"}

	// 黑名单关键词：变量名中不应包含这些词（大小写不敏感）
	blockKeywords := []string{"API_KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH"}

	var filtered []string
	for _, env := range os.Environ() {
		eq := strings.IndexByte(env, '=')
		if eq < 0 {
			continue
		}
		name := env[:eq]

		// 阻止变量名包含敏感关键词
		upper := strings.ToUpper(name)
		blocked := false
		for _, kw := range blockKeywords {
			if strings.Contains(upper, kw) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}

		// 放行精确匹配
		if exactAllow[name] {
			filtered = append(filtered, env)
			continue
		}

		// 放行前缀匹配
		for _, prefix := range prefixAllow {
			if strings.HasPrefix(name, prefix) {
				filtered = append(filtered, env)
				break
			}
		}
	}
	return filtered
}
