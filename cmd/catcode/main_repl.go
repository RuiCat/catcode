package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"catcode/agent/orchestrator"
	"catcode/agent/plan"
	"catcode/agent/role"
)

// runREPL 运行 REPL 交互循环
func runREPL(arch orchestrator.ArchitectInterface, app *Application) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, syscall.SIGINT, syscall.SIGTERM)

	inputCh := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			inputCh <- scanner.Text()
		}
		close(inputCh)
	}()

	for {
		fmt.Print("\n> ")
		select {
		case sig := <-quitCh:
			fmt.Printf("\n收到 %v，正在退出...\n", sig)
			cancel()
			return

		case input, ok := <-inputCh:
			if !ok {
				return
			}
			input = strings.TrimSpace(input)
			if input == "" {
				continue
			}
			if handled := handleCommand(input, arch, app); handled {
				if input == "/quit" || input == "/exit" {
					return
				}
				continue
			}
			fmt.Println()
			ch, err := arch.ProcessInput(ctx, input)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 错误: %v\n", err)
				continue
			}
			for {
				select {
				case <-quitCh:
					fmt.Println("\n⏹ 已中断")
					cancel()
					return
				case text, ok := <-ch:
					if !ok {
						goto doneStream
					}
					fmt.Print(text)
				}
			}
		doneStream:
			fmt.Println()
		}
	}
}

func handleCommand(input string, arch orchestrator.ArchitectInterface, app *Application) bool {
	switch input {
	case "/quit", "/exit":
		return true
	case "/help":
		printHelp()
		return true
	case "/roles":
		listRolesHandler(app)
		return true
	case "/status":
		printStatus(arch, app)
		return true
	case "/clear":
		arch.GetSession().Clear()
		if app.PlanEngine != nil {
			app.PlanEngine = plan.NewEngine(app.Bus)
		}
		fmt.Println("✓ 会话已清空")
		return true
	case "/save":
		if app.Wdb != nil {
			sess := arch.GetSession()
			conv := sess.ToConversationRow()
			msgs := sess.ToMessageRows()
			if err := app.Wdb.SaveConversation(conv, msgs); err != nil {
				fmt.Printf("❌ 保存失败: %v\n", err)
			} else {
				fmt.Println("✓ 会话已保存")
			}
		} else {
			fmt.Println("⚠ 会话存储未启用")
		}
		return true
	}
	return false
}

func printHelp() {
	fmt.Println(`
  ╭────────────────────────────────────────────╮
  │              catcode v` + Version + `                 │
  ├────────────────────────────────────────────┤
  │  /help    显示帮助                          │
  │  /quit    退出程序                          │
  │  /roles   列出已加载的角色                   │
  │  /status  显示当前状态 (池/规划/会话)        │
  │  /clear   清空对话历史和规划                 │
  │  /save    手动保存会话到工作区               │
  ├────────────────────────────────────────────┤
  │  直接输入文本开始与 AI 对话                  │
  │  AI 可调用工具: read/write/edit/glob/grep/  │
  │                bash/diff/task/todo          │
  ╰────────────────────────────────────────────╯`)
}

func printBanner() {
	fmt.Println()
	fmt.Println("  ╭──────────────────────────────────────╮")
	fmt.Println("  │  🐱  catcode  v" + Version + "                 │")
	fmt.Println("  │      TUI AI 编程助手                  │")
	fmt.Println("  ╰──────────────────────────────────────╯")
	fmt.Println()
}

func printCLIHelp() {
	fmt.Print(`
  ╭──────────────────────────────────────╮
  │  🐱  catcode  v` + Version + `                 │
  │      TUI AI 编程助手                  │
  ╰──────────────────────────────────────╯

用法: catcode [选项]

选项:
  -r, --repl            使用 REPL 模式而非 TUI
  -m, --model <name>    指定使用的模型
  -t, --temperature <n> 设置温度参数
  -w, --workspace <dir> 指定工作区目录
  -h, --help            显示此帮助

配置优先级（从低到高）:
  go:embed 默认值 → DB settings 表
  → 环境变量 (CATCODE_*) → CLI 参数

环境变量:
  CATCODE_MODEL          默认模型
  CATCODE_WORKSPACE      工作区目录
  DEEPSEEK_API_KEY       DeepSeek API 密钥
  OPENAI_API_KEY         通用 API 密钥（回退）
  CATCODE_BASE_URL       API 基础 URL
  CATCODE_THEME          TUI 主题 (dark/light)
`)
}

func listRolesHandler(app *Application) {
	fmt.Println("\n已加载的角色:")
	fmt.Println(strings.Repeat("─", 50))
	for _, inst := range app.RoleReg.GetAllActive() {
		icon := "🔧"
		if inst.Def.Type == role.RoleCompanion {
			icon = "💝"
		}
		mode := string(inst.Def.Mode)
		switch inst.Def.Mode {
		case role.ModePrimary:
			mode = "⭐ 主智能体"
		case role.ModeSubAgent:
			mode = "📎 子智能体"
		case role.ModeBackground:
			mode = "💤 后台"
		}
		fmt.Printf("  %s %-15s %s\n", icon, inst.Def.DisplayName, mode)
		if len(inst.Def.Tools) > 0 {
			fmt.Printf("      工具: %s\n", strings.Join(inst.Def.Tools, ", "))
		}
	}
	fmt.Printf("\n共 %d 个角色\n", app.RoleReg.Count())
}

func printStatus(arch orchestrator.ArchitectInterface, app *Application) {
	fmt.Println("\n当前状态:")
	fmt.Println(strings.Repeat("─", 40))

	sess := arch.GetSession()
	fmt.Printf("📝 会话: %d 条消息 (~%d tokens)\n",
		sess.MessageCount(), sess.TokenCount())

	fmt.Printf("🔧 工具: %d 个已注册\n", sess.ToolCount())

	if app.AgentPool != nil {
		fmt.Printf("🤖 子智能体池: %d 活跃 / %d 总实例\n",
			app.AgentPool.ActiveCount(), app.AgentPool.TotalCount())
	}

	if app.PlanEngine != nil {
		plan := app.PlanEngine.GetActivePlan()
		if plan != nil {
			fmt.Printf("📋 规划: %s (%.0f%% 完成)\n",
				plan.Description, app.PlanEngine.Progress(plan.ID)*100)
		} else {
			fmt.Println("📋 规划: 无活跃规划")
		}
	}

	fmt.Printf("📡 事件总线: %d 订阅者\n", app.Bus.SubscriberCount())
}
