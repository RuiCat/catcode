package plugin

import (
	"testing"
)

func TestUIAPI_RegisterSidebarTab(t *testing.T) {
	api := NewUIAPI()

	ch, err := api.RegisterSidebarTab("test-panel", "🧪 测试面板")
	if err != nil {
		t.Fatalf("RegisterSidebarTab failed: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// 写入内容
	ch <- "# 测试\n内容"
	close(ch) // 模拟

	panels := api.GetPanels()
	p, ok := panels["test-panel"]
	if !ok {
		t.Fatal("panel not found in GetPanels")
	}
	if p.Title != "🧪 测试面板" {
		t.Errorf("Title = %q, want %q", p.Title, "🧪 测试面板")
	}
	if p.Key != "test-panel" {
		t.Errorf("Key = %q, want %q", p.Key, "test-panel")
	}
}

func TestUIAPI_DuplicateRegister(t *testing.T) {
	api := NewUIAPI()
	_, err := api.RegisterSidebarTab("dup", "面板1")
	if err != nil {
		t.Fatalf("first RegisterSidebarTab failed: %v", err)
	}
	_, err = api.RegisterSidebarTab("dup", "面板2")
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestUIAPI_Unregister(t *testing.T) {
	api := NewUIAPI()
	_, err := api.RegisterSidebarTab("tmp", "临时")
	if err != nil {
		t.Fatalf("RegisterSidebarTab failed: %v", err)
	}
	if err := api.UnregisterSidebarTab("tmp"); err != nil {
		t.Fatalf("UnregisterSidebarTab failed: %v", err)
	}
	panels := api.GetPanels()
	if _, ok := panels["tmp"]; ok {
		t.Fatal("panel should have been removed")
	}
}

func TestUIAPI_UnregisterIdempotent(t *testing.T) {
	api := NewUIAPI()
	if err := api.UnregisterSidebarTab("nonexistent"); err != nil {
		t.Fatalf("UnregisterSidebarTab should be idempotent: %v", err)
	}
}

func TestUIAPI_GetPanelsEmpty(t *testing.T) {
	api := NewUIAPI()
	panels := api.GetPanels()
	if len(panels) != 0 {
		t.Errorf("expected 0 panels, got %d", len(panels))
	}
}
