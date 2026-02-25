#!/bin/bash
set -e

# AWECloud Signaling Agent 安装脚本（CloudIDE 环境）
# 支持反复执行：停旧进程 → 下载最新二进制 → 启动
#
# 必需环境变量：
#   SIGNAL_TOKEN  - 部署 Token
#   SIGNAL_SERVER - Server 地址
#
# 可选环境变量：
#   SIGNAL_VERSION   - Agent 版本（默认 v0.2.3）
#   SIGNAL_LOG_LEVEL - 日志级别（默认 info）
#   SIGNAL_HTTP_SERVER - 下载服务器地址

# === 配置 ===
HTTP_SERVER="${SIGNAL_HTTP_SERVER:-https://cache.ali.wodcloud.com}"
AGENT_VERSION="${SIGNAL_VERSION:-v0.2.3}"

BIN_DIR="$HOME/.local/bin"
DATA_DIR="$HOME/.local/share/signal"
LOG_DIR="$DATA_DIR/logs"

# === 1. 停止旧进程 ===
echo "=========================================="
echo "  AWECloud Signaling Agent 安装脚本"
echo "=========================================="
echo ""

OLD_PIDS=$(pgrep -f "signal_agent" 2>/dev/null || true)
if [ -n "$OLD_PIDS" ]; then
  echo "[停止] 发现旧进程: $OLD_PIDS"
  kill $OLD_PIDS 2>/dev/null || true
  sleep 2
  # 强杀残留
  OLD_PIDS=$(pgrep -f "signal_agent" 2>/dev/null || true)
  if [ -n "$OLD_PIDS" ]; then
    kill -9 $OLD_PIDS 2>/dev/null || true
  fi
  echo "[停止] 旧进程已清理"
else
  echo "[停止] 无旧进程"
fi

# === 2. 创建目录 ===
mkdir -p "$BIN_DIR" "$DATA_DIR" "$LOG_DIR"

# === 3. 检测架构 ===
LOCAL_ARCH=$(uname -m)
case "$LOCAL_ARCH" in
  x86_64)  TARGET_ARCH="amd64" ;;
  aarch64) TARGET_ARCH="arm64" ;;
  armv8*)  TARGET_ARCH="arm64" ;;
  *)
    echo "不支持的架构: $LOCAL_ARCH"
    exit 1
    ;;
esac

# === 4. 下载二进制（每次都重新下载，MVP 阶段快速迭代） ===
AGENT_BINARY="signal_agent-${AGENT_VERSION}-linux-${TARGET_ARCH}"
DOWNLOAD_URL="${HTTP_SERVER}/vscode/awecloud-signaling/${AGENT_BINARY}"

echo ""
echo "[下载] Agent ${AGENT_VERSION} (${TARGET_ARCH})"
echo "[下载] $DOWNLOAD_URL"

# 先下载到临时文件，成功后再替换（避免覆盖正在运行的二进制导致写入失败）
TEMP_FILE="$DATA_DIR/${AGENT_BINARY}.tmp"
rm -f "$TEMP_FILE"

if command -v curl &> /dev/null; then
  if ! curl -fsSL -o "$TEMP_FILE" "$DOWNLOAD_URL"; then
    echo "[下载] 失败" >&2
    exit 1
  fi
elif command -v wget &> /dev/null; then
  if ! wget -q -O "$TEMP_FILE" "$DOWNLOAD_URL"; then
    echo "[下载] 失败" >&2
    exit 1
  fi
else
  echo "需要 curl 或 wget" >&2
  exit 1
fi

# 替换旧二进制
TARGET_BINARY="$DATA_DIR/$AGENT_BINARY"
rm -f "$TARGET_BINARY"
mv "$TEMP_FILE" "$TARGET_BINARY"
chmod +x "$TARGET_BINARY"
echo "[下载] 完成"

# 创建符号链接
rm -f "$BIN_DIR/signal_agent"
ln -s "$TARGET_BINARY" "$BIN_DIR/signal_agent"

# === 5. 检查环境变量 ===
echo ""
if [ -z "$SIGNAL_TOKEN" ] || [ -z "$SIGNAL_SERVER" ]; then
  echo "[跳过] 缺少环境变量 SIGNAL_TOKEN / SIGNAL_SERVER"
  echo "  二进制已安装: $BIN_DIR/signal_agent"
  exit 0
fi

# === 6. 启动 Agent ===
SIGNAL_STATE_DIR="$DATA_DIR/tunnel"
mkdir -p "$SIGNAL_STATE_DIR"

export SIGNAL_TOKEN="$SIGNAL_TOKEN"
export SIGNAL_SERVER="$SIGNAL_SERVER"
export SIGNAL_STATE_DIR="$SIGNAL_STATE_DIR"
if [ -n "$SIGNAL_LOG_LEVEL" ]; then
  export SIGNAL_LOG_LEVEL="$SIGNAL_LOG_LEVEL"
fi

echo "[启动] Server: $SIGNAL_SERVER"
nohup sudo -E "$BIN_DIR/signal_agent" > "$LOG_DIR/agent.log" 2>&1 &
AGENT_PID=$!

sleep 3
if ps -p "$AGENT_PID" > /dev/null 2>&1; then
  echo "[启动] ✓ Agent 运行正常 (PID: $AGENT_PID)"
  echo ""
  echo "  日志: $LOG_DIR/agent.log"
else
  echo "[启动] ✗ 启动失败"
  tail -n 20 "$LOG_DIR/agent.log" 2>/dev/null || true
  exit 1
fi
