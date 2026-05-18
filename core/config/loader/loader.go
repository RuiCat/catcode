// Package loader 提供统一的配置加载框架
//
// 设计理念：
// - Source（源）：配置的来源，如文件、环境变量、远程等
// - Loader（加载器）：管理多个 Source，按优先级合并
// - Watcher（监视器）：支持配置热重载
//
// 优先级（从低到高）：
// 内置默认值 → 全局配置 → 项目配置 → 本地配置 → 环境变量 → CLI 参数
package loader

import (
	"encoding/json"
	"sync"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 核心类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Source 代表一个配置源
type Source struct {
	// Name 配置源名称（用于调试和错误报告）
	Name string
	// Priority 优先级（数字越大优先级越高）
	Priority int
	// Load 加载配置的函数
	Load func() (map[string]any, error)
}

// ChangeEvent 配置变更事件
type ChangeEvent struct {
	Source string   // 变更来源
	Keys   []string // 变更的键路径
}

// ChangeHandler 配置变更回调
type ChangeHandler func(event ChangeEvent)

// Loader 统一配置加载器
type Loader struct {
	mu       sync.RWMutex
	sources  []Source
	watchers []Watcher
	handlers []ChangeHandler
	cache    map[string]any // 合并后的缓存
}

// Watcher 配置监视器接口
type Watcher interface {
	// Start 开始监视配置变更
	Start(onChange func(ChangeEvent)) error
	// Stop 停止监视
	Stop() error
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 构造与配置
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// New 创建新的配置加载器
func New() *Loader {
	return &Loader{
		sources:  make([]Source, 0),
		handlers: make([]ChangeHandler, 0),
		cache:    make(map[string]any),
	}
}

// AddSource 添加配置源
func (l *Loader) AddSource(src Source) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sources = append(l.sources, src)
	// 按优先级排序（稳定排序，同优先级保持添加顺序）
	l.sortSources()
}

// AddWatcher 添加配置监视器
func (l *Loader) AddWatcher(w Watcher) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.watchers = append(l.watchers, w)
}

// OnChange 注册配置变更回调
func (l *Loader) OnChange(handler ChangeHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers = append(l.handlers, handler)
}

// sortSources 按优先级排序（低→高）
func (l *Loader) sortSources() {
	for i := 0; i < len(l.sources); i++ {
		for j := i + 1; j < len(l.sources); j++ {
			if l.sources[i].Priority > l.sources[j].Priority {
				l.sources[i], l.sources[j] = l.sources[j], l.sources[i]
			}
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 加载与合并
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Load 加载所有配置源并合并
// 返回合并后的扁平化键值对
func (l *Loader) Load() (map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	merged := make(map[string]any)

	for _, src := range l.sources {
		data, err := src.Load()
		if err != nil {
			return nil, cerr.Wrapf(err, "loader: 配置源 %q 加载失败", src.Name)
		}
		if data != nil {
			deepMerge(merged, data)
		}
	}

	l.cache = copyMap(merged)
	return merged, nil
}

// LoadInto 加载配置并填充到目标结构体
// 使用 JSON 作为中间格式，支持嵌套结构体
func (l *Loader) LoadInto(target any) error {
	data, err := l.Load()
	if err != nil {
		return err
	}

	// 将 map 序列化为 JSON，再反序列化到目标结构体
	// 这种方式天然支持嵌套结构体、类型转换等
	jsonData, err := json.Marshal(data)
	if err != nil {
		return cerr.Wrap(err, "loader: 序列化配置失败")
	}

	if err := json.Unmarshal(jsonData, target); err != nil {
		return cerr.Wrap(err, "loader: 反序列化到目标结构体失败")
	}

	return nil
}

// GetCache 获取缓存的合并配置
func (l *Loader) GetCache() map[string]any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return copyMap(l.cache)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 热重载
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// StartWatching 启动所有监视器
func (l *Loader) StartWatching() error {
	l.mu.RLock()
	watchers := make([]Watcher, len(l.watchers))
	copy(watchers, l.watchers)
	l.mu.RUnlock()

	for _, w := range watchers {
		if err := w.Start(func(event ChangeEvent) {
			l.handleChange(event)
		}); err != nil {
			return cerr.Wrap(err, "loader: 启动监视器失败")
		}
	}
	return nil
}

// StopWatching 停止所有监视器
func (l *Loader) StopWatching() error {
	l.mu.RLock()
	watchers := make([]Watcher, len(l.watchers))
	copy(watchers, l.watchers)
	l.mu.RUnlock()

	for _, w := range watchers {
		if err := w.Stop(); err != nil {
			return cerr.Wrap(err, "loader: 停止监视器失败")
		}
	}
	return nil
}

// handleChange 处理配置变更
func (l *Loader) handleChange(event ChangeEvent) {
	l.mu.RLock()
	handlers := make([]ChangeHandler, len(l.handlers))
	copy(handlers, l.handlers)
	l.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 内部工具函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// deepMerge 深度合并 src 到 dst
// map 类型递归合并，其他类型直接覆盖
func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		dv, exists := dst[k]
		if !exists {
			dst[k] = sv
			continue
		}
		// 两者都是 map，递归合并
		srcMap, srcOK := sv.(map[string]any)
		dstMap, dstOK := dv.(map[string]any)
		if srcOK && dstOK {
			deepMerge(dstMap, srcMap)
			continue
		}
		// 否则直接覆盖
		dst[k] = sv
	}
}

// copyMap 深拷贝 map
func copyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if m, ok := v.(map[string]any); ok {
			dst[k] = copyMap(m)
		} else {
			dst[k] = v
		}
	}
	return dst
}
