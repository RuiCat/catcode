# catcode 安全审计与代码审查报告

## 修复状态总结

| 编号 | 严重程度 | 标题 | 状态 |
|------|---------|------|------|
| C-01 | 严重 | 插件加载器 yaegi Unrestricted 模式允许任意代码执行 | ✅ 已修复 (v0.9.2) |
| C-02 | 严重 | MCP 客户端 call() 方法存在无限循环死锁风险 | ✅ 已修复 (v0.9.2) |
| C-03 | 严重 | Go 脚本执行存在代码注入风险 | ✅ 已修复 (v0.9.2) |
| C-04 | 严重 | EventBus publishDepth 存在竞态条件 | ✅ 已修复 (v0.9.2) |
| C-05 | 严重 | reviewWithGuard 绕过 Session 锁直接修改 Messages | ✅ 已修复 (v0.9.2) |
| C-06 | 严重 | API Key 明文存储在工作区数据库 | ✅ 已修复 (v0.9.2) |
| C-07 | 严重 | task 工具使用 context.Background() 绕过取消 | ✅ 已修复 (v0.9.2) |
| W-01 | 警告 | 文件路径遍历存在符号链接绕过 | ✅ 已修复 (v0.9.2) |
| W-02 | 警告 | bash 工具暴露完整环境变量 | ✅ 已修复 (v0.9.2) |
| W-03 | 警告 | MCP Close() 未强制终止子进程 | ✅ 已修复 (v0.9.2) |
| W-04 | 警告 | db_exec 仅阻止 DROP/ALTER，其他破坏性操作未阻止 | ✅ 已修复 (v0.9.2) |
| W-05 | 警告 | 记忆更新 goroutine 遗漏 scope 限定 | ✅ 已修复 (v0.9.2) |
| W-06 | 警告 | ErrorCollector 编号不一致 | ✅ 已修复 (v0.9.2) |
| W-07 | 警告 | Schema 迁移重复列错误被静默吞没 | ✅ 已修复 (v0.9.2) |
| W-08 | 警告 | MCP 子进程 stderr 被丢弃 | ✅ 已修复 (v0.9.2) |
| S-01 | 建议 | 工具注册器缺少权限分级 | ⬜ 未修复 (下一版本) |
| S-02 | 建议 | workspaceDBImpl 公开暴露底层锁方法 | ✅已修复 |
| S-03 | 建议 | runTUI/registerBuiltinTools 函数过长 | ✅已修复 |
| S-04 | 建议 | MCP reqID 非原子操作 | ✅已修复 |
| S-05 | 建议 | convertToolCalls 重复实现 | ✅已修复 |

## 统计摘要

| 严重程度 | 数量 |
|---------|------|
| 严重 (Critical) | 7 |
| 警告 (Warning) | 8 |
| 建议 (Suggestion) | 5 |
| **总计** | **20** |

---

## 严重 (Critical)

### C-01: 插件加载器 yaegi Unrestricted 模式允许任意代码执行 ✅ 已修复 (v0.9.2)

- **文件**: `plugin/loader.go:67-69`
- **问题**: yaegi 解释器配置为 `Unrestricted: true`，插件可执行 unsafe/syscall/reflect 等受限操作
- **影响**: RCE；若加载恶意插件可完全控制宿主进程
- **建议**: 改为 `Unrestricted: false`，仅允许使用已注册符号表

---

### C-02: MCP 客户端 call() 方法存在无限循环死锁风险 ✅ 已修复 (v0.9.2)

- **文件**: `mcp/client.go:215-227`
- **问题**: `for {}` 忙循环无超时，若服务器不响应则永久阻塞且持有 `c.mu` 锁
- **影响**: goroutine 泄漏 + 死锁，整个 MCP 客户端卡死
- **建议**: 增加超时机制和最大迭代次数

---

### C-03: Go 脚本执行存在代码注入风险 ✅ 已修复 (v0.9.2)

- **文件**: `tool/builtin/db_go.go:211-236`
- **问题**: 用户脚本通过 `fmt.Sprintf` 模板直接嵌入 `main()` 函数体，可构造恶意代码逃逸
- **影响**: 可访问 `wdb` 变量（完整 WorkspaceDB 接口），执行任意 SQL
- **建议**: 不要将用户脚本嵌入模板字符串，或对脚本进行安全转义

---

### C-04: EventBus publishDepth 存在竞态条件 ✅ 已修复 (v0.9.2)

- **文件**: `core/event/event.go:46,92-101`
- **问题**: `publishDepth` 没有同步原语保护，`PublishAsync` 多 goroutine 并发导致竞态
- **影响**: 递归深度限制被绕过，事件系统不可预期行为
- **建议**: 改为 `atomic.Int32`

---

### C-05: reviewWithGuard 绕过 Session 锁直接修改 Messages ✅ 已修复 (v0.9.2)

- **文件**: `agent/subagent/base.go:894-899`
- **问题**: 直接赋值 `sess.Messages = sess.Messages[:0]` 绕过 `Session.mu` 锁
- **影响**: 并发读写竞态，可能导致 panic
- **建议**: 通过 Session 公开方法完成清空操作

---

### C-06: API Key 明文存储在工作区数据库 ✅ 已修复 (v0.9.2)

