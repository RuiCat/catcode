package storage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Settings 扁平化/重组工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SettingEntry 配置条目
type SettingEntry struct {
	Key         string
	Value       string
	ValueType   string
	Source      string
	Description string
}

// flattenMap 递归扁平化嵌套 map 为 SettingEntry 列表
// 键名使用 "." 分隔层级，例如 "providers.deepseek.base_url"
func flattenMap(prefix string, data map[string]any) []SettingEntry {
	var entries []SettingEntry

	for k, v := range data {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		switch val := v.(type) {
		case map[string]any:
			entries = append(entries, flattenMap(fullKey, val)...)
		case string:
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: val, ValueType: "string",
			})
		case float64:
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: fmt.Sprintf("%v", val), ValueType: autoType(val),
			})
		case bool:
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: fmt.Sprintf("%v", val), ValueType: "bool",
			})
		case int:
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: fmt.Sprintf("%d", val), ValueType: "int",
			})
		case []any:
			jsonBytes, _ := json.Marshal(val)
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: string(jsonBytes), ValueType: "json",
			})
		case nil:
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: "", ValueType: "string",
			})
		default:
			jsonBytes, _ := json.Marshal(val)
			entries = append(entries, SettingEntry{
				Key: fullKey, Value: string(jsonBytes), ValueType: "json",
			})
		}
	}

	return entries
}

// autoType 智能判断 float64 是整数还是浮点数
func autoType(v float64) string {
	if v == float64(int64(v)) {
		return "int"
	}
	return "float"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// UnflattenSettings 将扁平化 entries 重组为嵌套 map
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// UnflattenSettings 将 setting entries 重组为嵌套 map
func UnflattenSettings(entries map[string]SettingEntry) map[string]any {
	result := make(map[string]any)

	for key, entry := range entries {
		parts := strings.Split(key, ".")
		setNested(result, parts, parseValue(entry.Value, entry.ValueType))
	}

	return result
}

// setNested 在嵌套 map 中设置值，自动创建中间路径
func setNested(m map[string]any, parts []string, value any) {
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if _, ok := m[part]; !ok {
			m[part] = make(map[string]any)
		}
		if nextMap, ok := m[part].(map[string]any); ok {
			m = nextMap
		} else {
			// 已存在的非 map 值被覆盖
			newMap := make(map[string]any)
			m[part] = newMap
			m = newMap
		}
	}
	m[parts[len(parts)-1]] = value
}

// parseValue 根据 value_type 解析字符串值
func parseValue(raw string, valueType string) any {
	switch valueType {
	case "int":
		var v int64
		fmt.Sscanf(raw, "%d", &v)
		return v
	case "float":
		var v float64
		fmt.Sscanf(raw, "%f", &v)
		return v
	case "bool":
		return raw == "true" || raw == "1"
	case "json":
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v
		}
		return raw
	default:
		return raw
	}
}
