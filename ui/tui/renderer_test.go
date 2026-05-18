package tui

import (
	"strings"
	"testing"
)

func newDarkRenderer() *MarkdownRenderer {
	return NewMarkdownRenderer(80, true)
}

func newLightRenderer() *MarkdownRenderer {
	return NewMarkdownRenderer(80, false)
}

func TestRenderer_Render_Empty(t *testing.T) {
	r := newDarkRenderer()
	if got := r.Render(""); got != "" {
		t.Errorf("Render(\"\") = %q, want empty", got)
	}
}

func TestRenderer_Render_PlainText(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("hello world")
	if !strings.Contains(result, "hello world") {
		t.Errorf("Render should contain plain text, got %q", result)
	}
}

func TestRenderer_Render_Bold(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("**bold** text")
	if result == "" || !strings.Contains(result, "bold") {
		t.Errorf("Render should contain bold text, got %q", result)
	}
}

func TestRenderer_Render_Italic(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("*italic* text")
	if result == "" || !strings.Contains(result, "italic") {
		t.Errorf("Render should contain italic text, got %q", result)
	}
}

func TestRenderer_Render_InlineCode(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("use `fmt.Println()` function")
	if result == "" || !strings.Contains(result, "fmt.Println()") {
		t.Errorf("Render should contain inline code, got %q", result)
	}
}

func TestRenderer_Render_Heading(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("# Title")
	if result == "" || !strings.Contains(result, "Title") {
		t.Errorf("Render should contain heading text, got %q", result)
	}
}

func TestRenderer_Render_Heading2(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("## Section")
	if result == "" || !strings.Contains(result, "Section") {
		t.Errorf("Render should contain h2 text, got %q", result)
	}
}

func TestRenderer_Render_HorizontalRule(t *testing.T) {
	r := newDarkRenderer()
	for _, hr := range []string{"---", "***", "___"} {
		result := r.Render(hr)
		if result == "" {
			t.Errorf("Render(%q) should not be empty", hr)
		}
	}
}

func TestRenderer_Render_Quote(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("> quoted text")
	if result == "" || !strings.Contains(result, "quoted text") {
		t.Errorf("Render should contain quoted text, got %q", result)
	}
}

func TestRenderer_Render_CodeBlock(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("```go\nfmt.Println(\"hello\")\n```")
	if result == "" {
		t.Error("Render should not be empty for code block")
	}
	// should contain the code
	if !strings.Contains(result, "fmt.Println") {
		t.Errorf("Render should contain code, got %q", result)
	}
}

func TestRenderer_Render_CodeBlockNoLang(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("```\nsome code\n```")
	if result == "" {
		t.Error("Render should not be empty for code block without language")
	}
	if !strings.Contains(result, "some code") {
		t.Errorf("Render should contain code, got %q", result)
	}
}

func TestRenderer_Render_UnclosedCodeBlock(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("```go\nfmt.Println(\"hello\")\n")
	if result == "" {
		t.Error("Render should not be empty for unclosed code block")
	}
}

func TestRenderer_Render_TaskList(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("- [x] done task\n- [ ] pending task")
	if result == "" {
		t.Error("Render should not be empty for task list")
	}
	if !strings.Contains(result, "done task") {
		t.Errorf("Render should contain done task, got %q", result)
	}
	if !strings.Contains(result, "pending task") {
		t.Errorf("Render should contain pending task, got %q", result)
	}
}

func TestRenderer_Render_OrderedList(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("1. first\n2. second\n3. third")
	if result == "" {
		t.Error("Render should not be empty for ordered list")
	}
	if !strings.Contains(result, "first") || !strings.Contains(result, "second") {
		t.Errorf("Render should contain list items, got %q", result)
	}
}

func TestRenderer_Render_Link(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("[Click here](https://example.com)")
	if result == "" {
		t.Error("Render should not be empty for link")
	}
	if !strings.Contains(result, "Click here") {
		t.Errorf("Render should contain link text, got %q", result)
	}
}

func TestRenderer_Render_MultipleParagraphs(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("first paragraph\n\nsecond paragraph")
	if result == "" {
		t.Error("Render should not be empty")
	}
}

func TestRenderer_DarkMode(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("test")
	if result == "" {
		t.Error("Dark mode render should work")
	}
}

func TestRenderer_LightMode(t *testing.T) {
	r := newLightRenderer()
	result := r.Render("test")
	if result == "" {
		t.Error("Light mode render should work")
	}
}

func TestRenderer_Render_BacktickInText(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("inline `code` with backticks")
	if result == "" {
		t.Error("Render should handle inline code with backticks")
	}
}

func TestRenderer_Render_LeadingSpace(t *testing.T) {
	r := newDarkRenderer()
	result := r.Render("  indented text")
	if !strings.Contains(result, "indented text") {
		t.Errorf("Render should contain indented text, got %q", result)
	}
}
