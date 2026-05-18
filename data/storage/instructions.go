package storage

import (
	"os"
	"path/filepath"
	"strings"
)

// InstructionFiles 项目指令文件集合
type InstructionFiles struct {
	AGENTS  string // AGENTS.md 内容
	CLAUDE  string // CLAUDE.md 内容
	Catcode string // .catcode/instructions.md 内容
	Root    string // 项目根目录（找到 .git 的目录）
}

// LoadInstructions 从工作目录读取指令文件
// 从 workDir 向上搜索 .git 确定项目根，然后读取所有支持的指令文件
func LoadInstructions(workDir string) *InstructionFiles {
	result := &InstructionFiles{}
	root := findProjectRoot(workDir)
	result.Root = root
	if root == "" {
		return result
	}
	result.AGENTS = readFileIfExists(filepath.Join(root, "AGENTS.md"), 8000)
	result.CLAUDE = readFileIfExists(filepath.Join(root, "CLAUDE.md"), 8000)
	result.Catcode = readFileIfExists(filepath.Join(root, ".catcode", "instructions.md"), 8000)
	return result
}

// IsEmpty 检查是否所有指令文件都为空
func (i *InstructionFiles) IsEmpty() bool {
	return i.AGENTS == "" && i.CLAUDE == "" && i.Catcode == ""
}

// FormatContext 格式化为 LLM 上下文消息（限制总长度，以 rune 计）
func (i *InstructionFiles) FormatContext(maxChars int) string {
	if maxChars <= 0 {
		maxChars = 4000
	}
	var sb strings.Builder
	sb.WriteString("[项目指令文件]\n")
	remaining := maxChars

	if i.AGENTS != "" {
		text := i.AGENTS
		runes := []rune(text)
		if len(runes) > remaining {
			text = string(runes[:remaining]) + "\n...(截断)"
		}
		sb.WriteString("📄 AGENTS.md:\n")
		sb.WriteString(text)
		sb.WriteString("\n---\n")
		remaining -= len(runes)
		if remaining <= 0 {
			return sb.String()
		}
	}
	if i.CLAUDE != "" {
		text := i.CLAUDE
		runes := []rune(text)
		if len(runes) > remaining {
			text = string(runes[:remaining]) + "\n...(截断)"
		}
		sb.WriteString("📄 CLAUDE.md:\n")
		sb.WriteString(text)
		sb.WriteString("\n---\n")
		remaining -= len(runes)
		if remaining <= 0 {
			return sb.String()
		}
	}
	if i.Catcode != "" {
		text := i.Catcode
		runes := []rune(text)
		if len(runes) > remaining {
			text = string(runes[:remaining]) + "\n...(截断)"
		}
		sb.WriteString("📄 .catcode/instructions.md:\n")
		sb.WriteString(text)
		sb.WriteString("\n---\n")
	}
	return sb.String()
}

// findProjectRoot 从给定目录向上搜索包含 .git 的目录
func findProjectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	current := abs
	for {
		gitPath := filepath.Join(current, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			// 到达文件系统根
			return abs // 返回原始目录作为fallback
		}
		current = parent
	}
}

// readFileIfExists 读取文件内容（如果存在），限制最大字符数（以 rune 计）
func readFileIfExists(path string, maxChars int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	runes := []rune(text)
	if len(runes) > maxChars {
		text = string(runes[:maxChars]) + "\n...(截断)"
	}
	return text
}
