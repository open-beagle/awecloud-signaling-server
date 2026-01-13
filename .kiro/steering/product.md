# AWECloud Signaling Server

安全的内网穿透访问系统，通过 Tailscale/Headscale 隧道，允许用户通过 Desktop 客户端访问内网服务（SSH、MySQL、Redis 等）。

## 核心组件

- **Server**：部署在公有云，作为信令服务器和流量中继。提供 REST API、gRPC 服务和 Web 管理界面。
- **Agent**：部署在内网环境，通过 Tailscale 连接到 Server，提供对内网服务的访问。
- **Desktop**：桌面客户端应用（独立仓库），供终端用户访问内网服务。
- **Web**：Vue 3 管理界面，用于管理 Agent、Client 和 Service。

## 核心功能

- 通过 Tailscale/Headscale 建立安全隧道
- 设备令牌认证，绑定硬件指纹
- 服务权限管理（公开/私有/分组访问）
- 连接审计日志
- Desktop 客户端版本控制
