// Package plugin 实现 catcode TUI 插件 API
// 插件可通过此 API 动态注册侧边栏标签并更新内容
package plugin

import (
	"sync"

	cerr "catcode/core/errors"
	"catcode/ui/tui/component"
)

// UIAPI 插件界面接口
// 插件通过此接口与 TUI 交互，无需直接操作 TUI 内部状态
type UIAPI interface {
	// RegisterSidebarTab 注册自定义侧边栏 Tab，返回更新内容的通道
	RegisterSidebarTab(key string, title string) (updateCh chan<- string, err error)

	// UnregisterSidebarTab 注销侧边栏 Tab
	UnregisterSidebarTab(key string) error

	// GetPanels 获取所有已注册的插件面板（用于侧边栏轮询渲染）
	GetPanels() map[string]component.PluginPanelEntry
}

// uiAPIImpl UIAPI 的具体实现
type uiAPIImpl struct {
	mu     sync.RWMutex
	panels map[string]*panelState
}

type panelState struct {
	info   component.PluginPanelEntry
	update chan string
}

// NewUIAPI 创建 UIAPI 实例
func NewUIAPI() UIAPI {
	return &uiAPIImpl{
		panels: make(map[string]*panelState),
	}
}

func (u *uiAPIImpl) RegisterSidebarTab(key string, title string) (chan<- string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, exists := u.panels[key]; exists {
		return nil, cerr.Newf("panel %q already registered", key)
	}

	ch := make(chan string, 1)
	u.panels[key] = &panelState{
		info:   component.PluginPanelEntry{Key: key, Title: title},
		update: ch,
	}
	return ch, nil
}

func (u *uiAPIImpl) UnregisterSidebarTab(key string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	state, exists := u.panels[key]
	if !exists {
		return nil // 幂等
	}
	close(state.update)
	delete(u.panels, key)
	return nil
}

func (u *uiAPIImpl) GetPanels() map[string]component.PluginPanelEntry {
	u.mu.RLock()
	defer u.mu.RUnlock()

	result := make(map[string]component.PluginPanelEntry, len(u.panels))
	for k, s := range u.panels {
		// 非阻塞读取最新内容
		select {
		case content := <-s.update:
			s.info.Content = content
		default:
		}
		result[k] = s.info
	}
	return result
}
