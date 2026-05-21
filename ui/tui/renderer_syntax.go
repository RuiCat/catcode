package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 代码语法高亮 — 轻量级关键词着色
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SyntaxTheme 语法高亮主题
type SyntaxTheme struct {
	Keyword     lipgloss.Color
	Type        lipgloss.Color
	String      lipgloss.Color
	Number      lipgloss.Color
	Comment     lipgloss.Color
	Operator    lipgloss.Color
	Function    lipgloss.Color
	Builtin     lipgloss.Color
	Punctuation lipgloss.Color
}

var (
	// DarkSyntax 暗色语法高亮主题
	DarkSyntax = SyntaxTheme{
		Keyword:     lipgloss.Color("#FF7B72"), // 红色 — 关键字
		Type:        lipgloss.Color("#79C0FF"), // 蓝色 — 类型
		String:      lipgloss.Color("#A5D6FF"), // 浅蓝 — 字符串
		Number:      lipgloss.Color("#79C0FF"), // 蓝色 — 数字
		Comment:     lipgloss.Color("#8B949E"), // 灰色 — 注释
		Operator:    lipgloss.Color("#FFA657"), // 橙色 — 操作符
		Function:    lipgloss.Color("#D2A8FF"), // 紫色 — 函数
		Builtin:     lipgloss.Color("#FFA657"), // 橙色 — 内置
		Punctuation: lipgloss.Color("#C9D1D9"), // 白色 — 标点
	}

	// LightSyntax 亮色语法高亮主题
	LightSyntax = SyntaxTheme{
		Keyword:     lipgloss.Color("#D73A49"), // 红色
		Type:        lipgloss.Color("#005CC5"), // 蓝色
		String:      lipgloss.Color("#032F62"), // 深蓝
		Number:      lipgloss.Color("#005CC5"), // 蓝色
		Comment:     lipgloss.Color("#6A737D"), // 灰色
		Operator:    lipgloss.Color("#D73A49"), // 红色
		Function:    lipgloss.Color("#6F42C1"), // 紫色
		Builtin:     lipgloss.Color("#E36209"), // 橙色
		Punctuation: lipgloss.Color("#24292E"), // 黑色
	}
)

// CodeBlockStyle 代码块样式
type CodeBlockStyle struct {
	HeaderFg  lipgloss.Color
	HeaderBg  lipgloss.Color
	BodyFg    lipgloss.Color
	BodyBg    lipgloss.Color
	LineNumFg lipgloss.Color
}

var (
	codeBlockDark = CodeBlockStyle{
		HeaderFg:  lipgloss.Color("#C9D1D9"),
		HeaderBg:  lipgloss.Color("#1a1b26"),
		BodyFg:    lipgloss.Color("#C9D1D9"),
		BodyBg:    lipgloss.Color("#1a1b26"),
		LineNumFg: lipgloss.Color("#484F58"),
	}
	codeBlockLight = CodeBlockStyle{
		HeaderFg:  lipgloss.Color("#24292E"),
		HeaderBg:  lipgloss.Color("#F6F8FA"),
		BodyFg:    lipgloss.Color("#24292E"),
		BodyBg:    lipgloss.Color("#FFFFFF"),
		LineNumFg: lipgloss.Color("#959DA5"),
	}
)

// highlightStyle 创建带背景色的高亮样式（保持代码块背景一致）
func (r *MarkdownRenderer) highlightStyle(fg lipgloss.Color) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(fg)
	if r.codeBg != "" {
		s = s.Background(r.codeBg)
	}
	return s
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 各语言语法高亮
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

var (
	goKeywords = map[string]bool{
		"break": true, "case": true, "chan": true, "const": true, "continue": true,
		"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
		"func": true, "go": true, "goto": true, "if": true, "import": true,
		"interface": true, "map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true, "var": true,
	}
	goTypes = map[string]bool{
		"bool": true, "byte": true, "complex64": true, "complex128": true,
		"error": true, "float32": true, "float64": true, "int": true,
		"int8": true, "int16": true, "int32": true, "int64": true,
		"rune": true, "string": true, "uint": true, "uint8": true,
		"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"nil": true, "true": true, "false": true, "iota": true,
	}
	goBuiltins = map[string]bool{
		"append": true, "cap": true, "close": true, "copy": true, "delete": true,
		"len": true, "make": true, "new": true, "panic": true, "print": true,
		"println": true, "recover": true,
	}
)

func (r *MarkdownRenderer) highlightGo(line string) string {
	// 注释
	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return r.highlightStyle(r.syntaxTheme.Comment).Render(line)
	}

	words := tokenize(line)
	for i, w := range words {
		if goKeywords[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Keyword).Render(w)
		} else if goTypes[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Type).Render(w)
		} else if goBuiltins[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Builtin).Render(w)
		}
	}
	return strings.Join(words, "")
}

