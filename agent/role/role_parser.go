package role

import (
	"path/filepath"
	"strings"

	cerr "catcode/core/errors"
	"gopkg.in/yaml.v3"
)

// isRoleFile 检查是否为角色定义文件
func isRoleFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

// mergeRoles 覆盖同名的已存在角色
func mergeRoles(roles []RoleDef, newDef RoleDef) []RoleDef {
	for i, r := range roles {
		if r.Name == newDef.Name {
			roles[i] = newDef // 文件系统版本覆盖嵌入版本
			return roles
		}
	}
	return append(roles, newDef)
}

// ParseYAML 从 YAML 字符串解析角色定义（公开 API）
func ParseYAML(content string) (RoleDef, error) {
	var def RoleDef
	if err := yaml.Unmarshal([]byte(content), &def); err != nil {
		return RoleDef{}, cerr.Wrap(err, "role: YAML 解析失败")
	}
	// 确保 nil 字段有默认零值
	if def.Permission == nil {
		def.Permission = make(map[string]any)
	}
	if def.Tools == nil {
		def.Tools = make([]string, 0)
	}
	if def.Triggers == nil {
		def.Triggers = make([]TriggerDef, 0)
	}
	if def.Persona == nil {
		def.Persona = make(map[string]int)
	}
	if def.States == nil {
		def.States = make([]StateDef, 0)
	}
	return def, nil
}
