# 归档文档

此目录包含已过时或不再适用的设计文档，保留供历史参考。

## 归档原因

项目已从 FRP 隧道方案迁移到 Tailscale/Headscale 方案，以下文档不再适用于当前架构。

## 归档文档列表

| 文档                              | 原用途                       | 归档原因                        |
| --------------------------------- | ---------------------------- | ------------------------------- |
| design_frp.md                     | FRP 隧道设计                 | 已迁移到 Tailscale              |
| design_frp_websocket_path.md      | FRP WebSocket 路径设计       | 已迁移到 Tailscale              |
| design_http2.md                   | HTTP/2 统一端口设计          | FRP 相关，已过时                |
| design_public_url.md              | FRP 公网地址配置             | 已迁移到 Tailscale              |
| design_stcp_visitor_management.md | STCP 访问管理                | 已迁移到 Tailscale Visitor      |
| design_desktop.md                 | Desktop 客户端设计（FRP 版） | 已有 Tailscale 版本             |
| design_tunnel_management.md       | 隧道管理设计                 | 部分内容已整合到 Tailscale 文档 |

## 当前架构文档

请参考以下文档了解当前 Tailscale 架构：

- `design_tailscale_*.md` - Tailscale 相关设计
- `design_headscale_*.md` - Headscale 集成设计
