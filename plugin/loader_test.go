package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"catcode/core/event"
)

// TestLoader_Discover 测试插件发现功能
func TestLoader_Discover(t *testing.T) {
	// 创建临时插件目录
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}
	loader := NewLoader(tmpDir, ctx)

	// 空目录应返回 nil
	files, err := loader.Discover()
	if err != nil {
		t.Fatalf("空目录 Discover 失败: %v", err)
	}
	if files != nil {
		t.Fatalf("期望 nil，得到 %v", files)
	}

	// 创建测试插件文件
	pluginContent := `package main
type TestPlugin struct{}
func (p *TestPlugin) Name() string { return "test" }
func (p *TestPlugin) Version() string { return "1.0.0" }
var Plugin TestPlugin
`
	pluginPath := filepath.Join(tmpDir, "test_plugin.go")
	if err := os.WriteFile(pluginPath, []byte(pluginContent), 0644); err != nil {
		t.Fatalf("写入插件文件失败: %v", err)
	}

	// 应该发现插件文件
	files, err = loader.Discover()
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("期望 1 个文件，得到 %d", len(files))
	}
	if files[0] != pluginPath {
		t.Fatalf("期望 %s，得到 %s", pluginPath, files[0])
	}

	// 非 .go 文件应被忽略
	nonGoPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(nonGoPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("写入非插件文件失败: %v", err)
	}
	files, err = loader.Discover()
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("期望 1 个文件（忽略 .txt），得到 %d", len(files))
	}
}

// TestLoader_Load_SimplePlugin 测试加载简单插件（不依赖 catcode 内部包）
func TestLoader_Load_SimplePlugin(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}
	loader := NewLoader(tmpDir, ctx)

	// 一个简单的插件，不依赖 catcode 内部包
	pluginContent := `package main

import "fmt"

type SimplePlugin struct{}

func (p *SimplePlugin) Name() string { return "simple" }
func (p *SimplePlugin) Version() string { return "0.1.0" }

var Plugin SimplePlugin
`
	pluginPath := filepath.Join(tmpDir, "simple.go")
	if err := os.WriteFile(pluginPath, []byte(pluginContent), 0644); err != nil {
		t.Fatalf("写入插件文件失败: %v", err)
	}

	p, err := loader.Load(pluginPath)
	if err != nil {
		t.Fatalf("加载插件失败: %v", err)
	}

	if p.Name() != "simple" {
		t.Fatalf("期望 Name()=simple，得到 %s", p.Name())
	}
	if p.Version() != "0.1.0" {
		t.Fatalf("期望 Version()=0.1.0，得到 %s", p.Version())
	}
}

// TestLoader_Load_WithCatcodeImports 测试加载依赖 catcode 内部包的插件
func TestLoader_Load_WithCatcodeImports(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}
	loader := NewLoader(tmpDir, ctx)

	// 这个插件依赖 catcode/tool 包
	pluginContent := `package main

import (
	"catcode/tool"
)

type ToolPlugin struct{}

func (p *ToolPlugin) Name() string { return "tool-test" }
func (p *ToolPlugin) Version() string { return "1.0.0" }
func (p *ToolPlugin) Tools(bus interface{}) []*tool.Tool { return nil }

var Plugin ToolPlugin
`
	pluginPath := filepath.Join(tmpDir, "tool_plugin.go")
	if err := os.WriteFile(pluginPath, []byte(pluginContent), 0644); err != nil {
		t.Fatalf("写入插件文件失败: %v", err)
	}

	p, err := loader.Load(pluginPath)
	if err != nil {
		t.Fatalf("依赖 catcode 包的插件加载失败: %v", err)
	}

	if p.Name() != "tool-test" {
		t.Fatalf("期望 Name()=tool-test，得到 %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("期望 Version()=1.0.0，得到 %s", p.Version())
	}

	// 验证类型检测
	if pw, ok := p.(*pluginWrapper); ok {
		if pw.infoType != "tool" {
			t.Fatalf("期望 infoType=tool，得到 %s", pw.infoType)
		}
		if !pw.hasTools {
			t.Fatal("期望 hasTools=true")
		}
	}
}

