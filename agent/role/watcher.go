package role

import (
	"path/filepath"
	"time"

	"catcode/core/config/loader"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色文件热重载
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// WatchUserRoles 监视 .catcode/roles/ 目录变化
// 当检测到角色文件变更时，调用 onChange 回调触发重新加载
func WatchUserRoles(workDir string, onChange func()) error {
	rolesDir := filepath.Join(workDir, ".catcode", "roles")
	watcher := loader.NewDirWatcher(rolesDir, "*", 3*time.Second)
	return watcher.Start(func(evt loader.ChangeEvent) {
		if onChange != nil {
			onChange()
		}
	})
}
