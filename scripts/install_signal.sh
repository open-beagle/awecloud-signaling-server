#!/bin/bash
set -e

# AWECloud Signaling Agent 安装脚本
# 用于在 CloudIDE 环境中部署 Agent（二进制内置到 CloudIDE 镜像）
#
# 必需环境变量：
#   SIGNAL_TOKEN  - 部署 Token（统一，无前缀区分）
#   SIGNAL_SERVER - Server 地址
#
# 可选环境变量：
#   SIGNAL_VERSION   - Agent 版本（默认 v0.2.3）
#   SIGNAL_LOG_LEVEL - 日志级别（默认 info）
#   HTTP_SERVER      - 下载服务器地址

# S3 配置
HTTP_SERVER="${HTTP_SERVER:-https://cache.example.com}"
AGENT_VERSION="${SIGNAL_VERSION:-v0.2.3}"

# 安装目录
INSTALL_DIR="$HOME/.local/signal"
BIN_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/signal"
DATA_DIR="$HOME/.local/share/signal"
LOG_DIR="$HOME/.local/share/signal/logs"

# 创建必要的目录
mkdir -p "$INSTALL_DIR"
mkdir -p "$BIN_DIR"
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$LOG_DIR"

# 检测系统架构
TARGET_ARCH="${TARGET_ARCH:-amd64}"
LOCAL_ARCH=$(uname -m)
if [ "$LOCAL_ARCH" = "x86_64" ]; then
  TARGET_ARCH="amd64"
elif [ "$(echo $LOCAL_ARCH | head -c 5)" = "armv8" ]; then
  TARGET_ARCH="arm64"
elif [ "$LOCAL_ARCH" = "aarch64" ]; then
  TARGET_ARCH="arm64"
else
  echo "This system's architecture $LOCAL_ARCH isn't supported"
  TARGET_ARCH="unsupported"
  exit 1
fi

# 从 S3 下载 Agent 二进制文件
AGENT_BINARY="agent-${AGENT_VERSION}-linux-${TARGET_ARCH}"
DOWNLOAD_URL="${HTTP_SERVER}/vscode/awecloud-signaling/${AGENT_BINARY}"

if ! [ -e "$INSTALL_DIR/$AGENT_BINARY" ]; then
  echo "正在下载 AWECloud Signaling Agent ${AGENT_VERSION} (${TARGET_ARCH})..."
  echo "下载地址: $DOWNLOAD_URL"

  if command -v curl &> /dev/null; then
    if ! curl -fsSL -o "$INSTALL_DIR/$AGENT_BINARY" "$DOWNLOAD_URL"; then
      echo "下载失败" >&2
      exit 1
    fi
  elif command -v wget &> /dev/null; then
    if ! wget -q -O "$INSTALL_DIR/$AGENT_BINARY" "$DOWNLOAD_URL"; then
      echo "下载失败" >&2
      exit 1
    fi
  else
    echo "需要 curl 或 wget" >&2
    exit 1
  fi

  chmod +x "$INSTALL_DIR/$AGENT_BINARY"
  echo "下载完成: $INSTALL_DIR/$AGENT_BINARY"
fi

# 创建符号链接
rm -f "$BIN_DIR/signal-agent"
ln -s "$INSTALL_DIR/$AGENT_BINARY" "$BIN_DIR/signal-agent"

# 生成配置文件（CloudIDE 默认开启 SSH、SSH_CONFIG、SOCKS）
if ! [ -e "$CONFIG_DIR/config.toml" ]; then
  echo "生成默认配置文件..."
  cat > "$CONFIG_DIR/config.toml" <<'EOF'
# AWECloud Signaling Agent 配置文件（CloudIDE 模式）
# 认证参数（token、server）通过环境变量 SIGNAL_TOKEN、SIGNAL_SERVER 传递

