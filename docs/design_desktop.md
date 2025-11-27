# Desktop 客户端设计文档

## 1. 概述

Desktop 客户端是 AWECloud Signaling 系统的终端用户应用，用于访问通过 Agent 暴露的远程服务。

**仓库信息**:

- Git 仓库: https://github.com/open-beagle/awecloud-signaling-desktop
- 本地路径: `desktop/`
- 技术栈: Wails (Go + Vue 3 + TypeScript)

**核心功能**:

1. 用户认证（Client ID + Secret）
2. 获取可访问的服务列表
3. 建立 STCP 隧道到远程服务
4. 在本地提供端口映射

## 2. 目录结构设计

Desktop 客户端采用清晰的目录结构，将 Wails 项目直接放在 `desktop/` 目录下：

```
desktop/                          # Desktop 客户端根目录（独立 Git 仓库）
├── README.md                     # 项目说明
├── main.go                       # Wails 应用入口
├── app.go                        # Wails 应用主结构
├── go.mod                        # Go 模块定义
├── go.sum                        # Go 依赖锁定
├── wails.json                    # Wails 配置文件
├── .gitignore                    # Git 忽略文件
│
├── internal/                     # 内部包（不对外暴露）
│   ├── client/                   # Desktop-Web 线程（gRPC 客户端）
│   │   ├── client.go             # gRPC 客户端实现
│   │   ├── auth.go               # 认证逻辑
│   │   └── service.go            # 服务管理
│   │
│   ├── frp/                      # Desktop-FRP 线程（FRP 客户端）
│   │   ├── manager.go            # FRP 管理器
│   │   ├── visitor.go            # STCP Visitor 管理
│   │   └── config.go             # FRP 配置
│   │
│   ├── config/                   # 配置管理
│   │   ├── config.go             # 配置结构定义
│   │   └── storage.go            # 配置存储（本地文件）
│   │
│   └── models/                   # 数据模型
│       ├── service.go            # 服务信息模型
│       ├── connection.go         # 连接状态模型
│       └── command.go            # 进程内通信命令模型
│
├── pkg/                          # 公共包（可对外暴露）
│   └── proto/                    # Protocol Buffers 定义（从主项目复制）
│       ├── client.proto          # Client 服务定义
│       └── client.pb.go          # 生成的 Go 代码
│
├── frontend/                     # Vue 3 前端代码
│   ├── src/
│   │   ├── App.vue               # 根组件
│   │   ├── main.ts               # 入口文件
│   │   ├── style.css             # 全局样式
│   │   │
│   │   ├── views/                # 页面组件
│   │   │   ├── Login.vue         # 登录页面
│   │   │   └── Services.vue      # 服务列表页面
│   │   │
│   │   ├── components/           # 通用组件
│   │   │   ├── ServiceCard.vue   # 服务卡片
│   │   │   └── StatusBadge.vue   # 状态徽章
│   │   │
│   │   ├── stores/               # Pinia 状态管理
│   │   │   ├── auth.ts           # 认证状态
│   │   │   └── services.ts       # 服务状态
│   │   │
│   │   └── utils/                # 工具函数
│   │       └── format.ts         # 格式化工具
│   │
│   ├── wailsjs/                  # Wails 自动生成的绑定代码
│   │   ├── go/                   # Go 方法绑定
│   │   └── runtime/              # Wails 运行时
│   │
│   ├── index.html                # HTML 模板
│   ├── package.json              # npm 依赖
│   ├── tsconfig.json             # TypeScript 配置
│   ├── vite.config.ts            # Vite 配置
│   └── README.md                 # 前端说明
│
├── build/                        # 构建资源
│   ├── appicon.png               # 应用图标
│   ├── windows/                  # Windows 构建配置
│   │   └── icon.ico              # Windows 图标
│   ├── darwin/                   # macOS 构建配置（后续版本）
│   └── linux/                    # Linux 构建配置（后续版本）
│
├── config/                       # 配置文件示例
│   └── config.example.json       # 配置文件示例
│
├── scripts/                      # 构建和开发脚本
│   ├── build.sh                  # 构建脚本
│   └── dev.sh                    # 开发脚本
│
└── docs/                         # 文档
    ├── README.md                 # 文档索引
    ├── development.md            # 开发指南
    └── user-guide.md             # 用户手册
```

