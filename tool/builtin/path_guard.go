// Package builtin 提供工具间共享的路径安全验证函数
package builtin

import (
	"path/filepath"
	"strings"

	cerr "catcode/core/errors"
)

// ResolveAndCheckPath 解析路径并验证在工作区范围内
// rawPath: 用户提供的路径（相对或绝对）
// workDir: 工作区根目录
// 返回解析后的绝对路径，若超出工作区范围则返回错误
func ResolveAndCheckPath(rawPath, workDir string) (string, error) {
	if workDir == "" {
		return "", cerr.New("工作目录未设置，无法进行路径安全检查")
	}

	if !filepath.IsAbs(rawPath) {
		rawPath = filepath.Join(workDir, rawPath)
	}

	cleanPath := filepath.Clean(rawPath)

	// 解析符号链接获取真实路径
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		// 如果文件不存在（如 write 创建新文件），解析父目录
		parentDir := filepath.Dir(cleanPath)
		resolvedParent, err := filepath.EvalSymlinks(parentDir)
		if err != nil {
			return "", cerr.Wrap(err, "无法解析路径")
		}
		resolvedPath = filepath.Join(resolvedParent, filepath.Base(cleanPath))
	}

	// 重新解析以确保完整路径正确
	resolvedPath = filepath.Clean(resolvedPath)

	if !strings.HasPrefix(resolvedPath, workDir) {
		return "", cerr.Newf("路径超出工作区范围: %s", rawPath)
	}

	return resolvedPath, nil
}
