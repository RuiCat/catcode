package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cerr "catcode/core/errors"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// skill — 加载和列出 .catcode/skills/ 下的技能文件
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func SkillTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "skill",
			Description: "加载专业化技能，将技能指令注入到上下文中。技能是 .catcode/skills/ 目录下的 Markdown 文件（含 YAML frontmatter）。不传 name 参数时列出所有可用技能。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"name": {Type: "string", Description: "要加载的技能名称（不含扩展名）。留空则列出所有可用技能。"},
				},
			}),
		},
		Call: skillCall,
	}
}

func skillCall(ctx *tool.Context, args map[string]any) (string, error) {
	skillsDir := filepath.Join(ctx.WorkDir, ".catcode", "skills")
	name, ok := args["name"].(string)
	if !ok {
		return "", cerr.Newf("skill: name 参数类型错误")
	}
	if name == "" {
		return listSkills(skillsDir)
	}
	return loadSkill(skillsDir, name)
}

func listSkills(skillsDir string) (string, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "未找到 .catcode/skills/ 目录，暂无可用技能。可在该目录下创建 Markdown 文件来定义技能。", nil
		}
		return "", cerr.Wrap(err, "读取技能目录失败")
	}

	var results []string
	var warnings []string
	seen := make(map[string]int)

	for i, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(skillsDir, entry.Name()))
		if err != nil {
			continue
		}
		fm, _, err := parseSkillFrontmatter(string(content))
		if err != nil || fm["name"] == "" && fm["description"] == "" {
			continue
		}
		skillName := fm["name"]
		if skillName == "" {
			skillName = strings.TrimSuffix(entry.Name(), ".md")
		}
		if prevIdx, exists := seen[skillName]; exists {
			warnings = append(warnings, fmt.Sprintf("警告: 技能名 \"%s\" 重复 (文件: %s, %s)",
				skillName, entries[prevIdx].Name(), entry.Name()))
		} else {
			seen[skillName] = i
		}
		results = append(results, fmt.Sprintf("- %s: %s", skillName, fm["description"]))
	}

	if len(results) == 0 {
		return "未找到任何技能文件。请在 .catcode/skills/ 目录下创建含 YAML frontmatter 的 .md 文件。", nil
	}

	var sb strings.Builder
	sb.WriteString("可用技能:\n")
	for _, r := range results {
		sb.WriteString(r)
		sb.WriteByte('\n')
	}
	if len(warnings) > 0 {
		sb.WriteByte('\n')
		for _, w := range warnings {
			sb.WriteString(w)
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func loadSkill(skillsDir, name string) (string, error) {
	skillPath := filepath.Join(skillsDir, name+".md")
	if _, err := os.Stat(skillPath); err != nil {
		entries, readErr := os.ReadDir(skillsDir)
		if readErr != nil {
			return "", cerr.Newf("skill: 未找到技能 \"%s\"", name)
		}
		skillPath = ""
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.TrimSuffix(entry.Name(), ".md") == name {
				skillPath = filepath.Join(skillsDir, entry.Name())
				break
			}
		}
		if skillPath == "" {
			return "", cerr.Newf("skill: 未找到技能 \"%s\"，使用 skill 工具不传参数可查看可用技能列表", name)
		}
	}

	content, err := os.ReadFile(skillPath)
	if err != nil {
		return "", cerr.Wrap(err, "读取技能文件失败")
	}

	fm, body, err := parseSkillFrontmatter(string(content))
	if err != nil {
		return "", cerr.Wrap(err, "解析技能文件失败")
	}

	skillName := fm["name"]
	if skillName == "" {
		skillName = name
	}

	return fmt.Sprintf("[技能已加载: %s]\n\n%s", skillName, body), nil
}

func parseSkillFrontmatter(content string) (map[string]string, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	if len(lines) < 2 || lines[0] != "---" {
		return nil, "", cerr.New("skill: 缺少 YAML frontmatter（文件应以 --- 开头）")
	}

	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return nil, "", cerr.New("skill: YAML frontmatter 未正确闭合（缺少结束的 ---）")
	}

	result := make(map[string]string)
	for _, line := range lines[1:closeIdx] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		result[key] = value
	}

	body := ""
	if closeIdx+1 < len(lines) {
		body = strings.Join(lines[closeIdx+1:], "\n")
	}

	return result, body, nil
}