### 2.1 目录说明

**根目录文件**:
- `main.go`: Wails 应用入口，初始化应用和绑定 Go 方法
- `app.go`: 应用主结构，包含 Desktop-Web 和 Desktop-FRP 的实例
- `wails.json`: Wails 配置，定义构建选项和应用信息

**internal/ 目录**:
- `client/`: Desktop-Web 线程实现，负责 gRPC 通信
- `frp/`: Desktop-FRP 线程实现，负责 FRP 连接和 Visitor 管理
- `config/`: 配置管理，包括本地配置文件的读写
- `models/`: 数据模型定义，用于进程内通信和状态管理

**frontend/ 目录**:
- 标准的 Vue 3 + TypeScript + Vite 项目结构
- `views/`: 页面级组件（Login, Services）
- `components/`: 可复用组件
- `stores/`: Pinia 状态管理
- `wailsjs/`: Wails 自动生成的 Go 方法绑定

**build/ 目录**:
- 应用图标和平台特定的构建资源
- MVP 阶段只需要 Windows 配置

### 2.2 与主项目的关系

Desktop 是一个**独立的 Git 仓库**，但需要从主项目复制一些文件：

1. **Protocol Buffers 定义**:
   - 从主项目 `pkg/proto/client.proto` 复制到 `pkg/proto/`
   - 使用相同的 protoc 命令生成 Go 代码

2. **配置示例**:
   - 参考主项目的配置格式，但 Desktop 有自己的配置文件

3. **文档**:
   - 主项目的 `docs/design_desktop.md` 是设计文档
   - Desktop 仓库的 `docs/` 是实现文档和用户手册

### 2.3 单进程双线程架构

Desktop 是一个单一进程，包含两个工作线程（goroutine）：

```
Desktop 进程
├── Desktop-Web 线程 (gRPC 客户端)
│   ├── 连接 Server-Web 线程 (端口 8080, HTTP/2)
│   ├── Client 认证
│   ├── 获取服务列表
│   └── 获取连接信息
│
└── Desktop-FRP 线程 (FRP 客户端)
    ├── 连接 Server-FRP 线程 (端口 7000, WebSocket)
    ├── 创建 STCP Visitor
    ├── 本地端口监听
    └── 数据转发
```

### 2.4 进程内通信

Desktop-Web 和 Desktop-FRP 之间通过 Go channel 进行通信：

```go
// 命令通道：Desktop-Web → Desktop-FRP
type VisitorCommand struct {
    Action       string // "connect" or "disconnect"
    InstanceName string
    SecretKey    string
    LocalPort    int
    Response     chan error
}

// 状态通道：Desktop-FRP → Desktop-Web
type VisitorStatus struct {
    InstanceName string
    Status       string // "connected", "disconnected", "error"
    LocalPort    int
    Error        string
}
```

## 3. 构建和部署策略

### 3.1 开发环境

**前置要求**:
- Go 1.21+
- Node.js 18+
- Wails CLI v2.11.0+

**开发命令**:
```bash
# 安装前端依赖
cd frontend && npm install

# 开发模式（热重载）
wails dev

# 构建（仅当前平台）
wails build

# 构建（指定平台）
wails build -platform windows/amd64
```

### 3.2 生产构建

**Windows 构建**:
```bash
# 构建 Windows 可执行文件
wails build -platform windows/amd64 -clean

# 输出位置
build/bin/awecloud-desktop.exe
```

**打包为安装程序**（后续版本）:
- 使用 NSIS 或 Inno Setup
- 包含自动更新功能

### 3.3 配置文件位置

Desktop 应用的配置文件存储在用户目录：

- **Windows**: `%APPDATA%\awecloud-desktop\config.json`
- **macOS**: `~/Library/Application Support/awecloud-desktop/config.json`
- **Linux**: `~/.config/awecloud-desktop/config.json`

## 4. Desktop-Web 线程设计

### 4.1 核心职责