func (r *MarkdownRenderer) highlightPython(line string) string {
	pyKeywords := map[string]bool{
		"False": true, "None": true, "True": true, "and": true, "as": true,
		"assert": true, "async": true, "await": true, "break": true, "class": true,
		"continue": true, "def": true, "del": true, "elif": true, "else": true,
		"except": true, "finally": true, "for": true, "from": true, "global": true,
		"if": true, "import": true, "in": true, "is": true, "lambda": true,
		"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
		"return": true, "try": true, "while": true, "with": true, "yield": true,
		"print": true, "len": true, "range": true, "type": true, "isinstance": true,
		"self": true, "cls": true,
	}

	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return r.highlightStyle(r.syntaxTheme.Comment).Render(line)
	}

	words := tokenize(line)
	for i, w := range words {
		if pyKeywords[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Keyword).Render(w)
		}
	}
	return strings.Join(words, "")
}

func (r *MarkdownRenderer) highlightJS(line string) string {
	jsKeywords := map[string]bool{
		"async": true, "await": true, "break": true, "case": true, "catch": true,
		"class": true, "const": true, "continue": true, "debugger": true, "default": true,
		"delete": true, "do": true, "else": true, "export": true, "extends": true,
		"finally": true, "for": true, "function": true, "if": true, "import": true,
		"in": true, "instanceof": true, "let": true, "new": true, "of": true,
		"return": true, "static": true, "switch": true, "this": true, "throw": true,
		"try": true, "typeof": true, "var": true, "void": true, "while": true,
		"with": true, "yield": true, "true": true, "false": true, "null": true,
		"undefined": true, "NaN": true,
	}

	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return r.highlightStyle(r.syntaxTheme.Comment).Render(line)
	}

	words := tokenize(line)
	for i, w := range words {
		if jsKeywords[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Keyword).Render(w)
		}
	}
	return strings.Join(words, "")
}

func (r *MarkdownRenderer) highlightRust(line string) string {
	rsKeywords := map[string]bool{
		"as": true, "break": true, "const": true, "continue": true, "crate": true,
		"else": true, "enum": true, "extern": true, "false": true, "fn": true,
		"for": true, "if": true, "impl": true, "in": true, "let": true,
		"loop": true, "match": true, "mod": true, "move": true, "mut": true,
		"pub": true, "ref": true, "return": true, "self": true, "static": true,
		"struct": true, "super": true, "trait": true, "true": true, "type": true,
		"unsafe": true, "use": true, "where": true, "while": true,
		"Some": true, "None": true, "Ok": true, "Err": true,
	}

	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return r.highlightStyle(r.syntaxTheme.Comment).Render(line)
	}

	words := tokenize(line)
	for i, w := range words {
		if rsKeywords[w] {
			words[i] = r.highlightStyle(r.syntaxTheme.Keyword).Render(w)
		}
	}
	return strings.Join(words, "")
}

func (r *MarkdownRenderer) highlightData(line string) string {
	// JSON/YAML 键值对着色
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		key := r.highlightStyle(r.syntaxTheme.String).Render(parts[0])
		return key + ":" + parts[1]
	}
	return line
}

func (r *MarkdownRenderer) highlightBash(line string) string {
	// 注释
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return r.highlightStyle(r.syntaxTheme.Comment).Render(line)
	}
	// 命令（第一个词）
	words := strings.Fields(line)
	if len(words) > 0 {
		words[0] = r.highlightStyle(r.syntaxTheme.Function).Render(words[0])
		return strings.Join(words, " ")
	}
	return line
}

func (r *MarkdownRenderer) highlightDiff(line string) string {
	if strings.HasPrefix(line, "+") {
		return r.highlightStyle(lipgloss.Color("#3FB950")).Render(line)
	}
	if strings.HasPrefix(line, "-") {
		return r.highlightStyle(lipgloss.Color("#F85149")).Render(line)
	}
	if strings.HasPrefix(line, "@@") {
		return r.highlightStyle(lipgloss.Color("#79C0FF")).Render(line)
	}
	return line
}

func (r *MarkdownRenderer) highlightGeneric(line string) string {
	// 通用高亮：注释、字符串、数字
	words := tokenize(line)
	for i, w := range words {
		// 数字
		if isNumber(w) {
			words[i] = r.highlightStyle(r.syntaxTheme.Number).Render(w)
		}
	}
	return strings.Join(words, "")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 辅助函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// tokenize 将代码行分词（保留空白和标点）
func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range line {
		switch {
		case r == ' ' || r == '\t':
			flush()
			tokens = append(tokens, string(r))
		case r == '(' || r == ')' || r == '{' || r == '}' || r == '[' || r == ']':
			flush()
			tokens = append(tokens, string(r))
		case r == ',' || r == ';' || r == '.':
			flush()
			tokens = append(tokens, string(r))
		case r == ':' || r == '=' || r == '+' || r == '-' || r == '*' || r == '/' || r == '&' || r == '|' || r == '!' || r == '<' || r == '>':
			flush()
			tokens = append(tokens, string(r))
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// isNumber 检查是否为数字
func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			if r != '.' && r != 'x' && r != 'X' && r != 'a' && r != 'b' && r != 'c' && r != 'd' && r != 'e' && r != 'f' {
				return false
			}
		}
	}
	return true
}
