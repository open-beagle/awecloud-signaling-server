# Tailscale 嵌入 Desktop - WGEngine 系统级 VPN 设计

> 本文档描述如何将 Tailscale 的 wgengine 能力嵌入 Desktop 客户端，实现跨平台（Windows/Linux/macOS）系统级 VPN。
>
> **关联文档**:
>
> - [Tailscale 升级 - Desktop 端变更设计 (tsnet 方案)](design_tailscale_desktop.md)
> - [Tailscale 多租户安全与权限设计](design_tailscale_security.md)

## 1. 核心目标

### 1.1 tsnet vs wgengine 对比

| 特性         | tsnet 方案 (旧)       | **wgengine 方案 (新)** | 优势                     |
| :----------- | :-------------------- | :--------------------- | :----------------------- |
| **网络层级** | L4 (Userspace TCP/IP) | **L3 (系统级 IP 层)**  | 真正的 VPN               |
| **路由**     | 仅进程内可见          | **全系统路由**         | 浏览器/终端/所有软件可用 |
| **SSH**      | 需配置 ProxyCommand   | **直接 SSH IP**        | VSCode Remote 无感连接   |
| **ICMP**     | 不支持 Ping           | **支持 Ping**          | 网络诊断方便             |
| **权限**     | 普通用户              | **管理员/Root**        | 必须操作驱动和路由表     |

### 1.2 目标场景

- 用户打开 Desktop，连接后获得 Tailscale IP（如 `100.64.0.x`）
- 系统任意程序可直接访问内网服务：`ssh root@100.64.0.5`、`mysql -h 100.64.0.10`
- VSCode Remote-SSH 无需任何代理配置，直接连接

## 2. 架构设计

### 2.1 模块结构

```txt
Desktop App (Wails v3)
  │
  ├─► UI Layer (Vue 3)
  │     └── 连接状态、IP 显示、服务列表
  │
  └─► Go Backend
        │
        ├─► Client (认证/业务 API)
        │
        └─► TailscaleManager
              │
              ├── tsd.System          # 核心依赖容器
              │     ├── EventBus      # 事件总线
              │     ├── Store         # 状态持久化
              │     └── NetMon        # 网络监控
              │
              ├── ipnlocal.LocalBackend  # 控制面
              │     ├── 状态机管理
              │     ├── Prefs 配置
              │     └── 登录流程
              │
              ├── wgengine.UserspaceEngine  # 数据面
              │     ├── wgdev (WireGuard 设备)
              │     ├── magicsock (NAT 穿透/DERP)
              │     └── router (系统路由)
              │
              └── tstun (TUN 设备)
                    ├── Windows: Wintun 驱动
                    ├── macOS: utun 接口
                    └── Linux: /dev/net/tun
```

### 2.2 Tailscale 源码关键模块

基于 `tailscale.com` 源码分析：

| 模块          | 路径                        | 作用                 |
| :------------ | :-------------------------- | :------------------- |
| **tstun**     | `net/tstun/`                | TUN 设备创建和包装   |
| **wgengine**  | `wgengine/userspace.go`     | WireGuard 引擎核心   |
| **router**    | `wgengine/router/`          | 系统路由管理         |
| **osrouter**  | `wgengine/router/osrouter/` | 各平台路由实现       |
| **magicsock** | `wgengine/magicsock/`       | NAT 穿透和 DERP 中继 |
| **ipnlocal**  | `ipn/ipnlocal/`             | LocalBackend 状态机  |
| **tsd**       | `tsd/`                      | 系统依赖注入容器     |

## 3. 跨平台实现

### 3.1 平台差异总览

| 平台        | TUN 驱动        | TUN 设备名 | 设备管理器显示  | 路由管理               | 防火墙                | 权限要求             |
| :---------- | :-------------- | :--------- | :-------------- | :--------------------- | :-------------------- | :------------------- |
| **Windows** | Wintun DLL      | `btun`     | `Beagle Tunnel` | `winipcfg`             | `netsh advfirewall`   | Administrator        |
| **macOS**   | utun (系统原生) | `btun`     | -               | `route` 命令           | `pf`                  | Root / Helper Tool   |
| **Linux**   | `/dev/net/tun`  | `btun`     | -               | `netlink` / `ip route` | `iptables`/`nftables` | Root / CAP_NET_ADMIN |