1. **用户认证**

   - 通过 gRPC 连接 Server-Web
   - 使用 Client ID 和 Secret 进行认证
   - 获取 Session Token

2. **服务管理**

   - 获取可访问的服务列表
   - 获取服务连接信息（Secret Key）
   - 管理服务连接状态

3. **命令下发**
   - 将用户操作转换为命令
   - 通过 channel 发送给 Desktop-FRP 线程
   - 等待操作结果

### 4.2 gRPC 接口

使用 `pkg/proto/client.proto` 定义的接口：

```protobuf
service ClientService {
  // Client 认证
  rpc Authenticate(AuthRequest) returns (AuthResponse);

  // 获取可访问服务列表
  rpc GetServices(GetServicesRequest) returns (GetServicesResponse);

  // 连接服务（获取连接信息）
  rpc ConnectService(ConnectRequest) returns (ConnectResponse);
}
```

### 4.3 核心数据结构

```go
type DesktopWeb struct {
    config *DesktopConfig

    // gRPC 连接
    grpcConn   *grpc.ClientConn
    grpcClient pb.ClientServiceClient

    // 认证信息
    sessionToken string
    clientID     string

    // 服务列表
    services     map[int64]*ServiceInfo
    servicesMutex sync.RWMutex

    // 命令通道（发送给 Desktop-FRP）
    commandChan chan *VisitorCommand

    // 状态通道（接收自 Desktop-FRP）
    statusChan chan *VisitorStatus

    // 上下文
    ctx    context.Context
    cancel context.CancelFunc
}
```

## 5. Desktop-FRP 线程设计

### 5.1 核心职责

1. **FRP 客户端管理**

   - 连接 Server-FRP (WebSocket)
   - 维护 FRP 客户端连接
   - 处理连接断开和重连

2. **Visitor 管理**

   - 动态创建 STCP Visitor
   - 动态删除 STCP Visitor
   - 管理 Visitor 生命周期

3. **本地端口映射**
   - 在本地监听端口
   - 转发数据到 STCP 隧道
   - 处理连接错误

### 5.2 核心数据结构

```go
type DesktopFRP struct {
    config *DesktopConfig

    // FRP 客户端
    service *client.Service

    // Visitor 配置
    visitors map[string]*v1.STCPVisitorConfig
    mutex    sync.RWMutex

    // 命令通道（接收自 Desktop-Web）
    commandChan chan *VisitorCommand

    // 状态通道（发送给 Desktop-Web）
    statusChan chan *VisitorStatus

    // 上下文
    ctx    context.Context
    cancel context.CancelFunc
}
```

### 5.3 STCP Visitor 配置

```go
visitorConfig := &v1.STCPVisitorConfig{
    VisitorBaseConfig: v1.VisitorBaseConfig{
        Name:       instanceName + "-visitor",
        Type:       "stcp",
        ServerName: instanceName,  // 对应 Agent 端的 STCP Proxy 名称
        BindAddr:   "127.0.0.1",
        BindPort:   localPort,
    },
    SecretKey: secretKey,  // 从 Server 获取
}
```

## 6. 用户界面设计

### 6.1 技术栈

- **前端框架**: Vue 3 + TypeScript
- **UI 组件库**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router
- **HTTP 客户端**: Axios（用于 RESTful API，可选）

### 6.2 页面结构

```
Desktop 应用
├── 登录页面
│   ├── Server 地址配置
│   ├── Client ID 输入
│   ├── Client Secret 输入
│   └── 登录按钮
│
├── 主界面
│   ├── 顶部栏
│   │   ├── 用户信息
│   │   ├── 连接状态
│   │   └── 设置按钮
│   │
│   └── 服务列表
│       ├── 服务卡片
│       │   ├── 服务名称
│       │   ├── 服务描述
│       │   ├── 本地端口配置
│       │   ├── 连接按钮
│       │   └── 连接状态
│       │
│       └── 刷新按钮
│
└── 设置页面
    ├── Server 地址
    ├── 自动连接配置
    └── 日志查看
```

### 6.3 Wails 绑定

