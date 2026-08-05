#!/bin/bash
set -e

# AWECloud Signaling Agent 的 Container Client / CloudIDE 安装入口。
# Agent 技术资源请使用 install_agent.sh。本入口会用 -t/-s 启动
# signal_agent，让二进制先调用 /api/v1/register，再按 user_role=client
# 切换到客户端模式。

HTTP_SERVER="${SIGNAL_HTTP_SERVER:-https://cache.ali.wodcloud.com}"
SIGNAL_SERVER="${SIGNAL_SERVER:-}"
AGENT_VERSION="${SIGNAL_VERSION:-}"
SIGNAL_TOKEN="${SIGNAL_TOKEN:-}"
SIGNAL_LOG_LEVEL="${SIGNAL_LOG_LEVEL:-info}"
UPGRADE_MODE=0

TRUE_HOME=$HOME
if [ "$EUID" -eq 0 ] && [ "$HOME" != "/root" ]; then
  TRUE_HOME="/root"
elif command -v getent >/dev/null 2>&1; then
  TRUE_HOME=$(getent passwd "$(whoami)" | cut -d: -f6)
fi

BIN_DIR="$TRUE_HOME/.local/bin"
DATA_DIR="$TRUE_HOME/.local/share/signal"
CONFIG_FILE="$DATA_DIR/agent.toml"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--token)
      SIGNAL_TOKEN="$2"
      shift 2
      ;;
    -s|--server)
      SIGNAL_SERVER="$2"
      shift 2
      ;;
    -v|--version)
      AGENT_VERSION="$2"
      shift 2
      ;;
    --upgrade)
      UPGRADE_MODE=1
      shift
      ;;
    *)
      shift
      ;;
  esac
done

read_config_value() {
  local key="$1"
  local file="$2"
  grep -m1 "^[[:space:]]*${key}[[:space:]]*=" "$file" 2>/dev/null | cut -d'"' -f2
}

echo "=========================================="
echo "  AWECloud Signaling Agent 客户端安装脚本"
echo "=========================================="
echo "  此下载入口 install_signal.sh 面向 Container Client；"
echo "  Agent 请走 install_agent.sh"
echo ""

SUDO_CMD=""
if command -v sudo >/dev/null 2>&1; then
  SUDO_CMD="sudo"
fi

OLD_PIDS=$(pgrep -x "signal_agent" 2>/dev/null || true)
if [ -n "$OLD_PIDS" ]; then
  echo "[停止] 发现旧进程: $OLD_PIDS"
  $SUDO_CMD kill $OLD_PIDS 2>/dev/null || true
  sleep 2
  OLD_PIDS=$(pgrep -x "signal_agent" 2>/dev/null || true)
  if [ -n "$OLD_PIDS" ]; then
    $SUDO_CMD kill -9 $OLD_PIDS 2>/dev/null || true
  fi
  echo "[停止] 旧进程已清理"
else
  echo "[停止] 无旧进程"
fi

mkdir -p "$BIN_DIR" "$DATA_DIR" "$DATA_DIR/logs"

LOCAL_ARCH=$(uname -m)
case "$LOCAL_ARCH" in
  x86_64) TARGET_ARCH="amd64" ;;
  aarch64|armv8*) TARGET_ARCH="arm64" ;;
  *)
    echo "不支持的架构: $LOCAL_ARCH" >&2
    exit 1
    ;;
esac

