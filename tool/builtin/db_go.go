package builtin

import (
	"fmt"
	"strings"
	"reflect"

	cerr "catcode/core/errors"
	"catcode/data/storage"
	"catcode/tool"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DB Query — 数据库只读查询
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DBQueryTool 创建数据库查询工具（只读 SELECT）
func DBQueryTool(wdb storage.WorkspaceDB) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "db_query",
			Description: "执行 SQL 查询（仅支持 SELECT）。可查询工作区数据库中的所有表：settings, agent_definitions, conversations, messages, memory, analysis, context_snapshots, file_snapshots, scheduled_tasks。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"sql": {Type: "string", Description: "SELECT 查询语句"},
				},
				Required: []string{"sql"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			sql, _ := args["sql"].(string)
			sql = strings.TrimSpace(sql)
			if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") {
				return "", cerr.Newf("db_query: 仅支持 SELECT 查询")
			}
			rows, err := wdb.DB().Query(sql)
			if err != nil {
				return "", cerr.Wrap(err, "db_query")
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			var result strings.Builder
			result.WriteString(strings.Join(cols, " | ") + "\n")
			result.WriteString(strings.Repeat("-", len(strings.Join(cols, " | "))) + "\n")

			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}

			// 检测 settings 表的 key/value 列，用于过滤敏感信息
			keyIdx := -1
			valIdx := -1
			for j, col := range cols {
				if col == "key" {
					keyIdx = j
				} else if col == "value" {
					valIdx = j
				}
			}

			count := 0
			for rows.Next() && count < 50 {
				if err := rows.Scan(ptrs...); err != nil {
					continue
				}

				// 过滤敏感信息：如果 key 列包含 api_key，隐藏 value 列
				if keyIdx >= 0 && valIdx >= 0 {
					keyStr := fmt.Sprintf("%v", values[keyIdx])
					if strings.HasSuffix(keyStr, ".api_key") || keyStr == "api_key" {
						switch values[valIdx].(type) {
						case []byte:
							values[valIdx] = []byte("[敏感信息已隐藏]")
						default:
							values[valIdx] = "[敏感信息已隐藏]"
						}
					}
				}
				var row []string
				for _, v := range values {
					switch val := v.(type) {
					case []byte:
						s := string(val)
						if len(s) > 60 {
							s = s[:60] + "..."
						}
						row = append(row, s)
					case nil:
						row = append(row, "NULL")
					default:
						s := fmt.Sprintf("%v", val)
						if len(s) > 60 {
							s = s[:60] + "..."
						}
						row = append(row, s)
					}
				}
				result.WriteString(strings.Join(row, " | ") + "\n")
				count++
			}
			if count >= 50 {
				result.WriteString(fmt.Sprintf("\n... 还有更多行 (仅显示前50行)"))
			}
			return result.String(), nil
		},
	}
}

// DBExecTool 创建数据库执行工具（INSERT/UPDATE/DELETE）
func DBExecTool(wdb storage.WorkspaceDB) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "db_exec",
			Description: "执行数据库写操作（INSERT/UPDATE/DELETE）。可修改 settings 配置、agent_definitions 智能体定义、scheduled_tasks 周期任务等。返回影响行数。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"sql": {Type: "string", Description: "INSERT/UPDATE/DELETE 语句"},
				},
				Required: []string{"sql"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			rawSQL, _ := args["sql"].(string)
			upperSQL := strings.TrimSpace(strings.ToUpper(rawSQL))
			if strings.HasPrefix(upperSQL, "DROP") ||
				strings.HasPrefix(upperSQL, "ALTER") ||
				strings.HasPrefix(upperSQL, "TRUNCATE") {
				return "", cerr.Newf("db_exec: DROP/ALTER/TRUNCATE 操作被禁止")
			}
			if strings.HasPrefix(upperSQL, "DELETE") && !strings.Contains(upperSQL, "WHERE") {
				return "", cerr.Newf("db_exec: DELETE 必须包含 WHERE 条件")
			}
			result, err := wdb.DB().Exec(rawSQL)
			if err != nil {
				return "", cerr.Wrap(err, "db_exec")
			}
			affected, _ := result.RowsAffected()
			return fmt.Sprintf("✓ 执行成功，影响 %d 行", affected), nil
		},
	}
}