```go
// Wails App 结构
type App struct {
    ctx        context.Context
    desktopWeb *DesktopWeb
    desktopFRP *DesktopFRP
}

// 暴露给前端的方法
func (a *App) Login(serverAddr, clientID, clientSecret string) error
func (a *App) GetServices() ([]ServiceInfo, error)
func (a *App) ConnectService(instanceID int64, localPort int) error
func (a *App) DisconnectService(instanceID int64) error
func (a *App) GetConnectionStatus() map[int64]string
```

## 7. 核心业务流程

### 7.1 用户登录流程

```
用户输入凭证
    ↓
Desktop-Web: 调用 Authenticate gRPC
    ↓
Server-Web: 验证 Client ID 和 Secret
    ↓
Server-Web: 返回 Session Token
    ↓
Desktop-Web: 保存 Session Token
    ↓
前端: 跳转到主界面
```

### 7.2 获取服务列表流程

```
用户进入主界面
    ↓
Desktop-Web: 调用 GetServices gRPC
    ↓
Server-Web: 查询 stcp_access 表
    ↓
Server-Web: 返回服务列表（ID, Name, SecretKey）
    ↓
Desktop-Web: 更新内存中的服务列表
    ↓
前端: 显示服务卡片
```

### 7.3 连接服务流程

```
用户点击"连接"按钮
    ↓
前端: 调用 Wails 方法 ConnectService(instanceID, localPort)
    ↓
Desktop-Web: 调用 ConnectService gRPC
    ↓
Server-Web: 返回连接信息（InstanceName, SecretKey）
    ↓
Desktop-Web: 创建 VisitorCommand
    ↓
Desktop-Web: 发送命令到 commandChan
    ↓
Desktop-FRP: 接收命令
    ↓
Desktop-FRP: 创建 STCP Visitor 配置
    ↓
Desktop-FRP: 添加 Visitor 到 FRP Client
    ↓
Desktop-FRP: FRP Client 连接 Server-FRP
    ↓
Server-FRP: 协调 Desktop-FRP 和 Agent-FRP
    ↓
建立 STCP 隧道
    ↓
Desktop-FRP: 在本地端口监听
    ↓
Desktop-FRP: 发送状态到 statusChan
    ↓
Desktop-Web: 接收状态
    ↓
前端: 更新连接状态为"已连接"
```

### 7.4 断开服务流程

```
用户点击"断开"按钮
    ↓
前端: 调用 Wails 方法 DisconnectService(instanceID)
    ↓
Desktop-Web: 创建 VisitorCommand (action="disconnect")
    ↓
Desktop-Web: 发送命令到 commandChan
    ↓
Desktop-FRP: 接收命令
    ↓
Desktop-FRP: 从 FRP Client 移除 Visitor
    ↓
Desktop-FRP: 关闭本地端口监听
    ↓
Desktop-FRP: 发送状态到 statusChan
    ↓
Desktop-Web: 接收状态
    ↓
前端: 更新连接状态为"已断开"
```

## 8. 配置管理

### 8.1 配置文件

Desktop 使用本地配置文件存储用户设置：

**位置**:

- Windows: `%APPDATA%/awecloud-signaling/config.json`
- macOS: `~/Library/Application Support/awecloud-signaling/config.json`
- Linux: `~/.config/awecloud-signaling/config.json`

**内容**:

```json
{
  "server_address": "server.example.com:8081",
  "client_id": "user@example.com",
  "client_secret": "encrypted_secret",
  "auto_connect": {
    "enabled": true,
    "services": [
      {
        "instance_id": 1,
        "local_port": 3306
      }
    ]
  },
  "ui": {
    "theme": "light",
    "language": "zh-CN"
  }
}
```

### 8.2 凭证存储

- Client Secret 使用操作系统的密钥链存储（Windows Credential Manager / macOS Keychain）
- Session Token 存储在内存中，不持久化

## 9. 错误处理

### 9.1 连接错误

**场景**: Server 不可达、网络中断

**处理**:

1. Desktop-Web: 显示连接错误提示
2. 自动重试机制（指数退避）
3. 用户可手动重连

### 9.2 认证错误

**场景**: Client ID 或 Secret 错误、Token 过期

**处理**:

1. 清除本地 Session Token
2. 提示用户重新登录
3. 记录错误日志

