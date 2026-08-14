#!/bin/bash
set -euo pipefail

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
if [[ "$EUID" -eq 0 && "$HOME" != "/root" ]]; then
  TRUE_HOME="/root"
elif command -v getent >/dev/null 2>&1; then
  TRUE_HOME=$(getent passwd "$(whoami)" | cut -d: -f6)
fi

BIN_DIR="$TRUE_HOME/.local/bin"
DATA_DIR="$TRUE_HOME/.local/share/signal"
CONFIG_FILE="$DATA_DIR/agent.toml"
SUDO_CMD=""
TARGET_ARCH=""
DOWNLOAD_URL=""
ARTIFACT_SHA=""
TEMP_FILE=""
TARGET_BINARY=""
STAGED_BINARY_PATH=""
AGENT_PID=""

read_config_value() {
  local key="$1"
  local file="$2"
  grep -m1 "^[[:space:]]*${key}[[:space:]]*=" "$file" 2>/dev/null | cut -d'"' -f2
}

json_string_field() {
  local json="$1"
  local key="$2"
  printf '%s' "$json" |
    tr -d '\r\n' |
    grep -o "\"${key}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" |
    head -n 1 |
    cut -d'"' -f4
}

json_object_string_field() {
  local json="$1"
  local object="$2"
  local key="$3"
  local object_body
  object_body=$(printf '%s' "$json" | tr -d '\r\n' |
    sed -n "s/.*\"${object}\"[[:space:]]*:[[:space:]]*{\([^}]*\)}.*/\1/p")
  json_string_field "$object_body" "$key"
}

fetch_text() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$url"
  else
    echo "[错误] 需要 curl 或 wget" >&2
    return 1
  fi
}

download_file() {
  local url="$1"
  local output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$output" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$output" "$url"
  else
    echo "[错误] 需要 curl 或 wget" >&2
    return 1
  fi
}