> **命名规范**: 所有平台 TUN 设备统一命名为 `btun`（Beagle TUN），Windows 在设备管理器中显示为 `Beagle Tunnel`。

### 3.2 Windows 实现

#### 3.2.1 Wintun DLL 加载

**关键点**：必须预加载正确版本的 `wintun.dll`，避免加载 system32 中的旧版本。

```go
// internal/tailscale/platform_windows.go
package tailscale

import (
    "log"
    "os"
    "path/filepath"

    "github.com/tailscale/wireguard-go/tun"
    "golang.org/x/sys/windows"
)

func init() {
    // 设置 TUN 类型（设备管理器中显示的名称）
    tun.WintunTunnelType = "Beagle Tunnel"

    // 固定 GUID（避免每次创建新接口）
    guid, _ := windows.GUIDFromString("{8C5E2B3A-F7D1-4E9A-B6C8-1A2D3E4F5678}")
    tun.WintunStaticRequestedGUID = &guid
}

// GetTunName 获取 TUN 设备名称
func GetTunName() string {
    return "btun"
}

// PlatformInit 预加载 wintun.dll
func PlatformInit() error {
    exe, err := os.Executable()
    if err != nil {
        return err
    }
    dllPath := filepath.Join(filepath.Dir(exe), "wintun.dll")

    if _, err := os.Stat(dllPath); err != nil {
        return fmt.Errorf("wintun.dll not found at %s", dllPath)
    }

    // 使用 windows.LoadDLL 预加载（不是 syscall.LoadDLL）
    log.Printf("[Wintun] Pre-loading DLL from: %s", dllPath)
    if _, err := windows.LoadDLL(dllPath); err != nil {
        return fmt.Errorf("failed to load wintun.dll: %w", err)
    }
    return nil
}
```

#### 3.2.2 Windows Router

Tailscale 的 Windows 路由实现位于 `wgengine/router/osrouter/router_windows.go`：

- 使用 `winipcfg` 包配置网络接口 IP 和路由
- 使用 `netsh advfirewall` 管理防火墙规则
- 支持 killswitch（阻止非 Tailscale 流量）

```go
// router 自动注册，无需手动创建
// 只需调用 router.New() 即可获得平台对应实现
r, err := router.New(logf, tunDev, netMon, healthTracker, eventBus)
```

### 3.3 macOS 实现

#### 3.3.1 utun 设备

macOS 使用系统原生的 utun 接口，无需额外驱动：

```go
// internal/tailscale/platform_darwin.go
package tailscale

// PlatformInit macOS 无需额外初始化
func PlatformInit() error {
    return nil
}

// GetTunName 获取 TUN 设备名称
// macOS 的 utun 名称会被系统忽略，自动分配编号
func GetTunName() string {
    return "btun"
}
```

#### 3.3.2 权限处理

macOS 需要 Root 权限或使用 Helper Tool：

```go
// 方案 1: 检测并提示用户以 sudo 运行
func isElevated() bool {
    return os.Geteuid() == 0
}

// 方案 2: 使用 SMJobBless 安装 Privileged Helper Tool（推荐生产环境）
// 需要 Apple Developer 签名
```

### 3.4 Linux 实现

#### 3.4.1 TUN 设备

Linux 使用标准的 `/dev/net/tun`：

```go
// internal/tailscale/platform_linux.go
package tailscale

// PlatformInit Linux 无需额外初始化
func PlatformInit() error {
    return nil
}

// GetTunName 获取 TUN 设备名称
func GetTunName() string {
    return "btun"
}
```

#### 3.4.2 路由管理

Linux 路由实现位于 `wgengine/router/osrouter/router_linux.go`：

- 使用 netlink 直接操作路由表
- 支持 iptables 和 nftables 防火墙
- 支持 policy routing

## 4. Manager 核心实现

### 4.1 日志规范

项目使用统一的日志系统，支持日志级别过滤：