- **文件**: `cmd/catcode/main.go:783-784`
- **问题**: API Key 明文写入 SQLite，LLM 可通过 `db_query` 工具读取
- **影响**: prompt injection + db_query 可泄露 API Key
- **建议**: 加密存储或使用系统凭据存储

---

### C-07: task 工具使用 context.Background() 绕过取消 ✅ 已修复 (v0.9.2)

- **文件**: `cmd/catcode/main.go:856`
- **问题**: `ExecuteAsync` 使用 `context.Background()`，用户取消后子智能体继续执行
- **影响**: 浪费 token 和资源
- **建议**: 传递父 context 而非 `context.Background()`

---

## 警告 (Warning)

### W-01: 文件路径遍历存在符号链接绕过 ✅ 已修复 (v0.9.2)

- **文件**: `tool/builtin/read_tool.go:44-46`（同 write/edit）
- **问题**: 仅检查 `..`，未解析符号链接
- **影响**: 可通过符号链接读取工作区外文件
- **建议**: 使用 `filepath.EvalSymlinks` 解析

---

### W-02: bash 工具暴露完整环境变量 ✅ 已修复 (v0.9.2)

- **文件**: `tool/builtin/bash_tool.go:87`
- **问题**: `cmd.Env = os.Environ()` 传递所有环境变量包括 API Key
- **影响**: 凭据泄露
- **建议**: 使用环境变量白名单

---

### W-03: MCP Close() 未强制终止子进程 ✅ 已修复 (v0.9.2)

- **文件**: `mcp/client.go:103-106`
- **问题**: 仅 `Wait()` 无超时，子进程不退出时永久阻塞
- **影响**: 资源泄漏，僵尸进程
- **建议**: 增加超时后 `Kill()` 机制

---

### W-04: db_exec 仅阻止 DROP/ALTER，其他破坏性操作未阻止 ✅ 已修复 (v0.9.2)

- **文件**: `tool/builtin/db_go.go:102-107`
- **问题**: DELETE/UPDATE/INSERT 未受限制
- **影响**: 可能误删或篡改数据
- **建议**: 表级别白名单限制

---

### W-05: 记忆更新 goroutine 遗漏 scope 限定 ✅ 已修复 (v0.9.2)

- **文件**: `data/storage/memory.go:67-71`
- **问题**: `UPDATE WHERE key = ?` 未检查 scope，跨 scope 冲突
- **影响**: 不同 scope 下相同 key 的记忆互相覆盖
- **建议**: 增加 scope 条件

---

### W-06: ErrorCollector 编号不一致 ✅ 已修复 (v0.9.2)

- **文件**: `core/errors/collector.go:27-38`
- **问题**: 先递增 `errorCount` 再检查上限，`FormatFeedback` 编号与实际不符
- **影响**: 错误编号偏移
- **建议**: 调整递增与检查的顺序

---

### W-07: Schema 迁移重复列错误被静默吞没 ✅ 已修复 (v0.9.2)

- **文件**: `data/storage/schema.go:275-278`
- **问题**: 仅匹配 "duplicate column" 字符串，其他错误也一并忽略
- **影响**: 真实 schema 错误被隐藏
- **建议**: 精确匹配或记录日志

---

### W-08: MCP 子进程 stderr 被丢弃 ✅ 已修复 (v0.9.2)

- **文件**: `mcp/client.go:59`
- **问题**: 子进程 stderr 未捕获或重定向到日志
- **影响**: 调试困难，错误信息丢失
- **建议**: 将 stderr 重定向到日志输出

---

## 建议 (Suggestion)

### S-01: 工具注册器缺少权限分级 ⬜ 未修复 (下一版本)

- 当前所有工具平等注册，无权限区分。建议增加工具权限等级（只读/读写/敏感），在智能体创建时按需分配。

---

### S-02: workspaceDBImpl 公开暴露底层锁方法 ✅ 已修复 (v0.9.2)

已删除 `Lock()`/`Unlock()` 方法，调用方不再能直接访问底层锁机制。

- `Lock()`/`Unlock()` 等方法直接暴露了底层 `*sql.DB` 的锁机制，调用者可能误用。建议封装为更高层的操作。

---

### S-03: runTUI/registerBuiltinTools 函数过长 ✅ 已修复 (v0.9.2)

已将 `runTUI` 拆分为 `buildInputHandler` + `loadTUIState` 子函数。

- `cmd/catcode/main.go` 中 `runTUI` 和 `registerBuiltinTools` 函数体量过大，建议拆分为多个子函数。

---

### S-04: MCP reqID 非原子操作 ✅ 已修复 (v0.9.2)

已将 `reqID` 从普通 `int` 改为 `atomic.Int64`。

- `mcp/client.go` 中 `reqID` 使用普通 `int` 递增，在并发调用时可能产生竞态。建议改为 `atomic.Int64`。

---

### S-05: convertToolCalls 重复实现 ✅已修复

- `convertToolCalls` 逻辑在多处重复（agent 与 tool 包各有一份），建议抽取为公共函数。

---

## 修复优先级建议

| 优先级 | 编号 | 原因 |
|--------|------|------|
| P0（立即修复） | C-01, C-02, C-03, C-06 | 直接导致 RCE、死锁或凭据泄露 |
| P1（尽快修复） | C-04, C-05, C-07, W-01, W-02 | 可能导致竞态崩溃或安全边界绕过 |
| P2（计划修复） | W-03 ~ W-08, S-01 ~ S-05 | 代码质量与防御性改进 |
