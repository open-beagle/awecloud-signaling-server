# Tailscale 升级 - Desktop 端变更设计 (wgengine 方案)

> 本文档描述采用 `wgengine` + `tstun` 实现系统级 VPN 的 Desktop 客户端变更设计。
>
> **关联文档**:
>
> - [Tailscale 升级 - Desktop 端变更设计 (tsnet 方案)](design_tailscale_desktop.md)
> - [Tailscale 多租户安全与权限设计](design_tailscale_security.md)

## 1. 核心目标 (wgengine vs tsnet)

| 特性         | tsnet 方案 (旧)       | **wgengine 方案 (新)** | 优势                     |
| :----------- | :-------------------- | :--------------------- | :----------------------- |
| **网络层级** | L4 (Userspace TCP/IP) | **L3 (系统级 IP 层)**  | 真正的 VPN               |
| **路由**     | 仅进程内可见          | **全系统路由**         | 浏览器/终端/所有软件可用 |
| **SSH**      | 需配置 ProxyCommand   | **直接 SSH IP**        | VSCode Remote 无感连接   |
| **ICMP**     | 不支持 Ping           | **支持 Ping**          | 网络诊断方便             |
| **权限**     | 普通用户              | **管理员/Root**        | 必须操作驱动和路由表     |

## 2. 架构设计

### 2.1 模块结构

```txt
Desktop App (Wails)
  │
  ├─► UI Layer (Vue 3)
  │
  └─► Go Backend (App)
        │
        ├─► Client (认证/业务 API)
        │
        └─► TailscaleManager (重构)
              │
              ├── wgengine (WireGuard 引擎)
              │     ├── router (系统路由管理)
              │     └── magicsock (NAT 穿透)
              │
              ├── netstack (用户态协议栈 - 可选混合模式)
              │
              └── tstun (TUN 设备包装器)
                    │
                    ├── Windows: Wintun 驱动
                    ├── macOS: utun 接口
                    └── Linux: /dev/net/tun
```

### 2.2 核心流程

#### 启动流程 (Connect)

1.  **权限检查**:
    - Windows: 检测是否为 Administrator。若否，通过 `runas` 动词重启自身。
    - macOS: 检测是否为 Root。若否，通过 AppleScript `do shell script ... with administrator privileges` 或 Helper Tool 提权。
2.  **驱动加载 (Windows)**:
    - 检测 `wintun.dll`。若不存在，释放嵌入的 DLL 文件至临时目录。
    - 创建 Wintun 适配器接口。
3.  **引擎初始化**:
    - 初始化 `wgengine.Engine`。
    - 配置 `router.Config`，接管 `100.64.0.0/10` 路由。
4.  **连接**:
    - 下发 Headscale 登录参数 (ControlURL, Key)。
    - 等待 `StatusRunning`。

## 3. 详细设计

### 3.1 TailscaleManager 重构

文件：`desktop/internal/tailscale/manager.go`

```go
type Manager struct {
    engine    wgengine.Engine
    tunDevice tun.Device

    // 状态监控
    status     *ipnstate.Status

    // ...
}

func (m *Manager) Connect(cfg Config) error {
    // 1. 创建 TUN 设备
    dev, err := tstun.New(logf, tunName)

    // 2. 创建引擎
    e, err := wgengine.NewUserspaceEngine(logf, wgengine.Config{
        Tun:        dev,
        Router:     router.New(logf, dev, ...),
        // ...
    })

    // 3. 启动
    e.Start()

    // 4. 登录 (通过 LocalBackend 或模拟控制协议)
    // ...
}
```

### 3.2 平台差异处理

| 平台        | 驱动/接口 | 关键依赖库                | 部署要求                         |
| :---------- | :-------- | :------------------------ | :------------------------------- |
| **Windows** | `Wintun`  | `golang.zx2c4.com/wintun` | 安装包需包含 `wintun.dll` 或内嵌 |
| **macOS**   | `utun`    | 系统原生支持              | 二进制需签名，甚至需要 Notarize  |
| **Linux**   | `tun`     | 系统原生支持              | 需 `sudo` 运行                   |

## 4. 用户体验设计

### 4.1 提权交互

- **场景**: 用户双击图标启动。
- **行为**: 程序启动后检测非 Admin，弹出对话框：“初始化网络服务需要管理员权限，即将尝试以管理员身份重启”。
- **确认**: 用户点击“确定”后，程序退出并重新以 Admin 启动。

### 4.2 SSH 场景验证

- **场景**: 内网 Server IP 为 `100.64.0.5`。
- **操作**:
  - 用户打开 PowerShell / Terminal。
  - 输入 `ping 100.64.0.5` -> 通。
  - 打开 VS Code -> Remote-SSH -> Connect to Host -> `root@100.64.0.5` -> 成功。
  - **无需** 任何代理设置。

## 5. 风险控制

1.  **安全软件误报**: 修改网络适配器和路由表可能被 360/Defender 拦截。需申请数字签名。
2.  **网络冲突**: 避免与用户已安装的官方 Tailscale 冲突（使用不同的端口和接口名称）。
3.  **驱动兼容性**: Wintun 版本需保持更新。

---

**版本**: 1.0 (Draft)
**日期**: 2026-01-14
