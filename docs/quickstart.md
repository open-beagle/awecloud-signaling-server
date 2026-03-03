# 快速开始

本文档介绍 AWECloud 四个组件的安装与升级方法。

---

## 1. Desktop

Desktop 是供终端用户使用的桌面客户端，用于访问内网服务（SSH、数据库、K8S 等）。

### 系统要求

- Windows 10/11（64 位）
- 能够访问 AWECloud Server

### 安装

从 Server 下载页面获取安装包，双击 `signal-desktop-setup.exe` 按向导完成安装。

安装完成后首次启动：

1. 输入 Server 地址（如 `https://signal.example.com`）
2. 输入账号和密码
3. 登录成功后自动绑定设备指纹

### 升级

Desktop 支持自动检测更新。登录后如有新版本，界面会提示升级，点击确认后自动下载并重启完成升级。

也可手动下载新版安装包覆盖安装，配置和登录状态会自动保留。

---

## 2. CloudIDE

CloudIDE 模式是 Agent 的一种运行形态，适用于开发环境（如 CloudIDE、远程开发容器）。它以当前用户身份运行，无需 root，通过 Tailscale 接入网络后提供 SSH 和 DNS 代理能力。

### 安装

需要两个环境变量：

- `SIGNAL_TOKEN`：从 Server 管理界面获取的 Client Token
- `SIGNAL_SERVER`：Server 地址

```bash
SIGNAL_TOKEN=<token> SIGNAL_SERVER=https://signal.example.com bash <(curl -fsSL https://signal.example.com/api/v1/download/install_signal.sh)
```

脚本会自动：

1. 检测系统架构（amd64 / arm64）
2. 下载对应二进制到 `~/.local/share/signal/`
3. 生成配置文件
4. 以 nohup 方式启动进程

### 升级

重新执行安装命令即可，脚本会停止旧进程、下载新版本、重新启动。

```bash
SIGNAL_TOKEN=<token> SIGNAL_SERVER=https://signal.example.com bash <(curl -fsSL https://signal.example.com/api/v1/download/install_signal.sh)
```

### 常用命令

```bash
# 查看日志
tail -f ~/.local/share/signal/logs/agent.log

# 检查进程
ps aux | grep signal_agent | grep -v grep

# 停止进程
sudo pkill -f signal_agent
```

---

## 3. Agent

Agent 部署在内网服务器上，通过 Tailscale 隧道连接 Server，为 Desktop 用户提供 SSH、K8S API、K8S Service 等访问能力。

Agent 以 systemd 服务方式运行，需要 root 权限。

### 安装

从 Server Web 界面的 Agent 管理页面生成部署 Token，然后执行：

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_agent.sh | \
  sudo bash -s -- \
  --deploy \
  -t <TOKEN> \
  -s https://signal.example.com
```

`--deploy` 模式会自动向 Server 注册并获取 Agent 名称，无需手动指定 `-n`。

如需手动指定 Agent 名称：

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_agent.sh | \
  sudo bash -s -- \
  -n beijing \
  -t <TOKEN> \
  -s https://signal.example.com
```

可选参数：

| 参数 | 说明 | 默认值 |
| --- | --- | --- |
| `-d, --device` | 设备名（用于域名注册） | hostname |
| `--no-ssh` | 禁用 SSH 能力 | 默认启用 |

安装完成后文件位置：

| 路径 | 说明 |
| --- | --- |
| `/opt/bin/signal_agent` | 二进制软链接 |
| `/etc/kubernetes/config/k8s-signaling.toml` | 配置文件 |
| `/etc/kubernetes/data/signaling/` | 数据目录（Tailscale 状态） |
| `/etc/systemd/system/k8s-signaling.service` | systemd 服务文件 |

### 升级

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_agent.sh | \
  sudo bash -s -- --upgrade -s https://signal.example.com
```

升级会停止服务、下载新版本、重新启动，配置文件保持不变。

### 卸载

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_agent.sh | \
  sudo bash -s -- --uninstall
```

配置文件和数据目录会保留，如需完全清除：

```bash
rm -f /etc/kubernetes/config/k8s-signaling.toml
rm -rf /etc/kubernetes/data/signaling
```

### 常用命令

```bash
# 查看状态
systemctl status k8s-signaling

# 查看日志
journalctl -u k8s-signaling -f

# 重启服务
systemctl restart k8s-signaling
```

### 能力配置

Agent 的 SSH、K8S API、K8S Service 等能力通过 Web 管理界面统一管理，不需要修改本地配置文件。

---

## 4. Endpoint

Endpoint 部署在无法直接访问公网的内网节点上，通过连接 Agent 的内网 gRPC 端口（默认 50052）接入系统，为 Desktop 用户提供 SSH、K8S API、K8S Service 等能力。

Endpoint 以 systemd 服务方式运行，需要 root 权限。

### 安装

从 Server Web 界面的 Agent 详情页生成 Endpoint Token（`ep_` 前缀），然后执行：

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_endpoint.sh | \
  sudo bash -s -- \
  -a 192.168.1.1:50052 \
  -t ep_xxxxxxxxxxxxxxxx \
  -s https://signal.example.com
```

参数说明：

| 参数 | 说明 | 必填 |
| --- | --- | --- |
| `-a, --agent` | Agent 内网 gRPC 地址（`IP:50052`） | 是 |
| `-t, --token` | Endpoint Token（`ep_` 前缀） | 是 |
| `-s, --server` | Server 地址（用于下载二进制） | 是 |
| `-n, --name` | Endpoint 名称 | 否，默认 hostname |
| `--ssh-port` | SSH 端口 | 否，默认 22 |
| `--no-ssh` | 禁用 SSH 能力 | 否 |
| `--k8s` | 启用 K8S API 能力 | 否 |
| `--k8s-api-server` | K8S API Server 地址 | 否，自动检测 |
| `--svc` | 启用 K8S Service 能力 | 否 |
| `--http-proxy` | HTTP 代理（无公网节点使用） | 否 |

安装完成后文件位置：

| 路径 | 说明 |
| --- | --- |
| `/opt/bin/signal_endpoint` | 二进制软链接 |
| `/etc/kubernetes/config/endpoint.toml` | 配置文件 |
| `/etc/systemd/system/signal-endpoint.service` | systemd 服务文件 |

### 升级

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_endpoint.sh | \
  sudo bash -s -- --upgrade -s https://signal.example.com
```

如果节点无法直接访问公网，通过代理升级：

```bash
export http_proxy="http://proxy.example.com:3128"
export https_proxy="http://proxy.example.com:3128"
curl -fsSL https://signal.example.com/api/v1/download/install_endpoint.sh | \
  bash -s -- --upgrade -s https://signal.example.com
```

### 卸载

```bash
curl -fsSL https://signal.example.com/api/v1/download/install_endpoint.sh | \
  sudo bash -s -- --uninstall
```

### 常用命令

```bash
# 查看状态
systemctl status signal-endpoint

# 查看日志
journalctl -u signal-endpoint -f

# 重启服务
systemctl restart signal-endpoint
```

### 能力配置

Endpoint 的 SSH、K8S API、K8S Service 等能力通过 Web 管理界面统一管理，不需要修改本地配置文件。