// TestManager_LoadAll 测试 Manager 的 LoadAll 方法
func TestManager_LoadAll(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}

	// 创建简单插件
	pluginContent := `package main
type SimplePlugin struct{}
func (p *SimplePlugin) Name() string { return "simple" }
func (p *SimplePlugin) Version() string { return "1.0.0" }
var Plugin SimplePlugin
`
	if err := os.WriteFile(filepath.Join(tmpDir, "simple.go"), []byte(pluginContent), 0644); err != nil {
		t.Fatalf("写入插件文件失败: %v", err)
	}

	mgr := NewManager(tmpDir, ctx)
	plugins, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}

	if len(plugins) != 1 {
		t.Fatalf("期望 1 个插件，得到 %d", len(plugins))
	}
	if plugins[0].Name != "simple" {
		t.Fatalf("期望 Name=simple，得到 %s", plugins[0].Name)
	}
	if plugins[0].Version != "1.0.0" {
		t.Fatalf("期望 Version=1.0.0，得到 %s", plugins[0].Version)
	}
	if plugins[0].Type != "unknown" {
		t.Fatalf("期望 Type=unknown（无接口实现），得到 %s", plugins[0].Type)
	}
	if !plugins[0].Enabled {
		t.Fatalf("期望 Enabled=true")
	}
}

// TestManager_List 测试 Manager 的 List 方法
func TestManager_List(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}

	// 创建两个插件
	plugin1 := `package main
type P1 struct{}
func (p *P1) Name() string { return "plugin-a" }
func (p *P1) Version() string { return "1.0.0" }
var Plugin P1
`
	plugin2 := `package main
type P2 struct{}
func (p *P2) Name() string { return "plugin-b" }
func (p *P2) Version() string { return "2.0.0" }
var Plugin P2
`
	os.WriteFile(filepath.Join(tmpDir, "p1.go"), []byte(plugin1), 0644)
	os.WriteFile(filepath.Join(tmpDir, "p2.go"), []byte(plugin2), 0644)

	mgr := NewManager(tmpDir, ctx)
	_, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("期望 2 个插件，得到 %d", len(list))
	}

	// 验证两个插件都在列表中
	names := make(map[string]bool)
	for _, p := range list {
		names[p.Name] = true
	}
	if !names["plugin-a"] {
		t.Fatal("缺少 plugin-a")
	}
	if !names["plugin-b"] {
		t.Fatal("缺少 plugin-b")
	}
}

// TestManager_Reload 测试插件重载
func TestManager_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &PluginContext{WorkDir: tmpDir, Bus: event.NewBus()}

	pluginContent := `package main
type TestPlugin struct{}
func (p *TestPlugin) Name() string { return "reload-test" }
func (p *TestPlugin) Version() string { return "1.0.0" }
var Plugin TestPlugin
`
	os.WriteFile(filepath.Join(tmpDir, "reload.go"), []byte(pluginContent), 0644)

	mgr := NewManager(tmpDir, ctx)
	loaded, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("期望 1 个插件，得到 %d", len(loaded))
	}

	// 重载已加载的插件
	err = mgr.Reload("reload-test")
	if err != nil {
		t.Fatalf("Reload 失败: %v", err)
	}

	// 重载不存在的插件应返回错误
	err = mgr.Reload("non-existent")
	if err == nil {
		t.Fatal("期望 Reload 不存在的插件返回错误")
	}
}

// TestManager_LoadAll_NonExistentDir 测试插件目录不存在的情况
func TestManager_LoadAll_NonExistentDir(t *testing.T) {
	nonExistentDir := filepath.Join(os.TempDir(), "catcode-test-non-existent-12345")
	ctx := &PluginContext{WorkDir: "/tmp", Bus: event.NewBus()}
	mgr := NewManager(nonExistentDir, ctx)

	plugins, err := mgr.LoadAll()
	if err != nil {
		t.Fatalf("目录不存在时应返回 nil 而非错误: %v", err)
	}
	if plugins != nil {
		t.Fatalf("期望 nil，得到 %v", plugins)
	}
}