| 级别    | 用途                     | 示例                   |
| :------ | :----------------------- | :--------------------- |
| `DEBUG` | 调试信息，生产环境不输出 | 详细的状态变化、参数值 |
| `INFO`  | 正常运行信息             | 启动、连接成功、断开   |
| `WARN`  | 警告，不影响运行         | 配置缺失使用默认值     |
| `ERROR` | 错误，需要关注           | 连接失败、权限不足     |

**日志格式规范**：

- 使用 `[模块名]` 前缀标识来源
- 日志级别标签：`[DEBUG]`、`[INFO]`、`[WARN]`、`[ERROR]`
- 内部引擎日志统一使用 `[Tunnel]` 前缀，级别为 DEBUG
- **禁止在日志中暴露 Tailscale 字样**

```go
// 正确示例
log.Printf("[INFO] [Tunnel] 连接成功: IP=%s", ip)
log.Printf("[DEBUG] [Tunnel] %s", engineMsg)
log.Printf("[ERROR] [Tunnel] 创建 TUN 设备失败: %v", err)

// 错误示例（不要这样写）
log.Printf("连接成功")  // 缺少模块前缀和级别
log.Printf("[Tunnel] 连接到: %s", url)  // 缺少级别
log.Printf("[Tailscale] ...")  // 禁止暴露 Tailscale
```

### 4.2 完整的 Manager 结构

