# Desktop 系统托盘功能设计文档

## 1. 概述

为 Desktop 客户端添加系统托盘功能，实现点击窗口关闭按钮时隐藏到系统托盘。

**目标平台**: Windows、macOS、Linux

**核心功能**:

1. 点击右上角关闭按钮 → 窗口隐藏到系统托盘
2. 点击右上角最小化按钮 → 正常最小化到任务栏
3. 单击/双击托盘图标 → 恢复窗口显示
4. 托盘图标右键菜单 → 显示窗口 / 退出

## 2. 技术方案

### 2.1 采用 Wails v3

Wails v3 原生支持系统托盘，无需第三方库。

**官方文档**: <https://v3alpha.wails.io/features/menus/systray/>

**平台支持**:

| 平台    | 支持状态                      |
| ------- | ----------------------------- |
| Windows | ✅ 完全支持                   |
| macOS   | ✅ 完全支持                   |
| Linux   | ✅ 支持（部分 DE 可能不支持） |

### 2.2 图标要求

| 平台    | 尺寸           | 格式     | 说明                       |
| ------- | -------------- | -------- | -------------------------- |
| Windows | 16x16 或 32x32 | PNG, ICO | 通知区域                   |
| macOS   | 18x18 到 22x22 | PNG      | 菜单栏，推荐 Template 图标 |
| Linux   | 22x22 到 48x48 | PNG, SVG | 因 DE 而异                 |

## 3. 实现方案

### 3.1 文件结构

```
desktop/
├── assets/
│   ├── icon.png          # 通用托盘图标
│   └── icon-dark.png     # macOS 深色模式图标（可选）
├── main.go               # 应用入口
└── app.go                # 应用逻辑
```

### 3.2 核心代码

```go
package main

import (
    _ "embed"
    "github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/icon.png
var icon []byte

func main() {
    app := application.New(application.Options{
        Name: "AWECloud Signaling",
        Mac: application.MacOptions{
            ApplicationShouldTerminateAfterLastWindowClosed: false,
        },
    })

    // 创建系统托盘
    systray := app.NewSystemTray()
    systray.SetIcon(icon)
    systray.SetLabel("Signaling")

    // 创建托盘菜单
    menu := app.NewMenu()
    menu.Add("显示窗口").OnClick(func(ctx *application.Context) {
        window.Show()
        window.SetFocus()
    })
    menu.AddSeparator()
    menu.Add("退出").OnClick(func(ctx *application.Context) {
        app.Quit()
    })
    systray.SetMenu(menu)

    // 单击托盘图标显示窗口
    systray.OnClick(func() {
        window.Show()
        window.SetFocus()
    })

    // 右键显示菜单
    systray.OnRightClick(func() {
        systray.OpenMenu()
    })

    // 创建主窗口（默认隐藏）
    window := app.NewWebviewWindow(application.WebviewWindowOptions{
        Title:  "AWECloud Signaling",
        Width:  1024,
        Height: 768,
        Hidden: true,
    })

    // 窗口关闭时隐藏到托盘
    window.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
        e.Cancel() // 阻止关闭
        window.Hide()
    })

    app.Run()
}
```

### 3.3 托盘菜单设计

| 菜单项   | 功能             |
| -------- | ---------------- |
| 显示窗口 | 显示并聚焦主窗口 |
| 退出     | 完全退出应用     |

## 4. 业务流程

### 4.1 关闭到托盘

```
用户点击关闭按钮
    ↓
WindowClosing 事件触发
    ↓
调用 e.Cancel() 阻止关闭
    ↓
调用 window.Hide()
    ↓
窗口隐藏，托盘图标保持显示
```

### 4.2 从托盘恢复

```
用户单击托盘图标 / 点击"显示窗口"菜单
    ↓
OnClick 回调触发
    ↓
调用 window.Show() + window.SetFocus()
    ↓
窗口恢复显示并获得焦点
```

### 4.3 退出应用

```
用户点击托盘菜单"退出"
    ↓
调用 app.Quit()
    ↓
应用退出
```

## 5. macOS 特殊处理

```go
// 使用 Template 图标（自动适应深色模式）
systray.SetTemplateIcon(iconBytes)

// 设置标签（显示在图标旁边）
systray.SetLabel("Signaling")

// 设置图标位置
systray.SetIconPosition(application.IconPositionRight)

// 阻止最后一个窗口关闭时退出应用
Mac: application.MacOptions{
    ApplicationShouldTerminateAfterLastWindowClosed: false,
}
```

## 6. 实现计划

| 阶段    | 内容               | 工时   |
| ------- | ------------------ | ------ |
| Phase 1 | 升级 Wails v2 → v3 | 1-2 天 |
| Phase 2 | 实现系统托盘功能   | 0.5 天 |
| Phase 3 | 测试三平台         | 0.5 天 |

详细升级指南见 `docs/changes_wails_v3.md`

## 7. 参考资料

- [Wails v3 System Tray 文档](https://wails.io/docs/learn/systray)
- [Wails v3 迁移指南](https://wails.io/docs/guides/migrating)

---

**文档版本**: v2.0
**更新日期**: 2025-12-29