### 9.3 隧道错误

**场景**: STCP 隧道建立失败、Agent 离线

**处理**:

1. Desktop-FRP: 返回错误状态
2. 前端: 显示具体错误信息
3. 提供重试按钮

## 10. 安全考虑

### 10.1 凭证保护

- Client Secret 加密存储
- Session Token 仅存储在内存
- 应用退出时清除敏感信息

### 10.2 通信安全

- gRPC 使用 TLS 加密（生产环境）
- WebSocket 使用 WSS 加密（生产环境）
- STCP 隧道本身是加密的

### 10.3 本地端口安全

- 默认只监听 127.0.0.1
- 不允许外部访问
- 端口冲突检测

## 11. 性能优化

### 11.1 连接复用

- gRPC 连接保持长连接
- WebSocket 连接保持长连接
- 避免频繁重连

### 11.2 资源管理

- 及时释放未使用的 Visitor
- 限制最大并发连接数
- 内存使用监控

### 11.3 UI 响应

- 异步操作不阻塞 UI
- 加载状态提示
- 操作结果反馈

## 12. MVP 功能范围

### 12.1 必须实现

- ✅ 用户登录（Client ID + Secret）
- ✅ 获取服务列表
- ✅ 连接服务（建立 STCP 隧道）
- ✅ 断开服务
- ✅ 连接状态显示
- ✅ 基本错误处理
- ✅ Windows 支持

### 12.2 可选功能（后续版本）

- ❌ 自动连接
- ❌ 配置导入导出
- ❌ 详细日志查看
- ❌ 连接统计
- ❌ macOS/Linux 支持
- ❌ 系统托盘图标
- ❌ 开机自启动

## 13. 开发计划

### 13.1 Week 6: 基础功能

**任务**:

1. Wails 项目初始化
2. Desktop-Web 线程实现
   - gRPC 客户端
   - 认证逻辑
   - 服务列表管理
3. 登录界面
4. 服务列表界面

**交付物**:

- 可运行的 Desktop 应用（开发版）
- 登录和服务列表功能

### 13.2 Week 7: 连接功能

**任务**:

1. Desktop-FRP 线程实现
   - FRP 客户端集成
   - Visitor 管理
   - 本地端口映射
2. 连接/断开功能
3. 状态管理
4. 错误处理
5. Windows 打包

**交付物**:

- 完整功能的 Desktop 应用
- Windows 安装包
- 用户文档

## 14. 测试计划

### 14.1 单元测试

- Desktop-Web 线程测试
- Desktop-FRP 线程测试
- 进程内通信测试

### 14.2 集成测试

- 与 Server 的 gRPC 通信测试
- 与 Server 的 WebSocket 通信测试
- STCP 隧道建立测试

### 14.3 端到端测试

- 完整的登录流程
- 完整的连接流程
- 完整的断开流程
- 错误场景测试

### 14.4 用户测试

- 安装测试
- 功能测试
- 性能测试
- 兼容性测试

## 15. 部署和分发

### 15.1 打包

使用 Wails 的打包工具：

```bash
# Windows
wails build -platform windows/amd64

# 输出
desktop/build/bin/awecloud-desktop.exe
```

### 15.2 安装程序

使用 NSIS 或 Inno Setup 创建 Windows 安装程序：

- 安装到 Program Files
- 创建桌面快捷方式
- 创建开始菜单项
- 支持卸载

### 15.3 自动更新

后续版本考虑：

- 检查更新功能
- 自动下载更新
- 静默安装

## 16. 文档

### 16.1 用户文档

- 安装指南
- 使用教程
- 常见问题
- 故障排查

### 16.2 开发文档

- 架构说明
- API 文档
- 构建指南
- 贡献指南

## 17. 参考资料

- Wails 文档: https://wails.io/
- FRP 文档: https://github.com/fatedier/frp
- gRPC Go 文档: https://grpc.io/docs/languages/go/
- Vue 3 文档: https://vuejs.org/
- Element Plus 文档: https://element-plus.org/

---

**文档版本**: v1.0  
**创建日期**: 2025-11-27  
**状态**: 待审批
