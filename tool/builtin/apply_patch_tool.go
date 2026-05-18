package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cerr "catcode/core/errors"
	"catcode/tool"
)

type patchItem struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func ApplyPatchTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "apply_patch",
			Description: "批量应用文件编辑补丁。接受 JSON 数组格式的补丁列表，每个补丁包含文件路径和搜索替换对。所有补丁原子性应用：任一失败则回滚全部已应用的更改。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"patches": {Type: "string", Description: "JSON 数组，每项含 path/old_string/new_string。例如: [{\"path\":\"a.go\",\"old_string\":\"foo\",\"new_string\":\"bar\"}]"},
				},
				Required: []string{"patches"},
			}),
		},
		Call: applyPatchCall,
	}
}

func applyPatchCall(ctx *tool.Context, args map[string]any) (string, error) {
	patchesRaw, _ := args["patches"].(string)
	if patchesRaw == "" {
		return "", cerr.New("apply_patch: patches 参数不能为空")
	}

	var patches []patchItem
	if err := json.Unmarshal([]byte(patchesRaw), &patches); err != nil {
		return "", cerr.Wrap(err, "apply_patch: 解析 patches JSON 失败")
	}

	if len(patches) == 0 {
		return "没有需要应用的补丁", nil
	}

	backups := make(map[string][]byte)

	for i, p := range patches {
		path := p.Path
		if !filepath.IsAbs(path) && ctx.WorkDir != "" {
			path = filepath.Join(ctx.WorkDir, path)
		}
		cleanPath := filepath.Clean(path)
		// 安全检查：检查每个路径段是否为 ".." 而非简单子串匹配
		safe := true
		for _, seg := range strings.Split(cleanPath, string(filepath.Separator)) {
			if seg == ".." {
				safe = false
				break
			}
		}
		if !safe {
			rollbackPatches(backups)
			return "", cerr.Newf("apply_patch: 不安全的路径: %s，已回滚全部更改", p.Path)
		}

		_, alreadyBackedUp := backups[cleanPath]

		data, readErr := os.ReadFile(cleanPath)
		fileExists := readErr == nil

		if readErr != nil && !os.IsNotExist(readErr) {
			rollbackPatches(backups)
			return "", cerr.Wrapf(readErr, "apply_patch: 读取文件 %s 失败，已回滚全部更改", cleanPath)
		}

		if fileExists && strings.Contains(string(data), "\x00") {
			rollbackPatches(backups)
			return "", cerr.Newf("apply_patch: 文件 %s 是二进制文件，无法应用补丁，已回滚全部更改", cleanPath)
		}

		if !alreadyBackedUp {
			if fileExists {
				backups[cleanPath] = data
			} else {
				backups[cleanPath] = nil
			}
		}

		if !fileExists {
			if p.OldString == "" {
				dir := filepath.Dir(cleanPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					rollbackPatches(backups)
					return "", cerr.Wrapf(err, "apply_patch: 创建目录 %s 失败，已回滚全部更改", dir)
				}
				if err := os.WriteFile(cleanPath, []byte(p.NewString), 0644); err != nil {
					rollbackPatches(backups)
					return "", cerr.Wrapf(err, "apply_patch: 写入文件 %s 失败，已回滚全部更改", cleanPath)
				}
				continue
			}
			rollbackPatches(backups)
			return "", cerr.Newf("apply_patch: 文件 %s 不存在，无法匹配 old_string，已回滚全部更改", cleanPath)
		}

		if p.OldString == "" {
			rollbackPatches(backups)
			return "", cerr.Newf("apply_patch: old_string 不能为空（文件 %s 已存在），已回滚全部更改", cleanPath)
		}

		content := string(data)
		oldStr := p.OldString
		newStr := p.NewString

		idx := strings.Index(content, oldStr)
		if idx == -1 {
			matchedIdx, matchedStr := fuzzyMatchEdit(content, oldStr)
			if matchedIdx == -1 {
				rollbackPatches(backups)
				return "", cerr.Newf("apply_patch: 补丁 %d 在文件 %s 中未找到 old_string，已回滚全部更改", i+1, cleanPath)
			}
			idx = matchedIdx
			oldStr = matchedStr
		}

		newContent := content[:idx] + newStr + content[idx+len(oldStr):]

		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			rollbackPatches(backups)
			return "", cerr.Wrapf(err, "apply_patch: 创建目录 %s 失败，已回滚全部更改", dir)
		}

		if err := os.WriteFile(cleanPath, []byte(newContent), 0644); err != nil {
			rollbackPatches(backups)
			return "", cerr.Wrapf(err, "apply_patch: 写入文件 %s 失败，已回滚全部更改", cleanPath)
		}
	}

	modifiedFiles := len(backups)
	return fmt.Sprintf("✓ 已成功应用 %d 个补丁，修改了 %d 个文件", len(patches), modifiedFiles), nil
}

func rollbackPatches(backups map[string][]byte) {
	for path, original := range backups {
		if original == nil {
			os.Remove(path)
		} else {
			os.WriteFile(path, original, 0644)
		}
	}
}