resolve_artifact() {
  local manifest_url="${HTTP_SERVER%/}/vscode/awecloud-signaling/signal_agent-version.json"
  local manifest manifest_version platform

  echo ""
  echo "[版本] 获取发布清单..."
  if ! manifest=$(fetch_text "$manifest_url"); then
    echo "[错误] 无法获取 Agent 发布清单: $manifest_url" >&2
    return 1
  fi

  platform="linux-${TARGET_ARCH}"
  manifest_version=$(json_string_field "$manifest" version || true)
  DOWNLOAD_URL=$(json_object_string_field "$manifest" files "$platform" || true)
  ARTIFACT_SHA=$(json_object_string_field "$manifest" sha256 "$platform" || true)

  if [[ -z "$manifest_version" || -z "$DOWNLOAD_URL" || -z "$ARTIFACT_SHA" ]]; then
    echo "[错误] 发布清单缺少 ${platform} 的版本、下载地址或 SHA256" >&2
    return 1
  fi
  if [[ ! "$ARTIFACT_SHA" =~ ^[0-9a-f]{64}$ ]]; then
    echo "[错误] 发布清单包含非法 SHA256" >&2
    return 1
  fi
  if [[ ! "$DOWNLOAD_URL" =~ ^https?:// ]]; then
    echo "[错误] 发布清单包含非法下载地址" >&2
    return 1
  fi
  if [[ -n "$AGENT_VERSION" && "$AGENT_VERSION" != "$manifest_version" ]]; then
    echo "[错误] 指定版本 $AGENT_VERSION 与当前发布清单 $manifest_version 不一致" >&2
    return 1
  fi

  AGENT_VERSION="$manifest_version"
  echo "[版本] 最新版本: $AGENT_VERSION"
}

stage_client() {
  local actual_sha artifact_name
  resolve_artifact

  artifact_name="signal_agent-${AGENT_VERSION}-linux-${TARGET_ARCH}-${ARTIFACT_SHA}"
  TEMP_FILE="$DATA_DIR/${artifact_name}.tmp.$$"
  TARGET_BINARY="$DATA_DIR/$artifact_name"

  echo ""
  echo "[下载] 客户端 ${AGENT_VERSION} (${TARGET_ARCH})"
  echo "[下载] $DOWNLOAD_URL"
  rm -f "$TEMP_FILE"
  if ! download_file "$DOWNLOAD_URL" "$TEMP_FILE"; then
    rm -f "$TEMP_FILE"
    echo "[错误] 下载客户端制品失败，旧客户端未停止" >&2
    return 1
  fi

  actual_sha=$(sha256sum "$TEMP_FILE" | awk '{print $1}')
  if [[ "$actual_sha" != "$ARTIFACT_SHA" ]]; then
    rm -f "$TEMP_FILE"
    echo "[错误] 客户端 SHA256 校验失败，旧客户端未停止" >&2
    return 1
  fi

  chmod 755 "$TEMP_FILE"
  if [[ -f "$TARGET_BINARY" ]] &&
    [[ "$(sha256sum "$TARGET_BINARY" | awk '{print $1}')" == "$ARTIFACT_SHA" ]]; then
    rm -f "$TEMP_FILE"
  else
    mv -f "$TEMP_FILE" "$TARGET_BINARY"
  fi
  STAGED_BINARY_PATH="$TARGET_BINARY"
  echo "[下载] 校验完成"
}

switch_client_link() {
  local target="$1"
  local next_link="${BIN_DIR}/.signal_agent.new.$$"
  rm -f "$next_link"
  ln -s "$target" "$next_link"
  mv -Tf "$next_link" "$BIN_DIR/signal_agent"
}

activate_staged_client() {
  switch_client_link "$STAGED_BINARY_PATH"
}

stop_client() {
  local old_pids
  old_pids=$(pgrep -x signal_agent 2>/dev/null || true)
  if [[ -z "$old_pids" ]]; then
    echo "[停止] 无旧进程"
    return 0
  fi

  echo "[停止] 发现旧进程: $old_pids"
  $SUDO_CMD kill $old_pids 2>/dev/null || true
  for _ in $(seq 1 10); do
    old_pids=$(pgrep -x signal_agent 2>/dev/null || true)
    [[ -z "$old_pids" ]] && break
    sleep 1
  done
  if [[ -n "$old_pids" ]]; then
    $SUDO_CMD kill -9 $old_pids 2>/dev/null || true
    sleep 1
  fi
  if pgrep -x signal_agent >/dev/null 2>&1; then
    echo "[错误] 无法停止旧客户端" >&2
    return 1
  fi
  echo "[停止] 旧进程已清理"
}

read_toml_section_value() {
  local section="$1"
  local key="$2"
  local file="$3"
  awk -F= -v section="$section" -v key="$key" '
    /^[[:space:]]*\[/ {
      current = $0
      gsub(/^[[:space:]]*\[|\][[:space:]]*$/, "", current)
      next
    }
    current == section {
      name = $1
      gsub(/[[:space:]]/, "", name)
      if (name == key) {
        value = substr($0, index($0, "=") + 1)
        sub(/[[:space:]]*#.*/, "", value)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
        gsub(/^"|"$/, "", value)
        print value
        exit
      }
    }
  ' "$file"
}

read_process_args() {
  local pid="$1"
  tr '\0' '\n' < "/proc/${pid}/cmdline" 2>/dev/null
}

find_client_pid() {
  local expected_binary pid process_args process_command process_binary
  expected_binary=$(readlink -f "$BIN_DIR/signal_agent")
  for pid in $(pgrep -x signal_agent 2>/dev/null || true); do
    process_args=$(read_process_args "$pid" || true)
    process_command=$(printf '%s\n' "$process_args" | head -n 1)
    process_binary=$(readlink -f "$process_command" 2>/dev/null || true)
    if [[ "$process_binary" == "$expected_binary" ]] &&
      printf '%s\n' "$process_args" | grep -Fxq -- "$CONFIG_FILE"; then
      printf '%s\n' "$pid"
      return 0
    fi
  done
  return 1
}

wait_client_ready() {
  local health_port health_url response
  health_port=$(read_toml_section_value health port "$CONFIG_FILE" || true)
  health_port="${health_port:-8090}"
  health_url="http://127.0.0.1:${health_port}/health/ready"

  for _ in $(seq 1 30); do
    response=""
    if command -v curl >/dev/null 2>&1; then
      response=$(curl -fsS --max-time 2 "$health_url" 2>/dev/null || true)
    elif command -v wget >/dev/null 2>&1; then
      response=$(wget -qO- -T 2 "$health_url" 2>/dev/null || true)
    fi
    if [[ "$response" =~ \"status\"[[:space:]]*:[[:space:]]*\"ready\" ]] &&
      [[ "$response" =~ \"grpc_connected\"[[:space:]]*:[[:space:]]*true ]] &&
      [[ "$response" =~ \"tailscale_connected\"[[:space:]]*:[[:space:]]*true ]]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

start_client() {
  local client_pid=""
  echo "[启动] 使用配置文件: $CONFIG_FILE"
  (
    cd "$DATA_DIR"
    nohup $SUDO_CMD "$BIN_DIR/signal_agent" -c "$CONFIG_FILE" -t "$SIGNAL_TOKEN" -s "$SIGNAL_SERVER" >/dev/null 2>&1 &
  )

  for _ in $(seq 1 10); do
    client_pid=$(find_client_pid || true)
    [[ -n "$client_pid" ]] && break
    sleep 1
  done
  if [[ -z "$client_pid" ]]; then
    echo "[启动] 未找到真实的 signal_agent 客户端进程" >&2
    tail -n 20 "$DATA_DIR/logs/agent.log" 2>/dev/null || true
    return 1
  fi
  if ! wait_client_ready; then
    echo "[启动] 客户端未在超时内恢复 gRPC/Tailscale 就绪状态" >&2
    tail -n 20 "$DATA_DIR/logs/agent.log" 2>/dev/null || true
    return 1
  fi

  AGENT_PID="$client_pid"
  echo "[启动] 客户端运行正常 (PID: $AGENT_PID)"
  return 0
}

rollback_client() {
  local previous_binary="$1"
  echo "[回滚] 正在恢复旧客户端..." >&2
  stop_client || true
  if [[ -z "$previous_binary" ]] || ! switch_client_link "$previous_binary"; then
    echo "[回滚] 恢复旧客户端链接失败，需要通过旁路连接人工处理" >&2
    return 1
  fi
  if ! start_client; then
    echo "[回滚] 旧客户端启动失败，需要通过旁路连接人工处理" >&2
    return 1
  fi
  echo "[回滚] 旧客户端已恢复" >&2
}

upgrade_client() {
  local previous_binary

  # 网络、清单和制品校验全部在旧客户端运行期间完成。
  stage_client

  previous_binary=$(readlink -f "$BIN_DIR/signal_agent" 2>/dev/null || true)
  if [[ -z "$previous_binary" ]]; then
    echo "[错误] 无法读取当前客户端链接，拒绝执行无回滚升级" >&2
    return 1
  fi

  stop_client
  if ! activate_staged_client; then
    rollback_client "$previous_binary" || true
    return 1
  fi
  if ! start_client; then
    rollback_client "$previous_binary" || true
    return 1
  fi
  echo "[升级] 完成"
}

deploy_client() {
  local previous_binary
  stage_client
  previous_binary=$(readlink -f "$BIN_DIR/signal_agent" 2>/dev/null || true)
  stop_client
  if ! activate_staged_client; then
    [[ -n "$previous_binary" ]] && rollback_client "$previous_binary" || true
    return 1
  fi
  if ! start_client; then
    [[ -n "$previous_binary" ]] && rollback_client "$previous_binary" || true
    return 1
  fi
}

prepare_client_config() {
  if [[ -f "$CONFIG_FILE" ]]; then
    echo "[配置] 发现现有配置文件: $CONFIG_FILE"
    if [[ -z "$SIGNAL_TOKEN" ]]; then
      SIGNAL_TOKEN=$(read_config_value token "$CONFIG_FILE")
    fi
    if [[ -z "$SIGNAL_SERVER" ]]; then
      SIGNAL_SERVER=$(read_config_value server "$CONFIG_FILE")
    fi
  elif [[ -n "$SIGNAL_TOKEN" && -n "$SIGNAL_SERVER" ]]; then
    local signal_state_dir="$DATA_DIR/tunnel"
    echo "[配置] 未找到配置文件，将根据参数生成"
    mkdir -p "$signal_state_dir"
    cat > "$CONFIG_FILE" <<EOF
# AWECloud Signaling Agent 配置文件（CloudIDE 自动生成）

[agent]
token = "$SIGNAL_TOKEN"
server = "$SIGNAL_SERVER"

[tunnel]
state_dir = "$signal_state_dir"
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
  elif [[ "$UPGRADE_MODE" -eq 1 ]]; then
    echo "[错误] 升级模式下未找到配置文件: $CONFIG_FILE" >&2
    return 1
  else
    echo "[错误] 缺少 SIGNAL_TOKEN / SIGNAL_SERVER，无法安装容器客户端" >&2
    return 1
  fi

  if [[ -z "$SIGNAL_TOKEN" || -z "$SIGNAL_SERVER" ]]; then
    echo "[错误] 配置缺少 token/server，无法启动容器客户端" >&2
    return 1
  fi
}

ensure_ssh_config() {
  local ssh_config="$TRUE_HOME/.ssh/config"
  if [[ -f "$ssh_config" ]] && grep -q "signal_agent dial" "$ssh_config" 2>/dev/null; then
    echo "[SSH] 已检测到 ProxyCommand 配置，跳过"
    return
  fi

  mkdir -p "$TRUE_HOME/.ssh"
  chmod 700 "$TRUE_HOME/.ssh"
  echo "" >> "$ssh_config"
  echo "# AWECloud Signal 客户端 - 通过本地代理访问 Beagle 资源" >> "$ssh_config"
  echo "Host *.beagle" >> "$ssh_config"
  echo "    ProxyCommand $TRUE_HOME/.local/bin/signal_agent dial %h %p" >> "$ssh_config"
  chmod 600 "$ssh_config"
  echo "[SSH] 已添加 ProxyCommand 到 $ssh_config"
}

parse_args() {
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
}

main() {
  parse_args "$@"

  echo "=========================================="
  echo "  AWECloud Signaling Agent 客户端安装脚本"
  echo "=========================================="
  echo "  此下载入口 install_signal.sh 面向 Container Client；"
  echo "  Agent 请走 install_agent.sh"
  echo ""

  if command -v sudo >/dev/null 2>&1; then
    SUDO_CMD="sudo"
  fi
  mkdir -p "$BIN_DIR" "$DATA_DIR" "$DATA_DIR/logs"

  case "$(uname -m)" in
    x86_64) TARGET_ARCH="amd64" ;;
    aarch64|armv8*) TARGET_ARCH="arm64" ;;
    *)
      echo "[错误] 不支持的架构: $(uname -m)" >&2
      return 1
      ;;
  esac

  prepare_client_config
  if [[ "$UPGRADE_MODE" -eq 1 ]]; then
    upgrade_client
  else
    deploy_client
  fi
  ensure_ssh_config
  echo ""
  echo "  日志: $DATA_DIR/logs/agent.log"
}

main "$@"
