package role

import (
	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色合并（3层优先级）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// mergeAgentDefs 按优先级合并角色定义：
// 用户文件 (user_file) > DB 用户 (user_db) > DB 内置 (builtin)
func mergeAgentDefs(dbDefs []*storage.AgentRow, fileDefs []RoleDef) []RoleDef {
	merged := make(map[string]RoleDef)

	// 第1轮：DB builtin（最低优先级）
	for _, d := range dbDefs {
		if d.Source == "builtin" {
			merged[d.Name] = AgentRowToRoleDef(d)
		}
	}

	// 第2轮：DB user（覆盖 builtin）
	for _, d := range dbDefs {
		if d.Source == "user_db" {
			merged[d.Name] = AgentRowToRoleDef(d)
		}
	}

	// 第3轮：文件定义（最高优先级，覆盖 DB 中同名角色）
	for _, d := range fileDefs {
		merged[d.Name] = d
	}

	result := make([]RoleDef, 0, len(merged))
	for _, v := range merged {
		result = append(result, v)
	}
	return result
}
