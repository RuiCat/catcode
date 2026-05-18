package loader

import (
	"testing"
)

func newTestSource(name string, priority int, data map[string]any) Source {
	return Source{
		Name:     name,
		Priority: priority,
		Load: func() (map[string]any, error) {
			return copyMap(data), nil
		},
	}
}

func TestNewLoader(t *testing.T) {
	l := New()
	if l == nil {
		t.Fatal("New() 返回 nil")
	}
}

func TestAddSource(t *testing.T) {
	l := New()
	l.AddSource(newTestSource("test", 0, map[string]any{"key": "value"}))
	data, err := l.Load()
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("期望 key=value, 得到 %v", data["key"])
	}
}

func TestSourcePriority(t *testing.T) {
	l := New()
	l.AddSource(newTestSource("low", 10, map[string]any{"key": "low"}))
	l.AddSource(newTestSource("high", 20, map[string]any{"key": "high"}))
	data, err := l.Load()
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	if data["key"] != "high" {
		t.Errorf("期望 key=high, 得到 %v", data["key"])
	}
}

func TestDeepMerge(t *testing.T) {
	l := New()
	l.AddSource(newTestSource("base", 10, map[string]any{
		"nested": map[string]any{"a": "base", "b": "base"},
	}))
	l.AddSource(newTestSource("override", 20, map[string]any{
		"nested": map[string]any{"a": "override"},
	}))
	data, err := l.Load()
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	nested := data["nested"].(map[string]any)
	if nested["a"] != "override" {
		t.Errorf("期望 override, 得到 %v", nested["a"])
	}
	if nested["b"] != "base" {
		t.Errorf("期望 base, 得到 %v", nested["b"])
	}
}

func TestCopyMap(t *testing.T) {
	original := map[string]any{
		"key1":   "value1",
		"nested": map[string]any{"key2": "value2"},
	}
	copied := copyMap(original)
	copied["key1"] = "modified"
	copied["nested"].(map[string]any)["key2"] = "modified"
	if original["key1"] != "value1" {
		t.Error("浅拷贝错误")
	}
	if original["nested"].(map[string]any)["key2"] != "value2" {
		t.Error("深拷贝错误")
	}
}

func TestDirWatcher(t *testing.T) {
	dir := t.TempDir()
	w := NewDirWatcher(dir, "*.json", 1000000)
	if w == nil {
		t.Fatal("NewDirWatcher 返回 nil")
	}
	w.Start(func(evt ChangeEvent) {})
	w.Stop()
}

func TestOnChange(t *testing.T) {
	l := New()
	called := false
	l.OnChange(func(event ChangeEvent) {
		called = true
	})
	l.handleChange(ChangeEvent{Source: "test"})
	if !called {
		t.Error("OnChange 未触发")
	}
}
