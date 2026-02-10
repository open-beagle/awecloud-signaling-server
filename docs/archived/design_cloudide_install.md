# CloudIDE 安装部署

## 部署方式

CloudIDE 通过二进制内置到镜像中，不使用容器部署。安装脚本 `scripts/install_signal.sh` 负责下载、配置和启动 Agent。

## 必需环境变量

CloudIDE 只需要 2 个环境变量：

| 环境变量      | 说明         | 来源                         |
| ------------- | ------------ | ---------------------------- |
| SIGNAL_TOKEN  | Client Token | 管理员在 Server Web 界面生成 |
| SIGNAL_SERVER | Server 地址  | 系统配置                     |

设备名自动使用容器 hostname，不需要额外配置。

## 安装脚本

脚本路径：`scripts/install_signal.sh`

### 功能

1. 检测系统架构（amd64/arm64）
2. 从 S3 下载 Agent 二进制到 `~/.local/signal/`
3. 创建符号链接 `~/.local/bin/signal-agent`
4. 生成默认配置文件（默认开启 SSH、SSH_CONFIG、SOCKS）
5. 停止旧进程，后台启动新 Agent

### 目录结构

```
~/.local/
├── signal/                    # 二进制存储
│   └── agent-v0.2.3-linux-amd64
├── bin/
│   └── signal-agent -> ../signal/agent-v0.2.3-linux-amd64
├── share/signal/
│   ├── logs/agent.log         # 日志
│   └── tunnel/                # Tailscale 状态
└── .config/signal/
    └── config.toml            # 配置文件
```

### 默认配置

安装脚本生成的配置文件默认开启 CloudIDE 所有功能：

```toml
[tunnel]
enable_ssh = true              # 允许 Desktop SSH 进来

[cloudide]
ssh_config = true              # 自动维护 ~/.ssh/config
socks = true                   # 启用 SOCKS5 代理
socks_addr = "127.0.0.1:1080"
dial_socket = "/tmp/signaling.sock"
```

## CloudIDE 镜像集成

### Dockerfile 示例

在 CloudIDE 基础镜像中预装 Agent：

```dockerfile
# 下载 Agent 二进制
RUN curl -fsSL -o /usr/local/bin/signal-agent \
    https://cache.example.com/vscode/awecloud-signaling/agent-v0.2.3-linux-amd64 && \
    chmod +x /usr/local/bin/signal-agent

# 复制安装脚本
COPY scripts/install_signal.sh /usr/local/bin/install-signal.sh
```

### Pod 环境变量配置

```yaml
env:
  - name: SIGNAL_TOKEN
    valueFrom:
      secretKeyRef:
        name: cloudide-signal
        key: token
  - name: SIGNAL_SERVER
    value: "https://signal.example.com"
```

### 启动方式

CloudIDE 容器启动时执行安装脚本：

```
bash /usr/local/bin/install-signal.sh
```

或者如果二进制已预装到镜像，直接启动：

```
signal-agent -c ~/.config/signal/config.toml
```

## 升级

更新 `SIGNAL_VERSION` 环境变量后重新执行安装脚本，脚本会自动下载新版本并重启。

```
SIGNAL_VERSION=v0.2.4 bash scripts/install_signal.sh
```

## 验证

```
# 检查进程
pgrep -f signal-agent

# 查看日志
tail -f ~/.local/share/signal/logs/agent.log

# 健康检查
curl http://localhost:8090/health
```