// DBTablesTool 列出数据库表结构
func DBTablesTool(wdb storage.WorkspaceDB) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "db_tables",
			Description: "列出数据库所有表及字段信息。",
			Parameters:  tool.MustMarshalSchema(tool.Schema{Type: "object", Properties: map[string]tool.Property{}}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			rows, err := wdb.DB().Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
			if err != nil {
				return "", err
			}

			// 先收集所有表名（避免嵌套查询）
			var tableNames []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					continue
				}
				tableNames = append(tableNames, name)
			}
			rows.Close()

			var result strings.Builder
			for _, name := range tableNames {
				result.WriteString(fmt.Sprintf("\n📋 %s\n", name))
				// 获取列信息（此时外层 rows 已关闭，不会死锁）
				colRows, err := wdb.DB().Query(fmt.Sprintf("PRAGMA table_info(%s)", name))
				if err != nil {
					continue
				}
				var cols []string
				for colRows.Next() {
					var cid int
					var colName, colType string
					var notNull int
					var defVal *string
					var pk int
					if err := colRows.Scan(&cid, &colName, &colType, &notNull, &defVal, &pk); err != nil {
						continue
					}
					pkMark := ""
					if pk > 0 {
						pkMark = " PK"
					}
					cols = append(cols, fmt.Sprintf("%s(%s%s)", colName, colType, pkMark))
				}
				colRows.Close()
				result.WriteString(strings.Join(cols, ", "))
			}
			return result.String(), nil
		},
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Go Run — yaegi 动态脚本执行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GoRunTool 创建 Go 脚本执行工具（基于 yaegi 解释器）
func GoRunTool(wdb storage.WorkspaceDB) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "go_run",
			Description: "执行 Go 脚本（使用 yaegi 解释器）。可访问工作区数据库（通过 wdb 变量）。用于数据查询、批量更新、自定义逻辑等。脚本需包含 package main 和 func main()。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"script": {Type: "string", Description: "Go 源代码（需 package main + func main()）"},
				},
				Required: []string{"script"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			script, _ := args["script"].(string)
			if script == "" {
				return "", cerr.Newf("go_run: script 不能为空")
			}
			return executeGoScript(script, wdb)
		},
	}
}

// executeGoScript 使用 yaegi 执行 Go 脚本
func executeGoScript(script string, wdb storage.WorkspaceDB) (string, error) {
	return runYaegiScript(script, wdb)
}

// runYaegiScript 实际的 yaegi 执行逻辑
func runYaegiScript(script string, wdb storage.WorkspaceDB) (string, error) {
	i := interp.New(interp.Options{Unrestricted: false})
	i.Use(stdlib.Symbols)

	if wdb != nil {
		i.Use(interp.Exports{
			"catcode/wdb/wdb": {
				"DB": reflect.ValueOf(wdb),
			},
		})
	}

	// Step 1: Package and imports（不嵌入用户脚本）
	_, err := i.Eval(`package main

import (
	"fmt"
	"strings"
)`)
	if err != nil {
		errMsg := err.Error()
		if idx := strings.LastIndex(errMsg, ": "); idx > 0 {
			errMsg = errMsg[idx+2:]
		}
		return "", cerr.Newf("go_run: %s", errMsg)
	}

	// Step 2: Output collector and helper functions
	_, err = i.Eval(`var _output strings.Builder

func println(args ...interface{}) {
	for _, a := range args {
		_output.WriteString(fmt.Sprint(a))
	}
	_output.WriteString("\n")
}

func printf(format string, args ...interface{}) {
	_output.WriteString(fmt.Sprintf(format, args...))
}`)
	if err != nil {
		errMsg := err.Error()
		if idx := strings.LastIndex(errMsg, ": "); idx > 0 {
			errMsg = errMsg[idx+2:]
		}
		return "", cerr.Newf("go_run: %s", errMsg)
	}

	// Step 3: 直接在解释器全局作用域执行用户脚本（不嵌入任何模板）
	_, err = i.Eval(script)
	if err != nil {
		errMsg := err.Error()
		if idx := strings.LastIndex(errMsg, ": "); idx > 0 {
			errMsg = errMsg[idx+2:]
		}
		return "", cerr.Newf("go_run: %s", errMsg)
	}

	// Step 4: Collect output
	v, evalErr := i.Eval("_output.String()")
	if evalErr != nil {
		return "✓ 脚本执行完成（无输出）", nil
	}
	result := strings.TrimSpace(fmt.Sprintf("%v", v.Interface()))
	if result == "" {
		return "✓ 脚本执行完成", nil
	}
	return result, nil
}
