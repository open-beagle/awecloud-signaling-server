# Desktop 系统托盘功能设计文档

## 1. 概述

为 Desktop 客户端添加系统托盘功能，实现点击窗口右上角最小化按钮时，窗口隐藏到系统托盘区域。

**目标平台**: Windows、macOS、Linux

**核心功能**:

1. 点击右上角最小化按钮 → 窗口隐藏到系统托盘
2. 点击右上角关闭按钮 → 直接退出应用
3. 双击/单击托盘图标 → 恢复窗口显示
4. 托盘图标右键菜单 → 显示窗口 / 退出

## 2. 技术方案

### 2.1 Wails v2 系统托盘支持现状

根据 [Wails GitHub Issue #1521](https://github.com/wailsapp/wails/issues/1521)：

- **Wails v2** 没有原生系统托盘支持
- **Wails v3** 已实现原生系统托盘（标签：`Implemented in v3`）
- **Wails v2 的解决方案**：使用第三方库 `energye/systray` 或 `fyne-io/systray`

### 2.2 推荐方案

使用 **`github.com/energye/systray`** 库，这是社区验证过的方案，与 Wails v2 集成良好。

| 平台    | 支持状态                                                      |
| ------- | ------------------------------------------------------------- |
| Windows | ✅ 完全支持                                                   |
| Linux   | ✅ 支持（需要 DBus/AppIndicator）                             |
| macOS   | ⚠️ 有冲突（AppDelegate 重复定义），需要使用 `fyne-io/systray` |

### 2.3 依赖安装

```bash
# Windows/Linux
go get github.com/energye/systray

# macOS（如需支持）
go get fyne.io/systray
```

## 3. 实现方案

### 3.1 文件结构

```
desktop/
├── internal/
│   └── tray/
│       ├── tray.go           # 系统托盘管理
│       └── icon.go           # 托盘图标资源（嵌入）
├── build/
│   └── tray/
│       ├── icon.ico          # Windows 托盘图标
│       └── icon.png          # Linux 托盘图标
```

### 3.2 核心代码示例

```go
package main

import (
    "context"
    "os"

    "github.com/energye/systray"
    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
    "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/tray/icon.ico
var trayIcon []byte

type App struct {
    ctx context.Context
}

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    // 启动系统托盘
    go systray.Run(a.onTrayReady, a.onTrayExit)
}

func (a *App) onTrayReady() {
    systray.SetIcon(trayIcon)
    systray.SetTitle("AWECloud Signaling")
    systray.SetTooltip("AWECloud Signaling Desktop")

    // 添加菜单项
    showItem := systray.AddMenuItem("显示窗口", "显示主窗口")
    systray.AddSeparator()
    exitItem := systray.AddMenuItem("退出", "退出应用")

    // 单击托盘图标显示窗口
    systray.SetOnClick(func() {
        runtime.WindowShow(a.ctx)
    })

    // 右键显示菜单
    systray.SetOnRClick(func(menu systray.IMenu) {
        menu.ShowMenu()
    })

    // 菜单项点击事件
    showItem.Click(func() {
        runtime.WindowShow(a.ctx)
    })

    exitItem.Click(func() {
        systray.Quit()
        runtime.Quit(a.ctx)
    })
}

func (a *App) onTrayExit() {
    // 清理资源
}

func main() {
    app := &App{}

    wails.Run(&options.App{
        Title:             "awecloud-signaling",
        Width:             1024,
        Height:            768,
        HideWindowOnClose: true,  // 关闭时隐藏而不是退出
        OnStartup:         app.startup,
        OnShutdown:        app.shutdown,
        Bind:              []interface{}{app},
    })
}
```

### 3.3 前端配合

前端需要拦截最小化按钮，调用后端方法隐藏窗口：

```typescript
// 最小化到托盘
const minimizeToTray = async () => {
  await HideWindow(); // 调用后端 runtime.WindowHide()
};
```

### 3.4 托盘菜单设计

| 菜单项   | 功能                                     |
| -------- | ---------------------------------------- |
| 显示窗口 | 调用 `runtime.WindowShow()`              |
| 退出     | 调用 `systray.Quit()` + `runtime.Quit()` |

## 4. 业务流程

### 4.1 最小化到托盘

```
用户点击最小化按钮
    ↓
前端调用 HideWindow()
    ↓
后端调用 runtime.WindowHide()
    ↓
窗口隐藏，托盘图标保持显示
```

### 4.2 从托盘恢复

```
用户单击托盘图标 / 点击"显示窗口"菜单
    ↓
systray 回调触发
    ↓
调用 runtime.WindowShow()
    ↓
窗口恢复显示
```

### 4.3 退出应用

```
用户点击关闭按钮 / 托盘菜单"退出"
    ↓
调用 systray.Quit()
    ↓
调用 runtime.Quit()
    ↓
应用退出
```

## 5. 实现计划

| 阶段    | 内容                                     | 工时   |
| ------- | ---------------------------------------- | ------ |
| Phase 1 | 集成 energye/systray，实现 Windows 托盘  | 0.5 天 |
| Phase 2 | 前端拦截最小化按钮                       | 0.5 天 |
| Phase 3 | Linux 测试和适配                         | 0.5 天 |
| Phase 4 | macOS 适配（可选，需要 fyne-io/systray） | 0.5 天 |

## 6. 注意事项

1. **macOS 兼容性**：`energye/systray` 与 Wails 在 macOS 上有 AppDelegate 冲突，需要使用 `fyne-io/systray` 的修改版本
2. **Linux 依赖**：部分 Linux 发行版需要安装 AppIndicator 支持
3. **图标格式**：Windows 用 .ico，Linux 用 .png
4. **goroutine**：`systray.Run()` 需要在独立 goroutine 中运行

## 7. 当前状态

**状态**: 待实现

当前实现：

- ✅ 点击关闭按钮直接退出
- ✅ 点击最小化按钮正常最小化到任务栏
- ❌ 系统托盘功能

## 8. 参考资料

- [Wails Issue #1521 - Support Tray Menus](https://github.com/wailsapp/wails/issues/1521)
- [energye/systray](https://github.com/energye/systray)
- [fyne-io/systray](https://github.com/fyne-io/systray)

---

**文档版本**: v1.3
**更新日期**: 2024-12-28
