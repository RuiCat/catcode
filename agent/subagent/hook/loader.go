package hook

import (
	"os"
	"path/filepath"

	"catcode/core/errors"
	"github.com/traefik/yaegi/interp"
)

// HookLoader 从磁盘加载 Hook 文件
type HookLoader struct {
	hooksDir string
}

// NewHookLoader 创建 Hook 加载器
// hooksDir 默认: ~/.catcode/hooks/
func NewHookLoader(hooksDir string) *HookLoader {
	return &HookLoader{hooksDir: hooksDir}
}

// Discover 发现可用的 Hook 文件
// 返回 map[agentType]filePath
func (l *HookLoader) Discover() (map[string]string, error) {
	if _, err := os.Stat(l.hooksDir); os.IsNotExist(err) {
		return nil, nil // 目录不存在，无 hook
	}

	entries, err := os.ReadDir(l.hooksDir)
	if err != nil {
		return nil, errors.Wrap(err, "hook: 读取 hook 目录失败")
	}

	hooks := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		agentType := entry.Name()[:len(entry.Name())-3] // 去掉 .go
		hooks[agentType] = filepath.Join(l.hooksDir, entry.Name())
	}
	return hooks, nil
}

// Load 加载并编译 Hook 文件
func (l *HookLoader) Load(filePath string) (*interp.Interpreter, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "hook: 读取文件失败")
	}

	i := interp.New(interp.Options{Unrestricted: false})
	policy := DefaultSandboxPolicy()
	policy.ApplyTo(i)
	registerSymbols(i)

	if _, err := i.Eval(string(src)); err != nil {
		return nil, errors.Wrap(err, "hook: 编译失败")
	}
	return i, nil
}
