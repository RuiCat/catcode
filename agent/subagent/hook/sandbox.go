// Package hook 提供 Hook 安全沙箱策略
package hook

import (
	"reflect"

	"catcode/core/errors"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// SandboxPolicy 定义 Hook 沙箱的安全策略
type SandboxPolicy struct {
	// AllowedPackages 允许的包路径白名单
	AllowedPackages []string
}

// DefaultSandboxPolicy 返回默认沙箱策略
func DefaultSandboxPolicy() SandboxPolicy {
	return SandboxPolicy{
		AllowedPackages: []string{
			"fmt", "strings", "strconv", "time", "encoding/json",
			"catcode/core/errors",
		},
	}
}

// ApplyTo 将沙箱策略应用到 yaegi 解释器
func (p SandboxPolicy) ApplyTo(i *interp.Interpreter) {
	// 只使用受限模式
	i.Use(stdlib.Symbols)
	// 额外注册 catcode errors 包符号
	i.Use(interp.Exports{
		"catcode/core/errors/errors": {
			"New":  reflect.ValueOf(errors.New),
			"Newf": reflect.ValueOf(errors.Newf),
		},
	})
}