```go
// internal/tailscale/manager.go
package tailscale

import (
    "context"
    "fmt"
    "log"
    "net"
    "os"
    "path/filepath"
    "runtime"
    "sync"
    "time"

    "tailscale.com/control/controlclient"
    "tailscale.com/health"
    "tailscale.com/ipn"
    "tailscale.com/ipn/ipnlocal"
    "tailscale.com/ipn/store"
    "tailscale.com/net/netmon"
    "tailscale.com/net/tstun"
    "tailscale.com/tsd"
    "tailscale.com/types/logger"
    "tailscale.com/types/logid"
    "tailscale.com/util/eventbus"
    "tailscale.com/util/usermetric"
    "tailscale.com/wgengine"
    "tailscale.com/wgengine/router"
)

// Manager 管理 Desktop 端 Tailscale 客户端 (System-Level VPN)
type Manager struct {
    lb *ipnlocal.LocalBackend

    // 状态
    tailscaleIP string
    connected   bool
    mutex       sync.RWMutex

    // 生命周期
    ctx    context.Context
    cancel context.CancelFunc
}

// NewManager 创建 TailscaleManager
func NewManager() *Manager {
    ctx, cancel := context.WithCancel(context.Background())
    return &Manager{
        ctx:    ctx,
        cancel: cancel,
    }
}

// Connect 连接隧道网络
func (m *Manager) Connect(controlURL, authKey, hostname string) error {
    log.Printf("[Tunnel] Connecting to: %s (System VPN Mode)", controlURL)

    // 1. 检查权限
    if !isElevated() {
        return fmt.Errorf("requires administrator/root privileges")
    }

    // 2. Windows: 预加载 Wintun
    if runtime.GOOS == "windows" {
        if err := PreloadWintun(); err != nil {
            log.Printf("[Wintun] Warning: %v", err)
        }
    }

    // 3. 初始化状态目录
    stateDir := m.getStateDir()
    if err := os.MkdirAll(stateDir, 0700); err != nil {
        return fmt.Errorf("failed to create state dir: %w", err)
    }

    // 4. 定义日志函数
    logf := logger.Logf(func(format string, args ...any) {
        log.Printf(format, args...)
    })

    // 5. 初始化核心依赖 (tsd.System)
    sys := tsd.NewSystem()

    // EventBus
    eb := sys.Bus.Get()
    if eb == nil {
        eb = eventbus.New()
        sys.Bus.Set(eb)
    }

    // HealthTracker
    ht := health.NewTracker(eb)

    // Store (状态持久化)
    storePath := filepath.Join(stateDir, "tailscaled.state")
    fstore, err := store.New(logf, storePath)
    if err != nil {
        return fmt.Errorf("failed to create store: %w", err)
    }
    sys.Set(fstore)

    // NetMon (网络监控)
    mon, err := netmon.New(eb, logf)
    if err != nil {
        return fmt.Errorf("failed to create netmon: %w", err)
    }
    mon.Start()
    sys.Set(mon)

    // Metrics (新版必须)
    metrics := usermetric.NewRegistry()

    // 6. 创建 TUN 设备
    tunName := "signal-tun0"
    tunDev, tunDevName, err := tstun.New(logf, tunName)
    if err != nil {
        mon.Close()
        return fmt.Errorf("failed to create TUN device: %w", err)
    }
    log.Printf("[TUN] Created device: %s", tunDevName)

    // 7. 创建 Router
    r, err := router.New(logf, tunDev, mon, ht, eb)
    if err != nil {
        tunDev.Close()
        mon.Close()
        return fmt.Errorf("failed to create router: %w", err)
    }

    // 8. 创建 WGEngine
    e, err := wgengine.NewUserspaceEngine(logf, wgengine.Config{
        Tun:           tunDev,
        Router:        r,
        HealthTracker: ht,
        Metrics:       metrics,
        ListenPort:    41641,
    })
    if err != nil {
        r.Close()
        tunDev.Close()
        mon.Close()
        return fmt.Errorf("failed to create engine: %w", err)
    }
    sys.Set(e)

    // 9. 创建 LocalBackend
    var pubID logid.PublicID
    lb, err := ipnlocal.NewLocalBackend(logf, pubID, sys, controlclient.LoginFlags(0))
    if err != nil {
        e.Close()
        return fmt.Errorf("failed to create local backend: %w", err)
    }
    m.lb = lb

    // 10. 启动
    opts := ipn.Options{
        AuthKey: authKey,
    }
    if err := lb.Start(opts); err != nil {
        return fmt.Errorf("failed to start backend: %w", err)
    }

    // 11. 应用配置
    prefs := ipn.NewPrefs()
    prefs.ControlURL = controlURL
    prefs.Hostname = hostname
    prefs.WantRunning = true

    if _, err := lb.EditPrefs(&ipn.MaskedPrefs{
        Prefs:          *prefs,
        ControlURLSet:  true,
        HostnameSet:    true,
        WantRunningSet: true,
    }); err != nil {
        return fmt.Errorf("failed to set prefs: %w", err)
    }

    // 12. 触发登录
    lb.StartLoginInteractive(m.ctx)

    // 13. 启动状态监控
    go m.watchStatus()

    log.Printf("[Tunnel] System VPN Engine started")
    return nil
}

// watchStatus 监听状态变化
func (m *Manager) watchStatus() {
    if m.lb == nil {
        return
    }

    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-m.ctx.Done():
            return
        case <-ticker.C:
            st := m.lb.Status()
            ip := ""
            connected := (st.BackendState == "Running")

            if len(st.TailscaleIPs) > 0 {
                ip = st.TailscaleIPs[0].String()
            }

            m.mutex.Lock()
            changed := (m.connected != connected) || (m.tailscaleIP != ip)
            m.tailscaleIP = ip
            m.connected = connected
            m.mutex.Unlock()

            if changed {
                log.Printf("[Tunnel] Status: State=%s, IP=%s", st.BackendState, ip)
            }
        }
    }
}

// Disconnect 断开连接
func (m *Manager) Disconnect() error {
    m.cancel()

    m.mutex.Lock()
    defer m.mutex.Unlock()

    if m.lb != nil {
        m.lb.Shutdown()
        m.lb = nil
    }

    m.connected = false
    m.tailscaleIP = ""

    log.Printf("[Tunnel] Disconnected")
    return nil
}

// Dial 通过隧道拨号（系统级 VPN 直接使用系统拨号器）
func (m *Manager) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
    var d net.Dialer
    return d.DialContext(ctx, network, addr)
}

// GetIP 获取隧道 IP
func (m *Manager) GetIP() string {
    m.mutex.RLock()
    defer m.mutex.RUnlock()
    return m.tailscaleIP
}

// IsConnected 检查连接状态
func (m *Manager) IsConnected() bool {
    m.mutex.RLock()
    defer m.mutex.RUnlock()
    return m.connected
}

// getStateDir 获取状态存储目录
func (m *Manager) getStateDir() string {
    var baseDir string
    if configDir, err := os.UserConfigDir(); err == nil {
        baseDir = configDir
    } else if homeDir, err := os.UserHomeDir(); err == nil {
        baseDir = filepath.Join(homeDir, ".config")
    } else {
        baseDir = "/tmp"
    }
    return filepath.Join(baseDir, "signal-desktop", "tailscale")
}

// isElevated 检查是否有管理员权限
func isElevated() bool {
    switch runtime.GOOS {
    case "windows":
        f, err := os.Open("\\\\.\\PHYSICALDRIVE0")
        if err == nil {
            f.Close()
            return true
        }
        return false
    default:
        return os.Geteuid() == 0
    }
}
```