[tunnel]
# Tailscale 状态目录（留空使用默认值）
state_dir = ""
# 状态同步间隔（分钟）
state_sync_interval = 5
# 启用 SSH（允许 Desktop SSH 进来）
enable_ssh = true

[cloudide]
# 自动维护 ~/.ssh/config（劫持 100.64.* 的 SSH 连接）
ssh_config = true
# 启用 SOCKS5 代理（供非 SSH 程序按需使用）
socks = true
# SOCKS5 监听地址
socks_addr = "127.0.0.1:1080"
# dial 子命令的 Unix Socket 路径
dial_socket = "/tmp/signaling.sock"

[visitor]
# Visitor 监听地址（留空自动检测局域网 IP）
listen_addr = ""

[health]
# 健康检查端口
port = 8090

[log]
# 日志级别: debug, info, warn, error
level = "info"
EOF
  echo "配置文件已生成: $CONFIG_DIR/config.toml"
fi

# 停止旧的 Agent 进程
echo ""
echo "检查并停止旧的 Agent 进程..."
OLD_PIDS=$(pgrep -f "signal-agent" || true)
if [ -n "$OLD_PIDS" ]; then
  echo "发现旧进程: $OLD_PIDS"
  for pid in $OLD_PIDS; do
    echo "  停止进程 $pid..."
    kill "$pid" 2>/dev/null || true
  done
  sleep 2

  # 强制杀死仍在运行的进程
  OLD_PIDS=$(pgrep -f "signal-agent" || true)
  if [ -n "$OLD_PIDS" ]; then
    echo "强制停止进程: $OLD_PIDS"
    for pid in $OLD_PIDS; do
      kill -9 "$pid" 2>/dev/null || true
    done
  fi
  echo "旧进程已停止"
else
  echo "没有发现旧进程"
fi

# 检查必需的环境变量
echo ""
if [ -z "$SIGNAL_TOKEN" ] || [ -z "$SIGNAL_SERVER" ]; then
  echo "跳过启动: 缺少必需的环境变量"
  echo "  需要: SIGNAL_TOKEN, SIGNAL_SERVER"
  echo ""
  echo "安装已完成，二进制文件: $BIN_DIR/signal-agent"
  echo "启动前请设置环境变量后手动运行"
  exit 0
fi

# 设置状态目录
SIGNAL_STATE_DIR="$DATA_DIR/tunnel"
mkdir -p "$SIGNAL_STATE_DIR"

# 导出环境变量（Agent 通过 SIGNAL_* 读取）
export SIGNAL_TOKEN="$SIGNAL_TOKEN"
export SIGNAL_SERVER="$SIGNAL_SERVER"
export SIGNAL_STATE_DIR="$SIGNAL_STATE_DIR"

# 日志级别
if [ -n "$SIGNAL_LOG_LEVEL" ]; then
  export SIGNAL_LOG_LEVEL="$SIGNAL_LOG_LEVEL"
fi

echo "启动 AWECloud Signaling Agent..."
echo "  Server:  $SIGNAL_SERVER"
echo "  State:   $SIGNAL_STATE_DIR"
echo ""

# 后台启动 Agent（SSH 功能需要 root 权限）
nohup sudo "$BIN_DIR/signal-agent" -c "$CONFIG_DIR/config.toml" > "$LOG_DIR/agent.log" 2>&1 &
AGENT_PID=$!
echo "Agent 已启动 (PID: $AGENT_PID)"
echo ""

# 等待几秒检查进程是否正常运行
sleep 3
if ps -p "$AGENT_PID" > /dev/null 2>&1; then
  echo "✓ Agent 运行正常"
  echo ""
  echo "日志文件: $LOG_DIR/agent.log"
  echo "配置文件: $CONFIG_DIR/config.toml"
  echo "数据目录: $DATA_DIR"
else
  echo "✗ Agent 启动失败，请查看日志: $LOG_DIR/agent.log"
  tail -n 20 "$LOG_DIR/agent.log" 2>/dev/null || true
fi
