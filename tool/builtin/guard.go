package builtin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	cerr "catcode/core/errors"
	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 命令守卫 — 正则分析 + 配置扩展
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GuardResult 命令审查结果
type GuardResult struct {
	Level      string
	Denied     bool
	Reason     string
	Suggestion string
}

type criticalRule struct {
	pattern *regexp.Regexp
	reason  string
	source  string // "builtin" / "config"
}

// GuardConfig 配置中的守卫规则
type GuardConfig struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

var builtinRules []criticalRule
var configRules []criticalRule

func init() {
	builtinRules = []criticalRule{
		{regexp.MustCompile(`\brm\s+.*-(?:recursive\s+)?(?:rf|fr|r\s+-f|f\s+-r)\s+(?:/(?:\s|$|\.|\*+)|~(?:\s|$|/)|\.(?:\s|$))`), "递归删除关键目录", "builtin"},
		{regexp.MustCompile(`\bdd\s+.*of\s*=\s*/dev/(?:sd|hd|nvme|mmcblk|vd|xvd)`), "dd 写入磁盘设备", "builtin"},
		{regexp.MustCompile(`\bmkfs\.\w*\s+/dev/`), "格式化文件系统", "builtin"},
		{regexp.MustCompile(`\bfdisk\s+/dev/`), "磁盘分区操作", "builtin"},
		{regexp.MustCompile(`:\s*\(\s*\)\s*\{`), "fork 炸弹", "builtin"},
		{regexp.MustCompile(`>\s*/dev/(?:sd|hd|nvme|mmcblk)`), "直接写入磁盘设备", "builtin"},
		{regexp.MustCompile(`\bchmod\s+.*777\s+(?:/|/etc|/bin|/usr|/boot)`), "修改关键目录权限为777", "builtin"},
		{regexp.MustCompile(`\b(?:mkfs|mkswap|wipefs)\s+/dev/`), "磁盘格式化/擦除操作", "builtin"},
		// 系统关机/重启
		{regexp.MustCompile(`\b(shutdown|reboot|halt|poweroff|init\s+[06])\b`), "系统关机/重启操作", "builtin"},
		// 管道执行远程脚本
		{regexp.MustCompile(`(?:curl|wget)\s+.*\|\s*(?:ba)?sh\b`), "管道执行远程脚本", "builtin"},
		// 直接写入磁盘设备
		{regexp.MustCompile(`>\s*/dev/sd[a-z]`), "直接覆盖磁盘设备", "builtin"},
		// 递归修改根目录所有者/权限
		{regexp.MustCompile(`\bchown\s+.*-R\s+.*\s+/(?:\s|$)`), "递归修改根目录所有者", "builtin"},
		{regexp.MustCompile(`\bchmod\s+.*-R\s+.*\s+/(?:\s|$)`), "递归修改根目录权限", "builtin"},
		// 修改 crontab
		{regexp.MustCompile(`\bcrontab\s+-`), "修改 crontab 定时任务", "builtin"},
		// 修改防火墙规则
		{regexp.MustCompile(`\biptables\s+-`), "修改防火墙规则", "builtin"},
		// netcat 监听模式
		{regexp.MustCompile(`\b(?:nc|ncat)\s+-[lL]`), "netcat 监听模式", "builtin"},
		// fork 炸弹
		{regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`), "fork 炸弹", "builtin"},
	}
}

// LoadGuardPatterns 从 DB 加载自定义守卫规则
func LoadGuardPatterns(wdb storage.WorkspaceDB) error {
	raw, _, err := wdb.GetSetting("guard.patterns")
	if err != nil || raw == "" {
		return nil // 无自定义规则
	}
	var configs []GuardConfig
	if err := json.Unmarshal([]byte(raw), &configs); err != nil {
		return cerr.Wrap(err, "guard: 解析 guard.patterns 失败")
	}

	configRules = nil
	for _, cfg := range configs {
		re, err := regexp.Compile(cfg.Pattern)
		if err != nil {
			continue // 跳过无效正则
		}
		configRules = append(configRules, criticalRule{
			pattern: re,
			reason:  cfg.Reason,
			source:  "config",
		})
	}
	return nil
}

// guardCheck 基于正则的命令安全审查（内置 + 配置规则）
func guardCheck(cmd string) GuardResult {
	normalized := normalizeCmd(cmd)

	// 先检查配置规则（用户自定义优先）
	for _, rule := range configRules {
		if rule.pattern.MatchString(normalized) {
			return GuardResult{
				Level: "CRITICAL", Denied: true, Reason: rule.reason,
				Suggestion: fmt.Sprintf("自定义守卫拦截: %s。", rule.reason),
			}
		}
	}
	// 再检查内置规则
	for _, rule := range builtinRules {
		if rule.pattern.MatchString(normalized) {
			return GuardResult{
				Level: "CRITICAL", Denied: true, Reason: rule.reason,
				Suggestion: "命令被守卫拦截。如果确认安全，请通过 @guard 子智能体审查。",
			}
		}
	}

	return GuardResult{Level: "SAFE", Denied: false}
}

func normalizeCmd(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	spaceRe := regexp.MustCompile(`\s+`)
	cmd = spaceRe.ReplaceAllString(cmd, " ")
	return " " + cmd + " "
}