### 4.2 平台特定文件

```go
// internal/tailscale/platform_windows.go
//go:build windows

package tailscale

import (
    "fmt"
    "log"
    "os"
    "path/filepath"

    "github.com/tailscale/wireguard-go/tun"
    "golang.org/x/sys/windows"
)

func init() {
    tun.WintunTunnelType = "Beagle Tunnel"
    guid, _ := windows.GUIDFromString("{8C5E2B3A-F7D1-4E9A-B6C8-1A2D3E4F5678}")
    tun.WintunStaticRequestedGUID = &guid
}

func GetTunName() string { return "btun" }

func PlatformInit() error {
    // 预加载 wintun.dll ...
}
```

```go
// internal/tailscale/platform_darwin.go
//go:build darwin

package tailscale

func GetTunName() string { return "btun" }
func PlatformInit() error { return nil }
```

```go
// internal/tailscale/platform_linux.go
//go:build linux

package tailscale

func GetTunName() string { return "btun" }
func PlatformInit() error { return nil }
```

## 5. 构建和部署

### 5.1 依赖管理

```bash
# go.mod 需要的依赖
go get tailscale.com@latest
go get github.com/tailscale/wireguard-go@latest
go get golang.zx2c4.com/wintun@latest  # Windows only
```

### 5.2 构建脚本

```bash
# scripts/build_desktop.sh

# Windows (需要包含 wintun.dll)
GOOS=windows GOARCH=amd64 go build -o bin/desktop-windows-amd64.exe ./desktop/...
cp wintun-amd64.dll bin/wintun.dll

# macOS
GOOS=darwin GOARCH=amd64 go build -o bin/desktop-darwin-amd64 ./desktop/...
GOOS=darwin GOARCH=arm64 go build -o bin/desktop-darwin-arm64 ./desktop/...

# Linux
GOOS=linux GOARCH=amd64 go build -o bin/desktop-linux-amd64 ./desktop/...
```

### 5.3 Wintun DLL 获取

```bash
# 下载 Wintun
curl -L https://www.wintun.net/builds/wintun-0.14.1.zip -o wintun.zip
unzip wintun.zip
cp wintun-0.14.1/bin/amd64/wintun.dll ./wintun-amd64.dll
cp wintun-0.14.1/bin/arm64/wintun.dll ./wintun-arm64.dll
```

## 6. 开发环境配置

### 6.1 Windows 开发

`desktop/scripts/dev.bat` 已配置：

1. 自动请求管理员权限
2. 自动下载 wintun.dll（如不存在）
3. 复制 wintun.dll 到 `.tmp/bin/`（wails3 dev 构建目录）

### 6.2 macOS/Linux 开发

```bash
# 需要 sudo 运行
sudo wails3 dev
```

## 7. 用户体验

### 7.1 提权流程

- **Windows**: 程序启动检测非 Admin，弹出 UAC 提示重启
- **macOS**: 首次连接时请求管理员密码
- **Linux**: 提示用户以 `sudo` 运行或配置 `setcap`

### 7.2 连接后验证

```bash
# 获得 IP 后，任意终端可直接访问
ping 100.64.0.5
ssh root@100.64.0.5
mysql -h 100.64.0.10 -u admin -p

# VSCode Remote-SSH 直接连接，无需代理配置
```

## 8. 风险和注意事项

1. **安全软件拦截**: 修改网络适配器可能被杀软拦截，建议申请代码签名
2. **接口冲突**: 使用独立的 TUN 名称和 GUID，避免与官方 Tailscale 冲突
3. **驱动版本**: Wintun 0.14.1 是稳定版本，保持更新
4. **权限最小化**: 仅在需要时请求管理员权限

---

**版本**: 2.0
**日期**: 2026-01-14
**状态**: 实施中
