package tui

import "testing"

func TestTruncStr_Shorter(t *testing.T) {
	result := truncStr("hello", 10)
	if result != "hello" {
		t.Errorf("truncStr(%q, 10) = %q, want %q", "hello", result, "hello")
	}
}

func TestTruncStr_Exactly(t *testing.T) {
	result := truncStr("hello", 5)
	if result != "hello" {
		t.Errorf("truncStr(%q, 5) = %q, want %q", "hello", result, "hello")
	}
}

func TestTruncStr_Longer(t *testing.T) {
	result := truncStr("hello world", 5)
	if result != "hello…" {
		t.Errorf("truncStr(%q, 5) = %q, want %q", "hello world", result, "hello…")
	}
}

func TestTruncStr_Empty(t *testing.T) {
	result := truncStr("", 5)
	if result != "" {
		t.Errorf("truncStr(%q, 5) = %q, want empty", "", result)
	}
}

func TestTruncStr_Zero(t *testing.T) {
	result := truncStr("hello", 0)
	if result != "…" {
		t.Errorf("truncStr(%q, 0) = %q, want %q", "hello", result, "…")
	}
}

func TestTruncStr_Unicode(t *testing.T) {
	result := truncStr("你好世界", 2)
	if result != "你好…" {
		t.Errorf("truncStr(%q, 2) = %q, want %q", "你好世界", result, "你好…")
	}
}

func TestMax_FirstGreater(t *testing.T) {
	if max(10, 5) != 10 {
		t.Error("max(10, 5) should be 10")
	}
}

func TestMax_SecondGreater(t *testing.T) {
	if max(5, 10) != 10 {
		t.Error("max(5, 10) should be 10")
	}
}

func TestMax_Equal(t *testing.T) {
	if max(5, 5) != 5 {
		t.Error("max(5, 5) should be 5")
	}
}

func TestMax_Negative(t *testing.T) {
	if max(-5, -10) != -5 {
		t.Error("max(-5, -10) should be -5")
	}
}

func TestWrapText_Empty(t *testing.T) {
	result := wrapText("", 10)
	if result != "" {
		t.Errorf("wrapText(%q, 10) = %q, want empty", "", result)
	}
}

func TestWrapText_NoWrap(t *testing.T) {
	result := wrapText("hello", 10)
	if result != "hello" {
		t.Errorf("wrapText(%q, 10) = %q, want %q", "hello", result, "hello")
	}
}

func TestWrapText_ExactWidth(t *testing.T) {
	result := wrapText("hello", 5)
	if result != "hello" {
		t.Errorf("wrapText(%q, 5) = %q, want %q", "hello", result, "hello")
	}
}

func TestWrapText_Wraps(t *testing.T) {
	// maxWidth <= 4 时直接返回原字符串
	result := wrapText("abcdefg", 3)
	if result != "abcdefg" {
		t.Errorf("wrapText(%q, 3) = %q, want %q", "abcdefg", result, "abcdefg")
	}
}

func TestWrapText_WrapsWide(t *testing.T) {
	result := wrapText("abcdefg", 5)
	if result != "abcde\nfg" {
		t.Errorf("wrapText(%q, 5) = %q, want %q", "abcdefg", result, "abcde\nfg")
	}
}

func TestWrapText_NarrowWidth(t *testing.T) {
	result := wrapText("test", 4)
	if result != "test" {
		t.Errorf("wrapText(%q, 4) = %q, want %q", "test", result, "test")
	}
}

func TestWrapText_Unicode(t *testing.T) {
	// maxWidth <= 4 时直接返回原字符串
	result := wrapText("你好世界你好", 2)
	if result != "你好世界你好" {
		t.Errorf("wrapText(%q, 2) = %q, want %q", "你好世界你好", result, "你好世界你好")
	}
}

func TestWrapText_UnicodeWide(t *testing.T) {
	result := wrapText("你好世界你好", 5)
	if result != "你好世界你\n好" {
		t.Errorf("wrapText(%q, 5) = %q, want %q", "你好世界你好", result, "你好世界你\n好")
	}
}
