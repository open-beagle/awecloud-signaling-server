# Wails v2 → v3 升级变更记录

## 升级状态

| 项目     | 状态      | 版本/说明         |
| -------- | --------- | ----------------- |
| Wails    | ✅ 已完成 | v3.0.0-alpha.55   |
| Go       | ✅ 已完成 | 1.25.0            |
| 系统托盘 | ✅ 已实现 | Wails v3 原生支持 |
| Windows  | ✅ 已测试 | 构建和运行正常    |
| macOS    | ⚠️ 待测试 | 需在 macOS 上构建 |
| 升级完成 | ✅        | 2026-01-03        |

## 升级原因

Wails v2 不支持系统托盘，需要第三方库（如 `energye/systray`），
但在 macOS 上会与 Wails 的 `AppDelegate` 产生符号冲突，无法编译。

Wails v3 原生支持系统托盘，跨平台兼容性好。

## 已完成的变更

### 1. 依赖变更

```go
// v2 (已移除)
github.com/wailsapp/wails/v2 v2.11.0
github.com/energye/systray v1.0.2

// v3 (当前)
github.com/wailsapp/wails/v3 v3.0.0-alpha.55
```

### 2. Go 后端文件变更

| 文件                     | 变更内容                        |
| ------------------------ | ------------------------------- |
| `desktop/main.go`        | 重写应用初始化，使用 v3 API     |
| `desktop/app.go`         | 移除 context，改用 window 对象  |
| `desktop/go.mod`         | 更新依赖，Go 1.25               |
| `desktop/internal/tray/` | 简化为资源文件，逻辑移至 app.go |

### 3. 前端绑定变更

| 变更项     | v2                        | v3                         |
| ---------- | ------------------------- | -------------------------- |
| 绑定目录   | `wailsjs/go/main/`        | `bindings/github.com/.../` |
| 运行时导入 | `wailsjs/runtime/runtime` | `@wailsio/runtime`         |
| 生成命令   | `wails generate module`   | `wails3 generate bindings` |

### 4. 构建系统变更

| 变更项   | v2            | v3                    |
| -------- | ------------- | --------------------- |
| CLI 命令 | `wails`       | `wails3`              |
| 构建命令 | `wails build` | `go build` (直接使用) |
| 开发模式 | `wails dev`   | `wails3 dev`          |
| 配置文件 | `wails.json`  | `Taskfile.yml` + 配置 |

### 5. 新增文件

| 文件                         | 说明              |
| ---------------------------- | ----------------- |
| `desktop/Taskfile.yml`       | Wails v3 任务配置 |
| `desktop/build/Taskfile.yml` | 通用构建任务      |
| `desktop/build/config.yml`   | 开发配置          |
| `desktop/build/windows/`     | Windows 构建任务  |
| `desktop/build/darwin/`      | macOS 构建任务    |
| `desktop/build/linux/`       | Linux 构建任务    |

## 注意事项

### macOS 构建限制

macOS 不支持从 Linux/Windows 交叉编译：

- Wails 需要 CGO 调用 macOS 原生框架（Cocoa/WebKit）
- 必须在 macOS 机器上构建，或使用 GitHub Actions

### API 变更要点

```go
// 窗口操作
// v2: runtime.WindowShow(ctx)
// v3: mainWindow.Show()

// 系统托盘
systray := mainApp.SystemTray.New()
systray.SetIcon(appIcon)
systray.OnClick(func() { mainWindow.Show() })

// 窗口关闭事件
mainWindow.RegisterHook(events.Common.WindowClosing, func(e) {
    e.Cancel()
    mainWindow.Hide()
})
```

## 参考资料

- [Wails v3 文档](https://v3alpha.wails.io/)
- [Wails v3 System Tray](https://v3alpha.wails.io/features/menus/systray/)

---

**创建日期**: 2025-12-29
**升级完成**: 2026-01-03
