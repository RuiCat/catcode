package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 目录监视器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DirWatcher 监视目录下所有配置文件变更
type DirWatcher struct {
	dir      string
	pattern  string
	interval time.Duration
	stopCh   chan struct{}
	modTimes map[string]time.Time
	mu       sync.RWMutex
}

// NewDirWatcher 创建目录监视器
func NewDirWatcher(dir, pattern string, interval time.Duration) *DirWatcher {
	return &DirWatcher{
		dir:      dir,
		pattern:  pattern,
		interval: interval,
		stopCh:   make(chan struct{}),
		modTimes: make(map[string]time.Time),
	}
}

// Start 开始监视目录变更
func (dw *DirWatcher) Start(onChange func(ChangeEvent)) error {
	// 初始化
	dw.scanFiles()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[PANIC] DirWatcher goroutine: %v\n%s\n", r, debug.Stack())
			}
		}()
		ticker := time.NewTicker(dw.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				dw.checkDirChanges(onChange)
			case <-dw.stopCh:
				return
			}
		}
	}()

	return nil
}

// Stop 停止监视
func (dw *DirWatcher) Stop() error {
	close(dw.stopCh)
	return nil
}

func (dw *DirWatcher) scanFiles() {
	entries, err := os.ReadDir(dw.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if dw.pattern != "" {
			if matched, _ := filepath.Match(dw.pattern, entry.Name()); !matched {
				continue
			}
		}

		path := filepath.Join(dw.dir, entry.Name())
		info, err := entry.Info()
		if err == nil {
			dw.mu.Lock()
			dw.modTimes[path] = info.ModTime()
			dw.mu.Unlock()
		}
	}
}

func (dw *DirWatcher) checkDirChanges(onChange func(ChangeEvent)) {
	entries, err := os.ReadDir(dw.dir)
	if err != nil {
		return
	}

	currentFiles := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if dw.pattern != "" {
			if matched, _ := filepath.Match(dw.pattern, entry.Name()); !matched {
				continue
			}
		}

		path := filepath.Join(dw.dir, entry.Name())
		currentFiles[path] = true

		info, err := entry.Info()
		if err != nil {
			continue
		}

		dw.mu.RLock()
		lastMod, exists := dw.modTimes[path]
		dw.mu.RUnlock()
		if !exists || info.ModTime().After(lastMod) {
			dw.mu.Lock()
			dw.modTimes[path] = info.ModTime()
			dw.mu.Unlock()
			onChange(ChangeEvent{
				Source: fmt.Sprintf("dir:%s", dw.dir),
				Keys:   []string{path},
			})
		}
	}

	// 检查删除的文件
	dw.mu.RLock()
	var deletedPaths []string
	for path := range dw.modTimes {
		if !currentFiles[path] {
			deletedPaths = append(deletedPaths, path)
		}
	}
	dw.mu.RUnlock()

	for _, path := range deletedPaths {
		dw.mu.Lock()
		delete(dw.modTimes, path)
		dw.mu.Unlock()
		onChange(ChangeEvent{
			Source: fmt.Sprintf("dir:%s", dw.dir),
			Keys:   []string{path},
		})
	}
}
