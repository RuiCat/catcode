package hook

import (
	"os"
	"sync"
	"time"

	"github.com/traefik/yaegi/interp"
)

// HookEngine 单例引擎，管理 Hook 的编译缓存和热重载
type HookEngine struct {
	mu           sync.RWMutex
	loader       *HookLoader
	interpreters map[string]*cachedHook // agentType → 编译缓存
}

type cachedHook struct {
	interp     *interp.Interpreter
	filePath   string
	mtime      time.Time
	compiledAt time.Time
}

// NewHookEngine 创建 Hook 引擎
func NewHookEngine(hooksDir string) *HookEngine {
	return &HookEngine{
		loader:       NewHookLoader(hooksDir),
		interpreters: make(map[string]*cachedHook),
	}
}

// LoadAll 加载所有发现的 Hook 文件
func (e *HookEngine) LoadAll() error {
	hooks, err := e.loader.Discover()
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for agentType, filePath := range hooks {
		interp, err := e.loader.Load(filePath)
		if err != nil {
			continue // 跳过编译失败的 hook
		}
		info, _ := os.Stat(filePath)
		e.interpreters[agentType] = &cachedHook{
			interp:     interp,
			filePath:   filePath,
			mtime:      info.ModTime(),
			compiledAt: time.Now(),
		}
	}
	return nil
}

// GetHook 获取指定 agentType 的编译后 Hook
// 自动检测文件 mtime 变更并热重载
func (e *HookEngine) GetHook(agentType string) *interp.Interpreter {
	e.mu.RLock()
	cached, ok := e.interpreters[agentType]
	e.mu.RUnlock()
	if !ok {
		return nil
	}

	// 检查热重载
	info, err := os.Stat(cached.filePath)
	if err == nil && info.ModTime().After(cached.mtime) {
		return e.reload(agentType, cached.filePath)
	}

	return cached.interp
}

// reload 重新编译 Hook 文件（热重载）
func (e *HookEngine) reload(agentType, filePath string) *interp.Interpreter {
	interp, err := e.loader.Load(filePath)
	if err != nil {
		return nil
	}

	e.mu.Lock()
	info, _ := os.Stat(filePath)
	e.interpreters[agentType] = &cachedHook{
		interp:     interp,
		filePath:   filePath,
		mtime:      info.ModTime(),
		compiledAt: time.Now(),
	}
	e.mu.Unlock()
	return interp
}
