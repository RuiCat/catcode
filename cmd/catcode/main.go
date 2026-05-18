// catcode — 纯 Go、工作区隔离的 TUI AI 辅助编程工具
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"catcode/agent/orchestrator"
	"catcode/agent/plan"
	agent "catcode/agent/pool"
	"catcode/agent/role"
	"catcode/ai/llm"
	"catcode/core/config"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/mcp"
	"catcode/plugin"
	"catcode/schedule"
	"catcode/tool/builtin"
	"catcode/ui/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// Version 当前版本号
const Version = "0.9.2"

// Application 应用依赖容器，集中管理所有模块引用
type Application struct {
	Wdb           storage.WorkspaceDB
	MemoryService storage.MemoryService
	Bus           event.EventBus
	RoleReg       role.RegistryInterface
	AgentPool     agent.PoolInterface
	PlanEngine    plan.PlanEngineInterface
	Provider      llm.Provider
	Scheduler     *schedule.Scheduler
	PluginMgr     *plugin.Manager
	McpMgr        *mcp.Manager
	WorkDir       string
	TUIProgram    *tea.Program
	TUIModel      *tui.Model
}

func main() {
	// 解析命令行参数
	useREPL := false
	cliModel := ""
	cliTemp := 0.0
	workDir := ""

	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch arg {
		case "--repl", "-r":
			useREPL = true
		case "--model", "-m":
			if i+1 < len(os.Args) {
				i++
				cliModel = os.Args[i]
			}
		case "--temperature", "-t":
			if i+1 < len(os.Args) {
				i++
				fmt.Sscanf(os.Args[i], "%f", &cliTemp)
			}
		case "--workspace", "-w":
			if i+1 < len(os.Args) {
				i++
				workDir = os.Args[i]
			}
		case "--help", "-h":
			printCLIHelp()
			os.Exit(0)
		}
	}

	// ── 初始化横幅 ──
	printBanner()

	app := &Application{}

	// 0. 发现工作区
	if workDir == "" {
		workDir = discoverWorkspace()
	}
	app.WorkDir = workDir
	fmt.Printf("  📁 工作区              %s\n", workDir)

	// 1. 打开工作区数据库
	wdb, isNew, err := storage.OpenWorkspace(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ 工作区数据库打开失败: %v\n", err)
		os.Exit(1)
	}
	app.Wdb = wdb

	// 写入种子数据 / 迁移旧格式配置（Seed 内部自适应）
	if err := wdb.Seed(); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ 种子数据写入失败: %v\n", err)
	} else if isNew {
		fmt.Println("  🌱 已初始化默认配置和角色")
	}
	// 加载自定义守卫规则
	if err := builtin.LoadGuardPatterns(wdb); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ 守卫规则加载失败: %v\n", err)
	}

	// 1.5 初始化记忆服务
	app.MemoryService = storage.NewMemoryService(app.Wdb)

	// 1.6 加载猫猫状态
	builtin.LoadCompanionState(app.Wdb)

	// 2. 加载配置（DB + 环境变量 + CLI）
	cfg, err := config.LoadFromWorkspace(wdb, cliModel, cliTemp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ 配置加载失败: %v\n", err)
		os.Exit(1)
	}
	primaryTemp := 0.0
	if defaultName := cfg.DefaultAgent; defaultName != "" {
		if agentCfg, ok := cfg.Agents[defaultName]; ok {
			primaryTemp = agentCfg.Temperature
		}
	} else {
		for _, agentCfg := range cfg.Agents {
			primaryTemp = agentCfg.Temperature
			break
		}
	}
	fmt.Printf("  📋 配置已加载         模型: %s | 温度: %.1f\n", cfg.Model, primaryTemp)

	// 3. 初始化事件总线
	app.Bus = event.NewBus()

	// 3.1 初始化空闲调度器（30秒无操作触发）
	app.Scheduler = schedule.NewScheduler(schedule.NewIdleDetector(30 * time.Second))
	schedule.LoadDBTasks(app.Scheduler, app.Wdb, func() bool {
		return app.AgentPool != nil && app.AgentPool.ActiveCount() > 0
	})

	// 4. 初始化角色系统（从 DB + .catcode/roles/ 加载）
	app.RoleReg = role.NewRegistry(app.Bus)
	if err := app.RoleReg.LoadFromWorkspace(wdb, workDir); err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ 角色加载失败: %v\n", err)
	} else {
		primary := app.RoleReg.GetPrimary()
		if primary != nil {
			fmt.Printf("  🎭 角色已加载          %d 个角色 (主智能体: %s)\n",
				app.RoleReg.Count(), primary.Def.DisplayName)
		}
	}

	if app.RoleReg.GetPrimary() == nil {
		fmt.Fprintf(os.Stderr, "  ❌ 未找到主智能体角色\n")
		os.Exit(1)
	}

	// 4.1 启用角色文件热重载（监听 .catcode/roles/ 目录）
	if err := role.WatchUserRoles(workDir, func() {
		fmt.Fprintf(os.Stderr, "\n  ♻ 角色文件变更，重新加载...\n")
		if app.RoleReg != nil && app.Wdb != nil {
			if err := app.RoleReg.ReloadFromWorkspace(app.Wdb, workDir); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ 角色重载失败: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "  ✓ 角色已重载 (%d 个)\n", app.RoleReg.Count())
			}
		}
	}); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ 角色热重载启动失败: %v\n", err)
	} else {
		fmt.Println("  🔄 角色热重载已启用    .catcode/roles/")
	}

	// 5. 初始化所有 Provider + ProviderRegistry（多模型支持）
	providers := initProviders(cfg, wdb)
	app.Provider = providers.Default()

	// 6. 初始化子智能体池
	app.AgentPool = agent.NewPool(agent.PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  agent.AgentConfigsFromDB(wdb),
		ToolFactory:   builtin.ToolFactory(app.Provider, app.RoleReg, app.Bus, app.Wdb, app.MemoryService),
	}, providers, app.Bus)
	app.AgentPool.SetWorkspaceDB(app.Wdb, 65536)
	app.AgentPool.SetMemoryService(app.MemoryService)
	app.AgentPool.SetGuardReviewer()
	app.AgentPool.SetWorkDir(app.WorkDir)

	// 6.1 初始化规划引擎（必须在 architect 创建之后、registerBuiltinTools 之前）
	app.PlanEngine = plan.NewEngine(app.Bus)

	// 7. 创建 Architect（从角色定义获取模型+上下文配置）
	archCfg := orchestrator.DefaultArchitectConfig()
	archCfg.Model = cfg.Model
	if primary := app.RoleReg.GetPrimary(); primary != nil {
		archCfg.SystemPrompt = primary.Def.SystemPrompt
		if primary.Def.Model.Name != "" {
			archCfg.Model = llm.BuildModelName(primary.Def.Model.Provider, primary.Def.Model.Name)
		}
		if primary.Def.Temperature > 0 {
			archCfg.Temperature = primary.Def.Temperature
		}
		// 从角色 model.limit 读取上下文窗口和最大输出
		if primary.Def.Model.Limit != nil {
			if primary.Def.Model.Limit.Context > 0 {
				archCfg.ContextLimit = primary.Def.Model.Limit.Context
			}
			if primary.Def.Model.Limit.Output > 0 {
				archCfg.MaxOutput = primary.Def.Model.Limit.Output
			}
		}
	}
	// 主智能体也通过 ProviderRegistry 获取 provider
	archProvider, _ := providers.ResolveModel(archCfg.Model)
	arch := orchestrator.NewArchitect(archCfg, archProvider, app.RoleReg, app.Bus, app.AgentPool, app.Wdb, app.MemoryService)
	arch.SetWorkDir(app.WorkDir)

	// 尝试从 DB 恢复上一次会话
	if app.Wdb != nil {
		conv, msgs, err := app.Wdb.LoadConversation("architect-main")
		if err == nil && len(msgs) > 0 {
			arch.LoadHistory(msgs)
			// 恢复会话级别的元数据
			if conv.Summary != "" {
				arch.GetSession().SetSummary(conv.Summary)
			}
			arch.InjectMemoryIndex()
			fmt.Printf("  📜 历史会话已恢复     %d 条消息\n", len(msgs))
		}
	}

	// 8. 注册内置工具
	registerBuiltinTools(arch, app)
	arch.SetPlanEngine(app.PlanEngine)

	// 8.1 加载插件（.catcode/plugins/）
	pluginCtx := &plugin.PluginContext{WorkDir: workDir, Bus: app.Bus}
	app.PluginMgr = plugin.NewManager(filepath.Join(workDir, ".catcode", "plugins"), pluginCtx)
	if plugins, err := app.PluginMgr.LoadAll(); err == nil && len(plugins) > 0 {
		for _, p := range plugins {
			fmt.Printf("  🔌 插件已加载          %s v%s (%s)\n", p.Name, p.Version, p.Type)
		}

		// 注册插件提供的工具到主智慧体
		for _, t := range app.PluginMgr.GetToolInstances(app.Bus) {
			if err := arch.RegisterTool(t); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ 插件工具注册失败 %s: %v\n", t.Function.Name, err)
			} else {
				fmt.Printf("  🛠 插件工具已注册      %s\n", t.Function.Name)
			}
		}

		// 注册插件提供的角色到角色系统
		for _, def := range app.PluginMgr.GetRoleInstances() {
			if err := app.RoleReg.Register(def); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ 插件角色注册失败 %s: %v\n", def.Name, err)
			} else {
				fmt.Printf("  🎭 插件角色已注册      %s\n", def.DisplayName)
			}
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ 插件加载失败: %v\n", err)
	}

	// 8.2 连接 MCP 服务器（settings.mcp_servers）
	app.McpMgr = mcp.NewManager()
	if mcpServersJSON, _, err := app.Wdb.GetSetting("mcp.servers"); err == nil && mcpServersJSON != "" {
		var servers []mcp.ServerConfig
		if json.Unmarshal([]byte(mcpServersJSON), &servers) == nil {
			for _, srv := range servers {
				tools, err := app.McpMgr.ConnectServer(srv)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ MCP %s 连接失败: %v\n", srv.Name, err)
					if app.Wdb != nil {
						buf := make([]byte, 2048)
						n := runtime.Stack(buf, false)
						_ = app.Wdb.LogError("网络", "warning", fmt.Sprintf("MCP %s: %v", srv.Name, err), string(buf[:n]), "startup", "")
					}
					continue
				}
				for _, t := range tools {
					arch.RegisterTool(t)
				}
				fmt.Printf("  🔗 MCP 已连接           %s (%d 工具)\n", srv.Name, len(tools))
			}
		}
	}

	// 10. 注册事件回调
	registerEventCallbacks(app)

	// ── 就绪状态 ──
	companionCount := 0
	for _, inst := range app.RoleReg.GetAllActive() {
		if inst.Def.Type == role.RoleCompanion {
			companionCount++
		}
	}
	fmt.Printf("  🚀 主智能体就绪        工具: %d | 子智能体: %d | 陪伴: %d\n",
		arch.GetSession().ToolCount(), app.AgentPool.TotalCount(), companionCount)
	fmt.Println(strings.Repeat("─", 50))

	if useREPL {
		fmt.Println("  💬 输入消息开始对话  /quit 退出  /help 查看帮助")
		fmt.Println()
		runREPL(arch, app)
		cleanup(arch, app)
	} else {
		runTUI(arch, cfg, app)
		cleanup(arch, app)
	}
	fmt.Println("\n👋 再见！🐱")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工作区发现
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// discoverWorkspace 从当前目录向上搜索工作区
func discoverWorkspace() string {
	// 1. 环境变量优先
	if ws := os.Getenv("CATCODE_WORKSPACE"); ws != "" {
		return ws
	}

	// 2. 从当前目录向上搜索 .catcode/ 或 .git/
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	dir := cwd
	for {
		// 检查 .catcode/ 目录（已初始化的工作区）
		if info, err := os.Stat(filepath.Join(dir, ".catcode")); err == nil && info.IsDir() {
			return dir
		}
		// 检查 .git/ 目录（git 仓库根目录）
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// 已到文件系统根目录
			break
		}
		dir = parent
	}

	// 3. 回退到当前目录
	return cwd
}

// cleanup 退出清理
func cleanup(arch orchestrator.ArchitectInterface, app *Application) {
	if app.Wdb != nil {
		fmt.Print("正在保存会话... ")
		sess := arch.GetSession()
		conv := sess.ToConversationRow()
		msgs := sess.ToMessageRows()
		if err := app.Wdb.SaveConversation(conv, msgs); err != nil {
			fmt.Printf("失败: %v\n", err)
		} else {
			fmt.Println("完成")
		}
		// 保存侧边栏宽度
		if app.TUIModel != nil {
			w := app.TUIModel.SidebarWidth()
			app.Wdb.SetSetting("tui.sidebar_width", fmt.Sprintf("%d", w), "int", "user")
		}
		app.Wdb.Close()
	}
	if app.PlanEngine != nil {
		app.PlanEngine.Close()
	}
	if app.AgentPool != nil {
		app.AgentPool.Shutdown()
	}
}