if [ -z "$AGENT_VERSION" ]; then
  echo ""
  echo "[版本] 获取最新版本..."
  VERSION_URL="${HTTP_SERVER}/vscode/awecloud-signaling/signal_agent-version.json"
  VERSION_RESPONSE=""
  if command -v curl >/dev/null 2>&1; then
    VERSION_RESPONSE=$(curl -fsSL "$VERSION_URL" 2>&1) || true
  elif command -v wget >/dev/null 2>&1; then
    VERSION_RESPONSE=$(wget -qO- "$VERSION_URL" 2>&1) || true
  fi
  if [ -n "$VERSION_RESPONSE" ]; then
    AGENT_VERSION=$(echo "$VERSION_RESPONSE" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
  fi
  if [ -z "$AGENT_VERSION" ]; then
    echo "[版本] 无法获取最新版本，使用默认版本 v0.2.4"
    AGENT_VERSION="v0.2.4"
  else
    echo "[版本] 最新版本: $AGENT_VERSION"
  fi
else
  echo ""
  echo "[版本] 使用指定版本: $AGENT_VERSION"
fi

AGENT_BINARY="signal_agent-${AGENT_VERSION}-linux-${TARGET_ARCH}"
DOWNLOAD_URL="${HTTP_SERVER}/vscode/awecloud-signaling/${AGENT_BINARY}"
TEMP_FILE="$DATA_DIR/${AGENT_BINARY}.tmp"
TARGET_BINARY="$DATA_DIR/$AGENT_BINARY"

echo ""
echo "[下载] 客户端 ${AGENT_VERSION} (${TARGET_ARCH})"
echo "[下载] $DOWNLOAD_URL"
rm -f "$TEMP_FILE"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "$TEMP_FILE" "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "$TEMP_FILE" "$DOWNLOAD_URL"
else
  echo "需要 curl 或 wget" >&2
  exit 1
fi
rm -f "$TARGET_BINARY"
mv "$TEMP_FILE" "$TARGET_BINARY"
chmod +x "$TARGET_BINARY"
rm -f "$BIN_DIR/signal_agent"
ln -s "$TARGET_BINARY" "$BIN_DIR/signal_agent"
echo "[下载] 完成"

echo ""
if [ -f "$CONFIG_FILE" ]; then
  echo "[配置] 发现现有配置文件: $CONFIG_FILE"
  if [ -z "$SIGNAL_TOKEN" ]; then
    SIGNAL_TOKEN=$(read_config_value token "$CONFIG_FILE")
  fi
  if [ -z "$SIGNAL_SERVER" ]; then
    SIGNAL_SERVER=$(read_config_value server "$CONFIG_FILE")
  fi
elif [ -n "$SIGNAL_TOKEN" ] && [ -n "$SIGNAL_SERVER" ]; then
  echo "[配置] 未找到配置文件，将根据参数生成"
  SIGNAL_STATE_DIR="$DATA_DIR/tunnel"
  mkdir -p "$SIGNAL_STATE_DIR"
  cat > "$CONFIG_FILE" <<EOF
# AWECloud Signaling Agent 配置文件（CloudIDE 自动生成）

[agent]
token = "$SIGNAL_TOKEN"
server = "$SIGNAL_SERVER"

[tunnel]
state_dir = "$SIGNAL_STATE_DIR"
enable_ssh = false

[cloudide]
socks = false
socks_addr = "127.0.0.1:1080"
dial_socket = "/tmp/signaling.sock"

[log]
level = "$SIGNAL_LOG_LEVEL"
file = "$DATA_DIR/logs/agent.log"
EOF
  chmod 600 "$CONFIG_FILE"
  echo "[配置] 已生成: $CONFIG_FILE"
elif [ "$UPGRADE_MODE" -eq 1 ]; then
  echo "[错误] 升级模式下未找到配置文件: $CONFIG_FILE" >&2
  exit 1
else
  echo "[跳过] 缺少 SIGNAL_TOKEN / SIGNAL_SERVER 且未找到配置文件"
  echo "  二进制已安装: $BIN_DIR/signal_agent"
  exit 0
fi

if [ -z "$SIGNAL_TOKEN" ] || [ -z "$SIGNAL_SERVER" ]; then
  echo "[错误] 缺少 token/server，无法注册容器客户端" >&2
  exit 1
fi

echo "[启动] 使用配置文件: $CONFIG_FILE"
nohup $SUDO_CMD "$BIN_DIR/signal_agent" -c "$CONFIG_FILE" -t "$SIGNAL_TOKEN" -s "$SIGNAL_SERVER" >/dev/null 2>&1 &
AGENT_PID=$!

sleep 3
if ps -p "$AGENT_PID" >/dev/null 2>&1; then
  echo "[启动] 客户端运行正常 (PID: $AGENT_PID)"
  echo ""
  echo "  日志: $DATA_DIR/logs/agent.log"
else
  echo "[启动] ✗ 启动失败"
  tail -n 20 "$DATA_DIR/logs/agent.log" 2>/dev/null || true
  exit 1
fi

SSH_CONFIG="$TRUE_HOME/.ssh/config"
if [[ -f "$SSH_CONFIG" ]] && grep -q "signal_agent dial" "$SSH_CONFIG" 2>/dev/null; then
  echo "[SSH] 已检测到 ProxyCommand 配置，跳过"
else
  mkdir -p "$TRUE_HOME/.ssh"
  chmod 700 "$TRUE_HOME/.ssh"
  echo "" >> "$SSH_CONFIG"
  echo "# AWECloud Signal 客户端 - 通过本地代理访问 Beagle 资源" >> "$SSH_CONFIG"
  echo "Host *.beagle" >> "$SSH_CONFIG"
  echo "    ProxyCommand $TRUE_HOME/.local/bin/signal_agent dial %h %p" >> "$SSH_CONFIG"
  chmod 600 "$SSH_CONFIG"
  echo "[SSH] 已添加 ProxyCommand 到 $SSH_CONFIG"
fi
