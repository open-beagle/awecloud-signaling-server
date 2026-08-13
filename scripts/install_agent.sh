#!/bin/bash
# AWECloud Agent 一键安装脚本
# 用法: curl -fsSL https://server/api/v1/download/install.sh | sudo bash -s -- -n <name> -t <token> -s <server>

# 注意：不使用 set -e，因为管道命令（curl | grep | cut）在 set -e 下
# 任何一步失败都会导致脚本静默退出，无法输出错误信息

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 默认值
AGENT_NAME=""
AGENT_TOKEN=""
SERVER_ADDRESS=""
DEVICE_NAME=""  # 设备名，默认使用 hostname
ENABLE_SSH="true"  # 默认启用 SSH
SSH_PORT=""        # Agent 节点实际 SSH 端口，留空则自动检测，检测失败回退 22
SSH_PORT_EXPLICIT="false"
UPGRADE_MODE="false"
UNINSTALL_MODE="false"
DEPLOY_MODE="false"  # 部署模式：使用 Token 自动注册

# 安装路径
DOWNLOAD_DIR="/etc/kubernetes/downloads"
INSTALL_DIR="/opt/bin"
BINARY_NAME="signal_agent"
CONFIG_DIR="/etc/kubernetes/config"
DATA_DIR="/etc/kubernetes/data/signaling"
TUNNEL_STATE_DIR="${DATA_DIR}/tunnel"
CONFIG_FILE="${CONFIG_DIR}/k8s-signaling.toml"
SERVICE_NAME="k8s-signaling"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
SSH_BANNER_PROFILE="/etc/profile.d/awecloud-signaling-ssh-banner.sh"

# 打印消息
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 显示帮助
show_help() {
    cat << EOF
AWECloud Agent 安装脚本

用法:
  install.sh [选项]

选项:
  -n, --name <name>       Agent 名称（必填，如 beijing）
  -t, --token <token>     认证 Token（必填，从 Server 获取）
  -s, --server <url>      Server 地址（必填，如 https://signal.example.com）
  -d, --device <name>     设备名（可选，默认使用 hostname）
      --no-ssh            禁用 SSH（默认启用）
      --ssh-port <port>   Agent 节点实际 SSH 端口（默认 22）
      --deploy            部署模式：使用 Token 自动注册获取配置
  -u, --upgrade           升级模式，保留现有配置
  -U, --uninstall         卸载 Agent
  -h, --help              显示帮助信息

示例:
  # 部署模式（推荐）：使用 Web 生成的 Token 自动注册
  curl -fsSL https://server/api/v1/download/install.sh | \\
    sudo bash -s -- --deploy -t <TOKEN> -s https://signal.example.com

  # 传统安装（手动指定 Agent 名称）
  curl -fsSL https://server/api/v1/download/install.sh | \\
    sudo bash -s -- -n beijing -t <TOKEN> -s https://signal.example.com

  # 安装（指定设备名）
  curl -fsSL https://server/api/v1/download/install.sh | \\
    sudo bash -s -- -n beijing -t <TOKEN> -s https://signal.example.com -d beagle-xxx

  # 升级
  curl -fsSL https://server/api/v1/download/install.sh | \\
    sudo bash -s -- --upgrade -s https://signal.example.com

  # 卸载
  curl -fsSL https://server/api/v1/download/install.sh | \\
    sudo bash -s -- --uninstall
EOF
}

# 解析参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -n|--name)
                AGENT_NAME="$2"
                shift 2
                ;;
            -t|--token)
                AGENT_TOKEN="$2"
                shift 2
                ;;
            -s|--server)
                SERVER_ADDRESS="$2"
                shift 2
                ;;
            -d|--device)
                DEVICE_NAME="$2"
                shift 2
                ;;
            --no-ssh)
                ENABLE_SSH="false"
                shift
                ;;
            --ssh-port)
                SSH_PORT="$2"
                SSH_PORT_EXPLICIT="true"
                shift 2
                ;;
            --deploy)
                DEPLOY_MODE="true"
                shift
                ;;
            -u|--upgrade)
                UPGRADE_MODE="true"
                shift
                ;;
            -U|--uninstall)
                UNINSTALL_MODE="true"
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                error "未知参数: $1"
                ;;
        esac
    done
    
    # 如果没有指定设备名，使用 hostname
    if [[ -z "$DEVICE_NAME" ]]; then
        DEVICE_NAME=$(hostname)
    fi
}

# 检查 root 权限
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "此脚本需要 root 权限运行，请使用 sudo"
    fi
}

# 检测系统架构
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        *)
            error "不支持的架构: $arch"
            ;;
    esac
}

is_tcp_listening() {
    local port="$1"
    if command -v ss &> /dev/null; then
        ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "(^|:|\\])${port}$"
        return $?
    fi
    if command -v netstat &> /dev/null; then
        netstat -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "(^|:|\\])${port}$"
        return $?
    fi
    return 1
}

detect_ssh_port() {
    if [[ "$ENABLE_SSH" != "true" ]]; then
        [[ -z "$SSH_PORT" ]] && SSH_PORT="22"
        return
    fi

    if [[ "$SSH_PORT_EXPLICIT" == "true" ]]; then
        info "使用指定 SSH 端口: ${SSH_PORT}"
        return
    fi

    local detected=""
    local current_ssh_port=""
    if [[ -n "${SSH_CONNECTION:-}" ]]; then
        current_ssh_port=$(echo "$SSH_CONNECTION" | awk '{print $4}')
        if [[ "$current_ssh_port" =~ ^[0-9]+$ ]] && is_tcp_listening "$current_ssh_port"; then
            detected="$current_ssh_port"
        fi
    fi

    if [[ -z "$detected" ]]; then
        local listen22="false"
        local listen2222="false"
        is_tcp_listening "22" && listen22="true"
        is_tcp_listening "2222" && listen2222="true"

        if [[ "$listen22" == "true" && "$listen2222" != "true" ]]; then
            detected="22"
        elif [[ "$listen2222" == "true" && "$listen22" != "true" ]]; then
            detected="2222"
        elif [[ "$listen22" == "true" && "$listen2222" == "true" ]]; then
            detected="22"
            warn "检测到 22 和 2222 同时监听，默认使用 22，可通过 --ssh-port 覆盖"
        fi
    fi

    if [[ -z "$detected" ]]; then
        detected="22"
        warn "未能自动检测 SSH 端口，默认使用 22，可通过 --ssh-port 覆盖"
    fi

    SSH_PORT="$detected"
    info "Agent SSH 端口: ${SSH_PORT}"
}

# 下载 Agent 二进制
download_agent() {
    local arch=$(detect_arch)
    
    info "检测到架构: ${arch}"
    info "获取最新版本..."
    
    # 获取版本信息
    local version_url="${SERVER_ADDRESS}/api/v1/download/agent/version"
    local version_response=""
    local version=""
    local artifact_sha=""
    
    # 先获取完整响应，再解析，避免管道失败导致脚本退出
    if command -v curl &> /dev/null; then
        version_response=$(curl -fsSL "$version_url" 2>&1) || true
    elif command -v wget &> /dev/null; then
        version_response=$(wget -qO- "$version_url" 2>&1) || true
    fi
    
    if [[ -n "$version_response" ]]; then
        version=$(echo "$version_response" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || true)
        artifact_sha=$(echo "$version_response" | grep -o "\"linux-${arch}\":\"[0-9a-f]\{64\}\"" | head -1 | cut -d'"' -f4 || true)
    fi
    
    if [[ -z "$version" || ! "$artifact_sha" =~ ^[0-9a-f]{64}$ ]]; then
        error "Agent 版本信息缺少 version 或 linux-${arch} SHA256（URL: ${version_url}）"
    fi
    
    info "最新版本: ${version}"
    
    # 构建下载 URL
    local download_url="${SERVER_ADDRESS}/api/v1/download/agent?os=linux&arch=${arch}"
    
    # 二进制文件名使用不可变摘要，同版本不同 Commit 不会互相覆盖
    local binary_filename="${BINARY_NAME}-${artifact_sha}"
    local binary_path="${DOWNLOAD_DIR}/${binary_filename}"
    local symlink_path="${INSTALL_DIR}/${BINARY_NAME}"
    
    info "下载 Agent..."
    
    # 创建下载目录
    mkdir -p "$DOWNLOAD_DIR" || error "创建目录失败: $DOWNLOAD_DIR"
    mkdir -p "$INSTALL_DIR" || error "创建目录失败: $INSTALL_DIR"
    
    # 下载到临时文件
    local tmp_file
    tmp_file=$(mktemp)
    
    if command -v curl &> /dev/null; then
        if ! curl -fsSL -o "$tmp_file" "$download_url"; then
            rm -f "$tmp_file"
            error "下载失败（URL: ${download_url}）"
        fi
    elif command -v wget &> /dev/null; then
        if ! wget -q -O "$tmp_file" "$download_url"; then
            rm -f "$tmp_file"
            error "下载失败（URL: ${download_url}）"
        fi
    else
        error "需要 curl 或 wget"
    fi
    
    if [[ ! -f "$tmp_file" ]] || [[ ! -s "$tmp_file" ]]; then
        rm -f "$tmp_file"
        error "下载的文件无效"
    fi

    local downloaded_sha
    downloaded_sha=$(sha256sum "$tmp_file" | awk '{print $1}')
    if [[ "$downloaded_sha" != "$artifact_sha" ]]; then
        rm -f "$tmp_file"
        error "下载的 Agent SHA256 校验失败"
    fi
    
    # 移动到目标位置
    mv "$tmp_file" "$binary_path"
    chmod +x "$binary_path"
    
    # 创建软链接
    ln -sf "$binary_path" "$symlink_path"
    
    info "Agent ${version} 已安装"
    info "  二进制: ${binary_path}"
    info "  软链接: ${symlink_path}"
}

# 创建目录
create_directories() {
    info "创建目录..."
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR"
    chmod 755 "$CONFIG_DIR"
    chmod 755 "$DATA_DIR"
}

# 生成配置文件
generate_config() {
    info "生成配置文件..."
    
    # 创建日志目录
    mkdir -p "${DATA_DIR}/logs"
    
    cat > "$CONFIG_FILE" << EOF
# AWECloud Agent 配置文件
# 由安装脚本自动生成

[agent]
token = "${AGENT_TOKEN}"
server = "${SERVER_ADDRESS}"

# Tunnel 配置
[tunnel]
state_dir = "${TUNNEL_STATE_DIR}"
state_sync_interval = 5
enable_ssh = ${ENABLE_SSH}
ssh_port = ${SSH_PORT}

# Visitor
[visitor]
listen_addr = ""   # 留空自动检测局域网 IP

# 日志配置
[log]
level = "info"
file = "$DATA_DIR/logs/agent.log"

EOF

    chmod 600 "$CONFIG_FILE"
    info "配置文件已生成: ${CONFIG_FILE}"
}

# 安装 systemd 服务
install_service() {
    info "安装 systemd 服务..."

    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=K8S Signaling Agent
After=network-online.target
Wants=network-online.target

# 启动失败重试限制（5次/5分钟）
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=root
Group=root
# 工作目录
WorkingDirectory=/etc/kubernetes

# 二进制文件路径
ExecStart=${INSTALL_DIR}/${BINARY_NAME} -c ${CONFIG_FILE}

# 重启策略
Restart=always
RestartSec=10s

# 资源限制
LimitNOFILE=65536
LimitNPROC=4096

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload || error "重新加载 systemd 配置失败"
    install_ssh_banner_profile
    systemctl enable "$SERVICE_NAME" || error "启用服务失败"
    systemctl restart "$SERVICE_NAME" || error "启动服务失败"
    
    info "服务已启动"
}

install_ssh_banner_profile() {
    cat > "$SSH_BANNER_PROFILE" << EOF
# AWECloud Signaling detailed SSH banner. Interactive PTY sessions only.
if [ -n "\${SSH_TTY:-}" ] && [ -n "\${SSH_CONNECTION:-}" ] && [ -x "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    AWE_SSH_REMOTE_IP=\${SSH_CONNECTION%% *}
    AWE_SSH_CONNECTION_REST=\${SSH_CONNECTION#* }
    AWE_SSH_REMOTE_PORT=\${AWE_SSH_CONNECTION_REST%% *}
    "${INSTALL_DIR}/${BINARY_NAME}" be-child banner "\${AWE_SSH_REMOTE_IP}" "\${AWE_SSH_REMOTE_PORT}" 2>/dev/null || true
    unset AWE_SSH_REMOTE_IP AWE_SSH_CONNECTION_REST AWE_SSH_REMOTE_PORT
fi
EOF
    chmod 0644 "$SSH_BANNER_PROFILE" || error "设置 SSH 横幅脚本权限失败"
}

# 准备重新部署 Agent
prepare_redeploy_agent() {
    info "重新部署模式：停止旧服务并备份旧隧道状态..."

    if systemctl cat "$SERVICE_NAME" >/dev/null 2>&1; then
        systemctl stop "$SERVICE_NAME" || error "停止旧服务失败"
    fi

    if [[ -d "$TUNNEL_STATE_DIR" ]]; then
        local timestamp backup_dir
        timestamp="$(date +%Y%m%d%H%M%S)-$$"
        backup_dir="${TUNNEL_STATE_DIR}.bak.${timestamp}"
        mv "$TUNNEL_STATE_DIR" "$backup_dir" || error "备份旧隧道状态失败"
        info "检测到旧隧道状态，已备份: ${backup_dir}"
    fi
}

# 升级 Agent
upgrade_agent() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        error "升级要求现有配置文件: ${CONFIG_FILE}"
    fi
    if [[ "$(stat -c %a "$CONFIG_FILE" 2>/dev/null)" != "600" ]]; then
        error "升级要求配置文件权限为 0600: ${CONFIG_FILE}"
    fi
    local config_sha_before
    config_sha_before=$(sha256sum "$CONFIG_FILE" | awk '{print $1}') || error "无法读取现有配置文件"

    info "升级 Agent..."
    
    # 停止服务
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    
    # 下载新版本（会自动更新软链接）
    download_agent
    
    # 安装新的 systemd 服务（覆盖旧的）
    install_service

    local config_sha_after
    config_sha_after=$(sha256sum "$CONFIG_FILE" | awk '{print $1}') || error "无法复核现有配置文件"
    if [[ "$config_sha_before" != "$config_sha_after" ]]; then
        error "升级过程中配置文件发生变化"
    fi
    
    info "升级完成"
}

# 卸载 Agent
uninstall_agent() {
    info "卸载 Agent..."
    
    # 停止并禁用服务
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    
    # 删除文件
    rm -f "$SERVICE_FILE"
    rm -f "$SSH_BANNER_PROFILE"
    rm -f "${INSTALL_DIR}/${BINARY_NAME}"
    rm -f "${INSTALL_DIR}/signaling"  # 旧软链接
    rm -rf "${DOWNLOAD_DIR}/${BINARY_NAME}-"*
    rm -rf "${DOWNLOAD_DIR}/signaling-"*  # 旧二进制
    
    systemctl daemon-reload
    
    info "Agent 已卸载"
    info "配置和数据目录已保留: ${CONFIG_DIR}, ${DATA_DIR}"
    info "如需完全删除，请手动执行: rm -rf ${CONFIG_FILE} ${DATA_DIR}"
}

# 生成设备指纹（统一使用 hostname 的 SHA256 哈希）
generate_fingerprint() {
    local hostname=$(hostname)
    echo -n "$hostname" | sha256sum | awk '{print $1}'
}

# 部署模式：使用 Token 注册获取配置
deploy_with_token() {
    info "部署模式：使用 Token 注册..."
    
    local fingerprint=$(generate_fingerprint)
    local register_url="${SERVER_ADDRESS}/api/v1/register"
    
    info "设备指纹: ${fingerprint}"
    info "注册地址: ${register_url}"
    
    # 构建请求体
    local request_body=$(cat << EOF
{
    "token": "${AGENT_TOKEN}",
    "device_fingerprint": "${fingerprint}",
    "device_name": "${DEVICE_NAME}"
}
EOF
)
    
    # 发送注册请求
    local response=""
    if command -v curl &> /dev/null; then
        response=$(curl -fsSL -X POST \
            -H "Content-Type: application/json" \
            -d "$request_body" \
            "$register_url" 2>&1) || error "注册失败: $response"
    elif command -v wget &> /dev/null; then
        response=$(wget -qO- --post-data="$request_body" \
            --header="Content-Type: application/json" \
            "$register_url" 2>&1) || error "注册失败: $response"
    else
        error "需要 curl 或 wget"
    fi
    
    # 解析响应（Server 返回格式: {"success":true,"data":{"user_role":"agent","device_name":"...","user_name":"..."}}）
    local success=$(echo "$response" | grep -o '"success":\s*true' || true)
    if [[ -z "$success" ]]; then
        local message=$(echo "$response" | grep -o '"message":"[^"]*"' | cut -d'"' -f4)
        error "注册失败: ${message:-$response}"
    fi
    
    # 从 data.device_name 提取 Agent 名称（优先），回退到 data.user_name
    AGENT_NAME=$(echo "$response" | grep -o '"device_name":"[^"]*"' | head -1 | cut -d'"' -f4)
    if [[ -z "$AGENT_NAME" ]]; then
        AGENT_NAME=$(echo "$response" | grep -o '"user_name":"[^"]*"' | head -1 | cut -d'"' -f4)
    fi
    
    if [[ -z "$AGENT_NAME" ]]; then
        error "注册响应缺少 Agent 名称"
    fi
    
    # Token 保持传入的值（注册接口不返回新 Token）
    
    info "注册成功！"
    info "  Agent 名称: ${AGENT_NAME}"
}

# 验证参数
validate_args() {
    if [[ "$UNINSTALL_MODE" == "true" ]]; then
        return
    fi
    
    if [[ "$UPGRADE_MODE" == "true" ]]; then
        return
    fi
    
    # 部署模式只需要 token 和 server
    if [[ "$DEPLOY_MODE" == "true" ]]; then
        if [[ -z "$AGENT_TOKEN" ]]; then
            error "缺少参数: --token (-t)"
        fi
        if [[ -z "$SERVER_ADDRESS" ]]; then
            error "缺少参数: --server (-s)"
        fi
        return
    fi
    
    # 传统模式需要 name, token, server
    if [[ -z "$AGENT_NAME" ]]; then
        error "缺少参数: --name (-n)"
    fi
    
    if [[ -z "$AGENT_TOKEN" ]]; then
        error "缺少参数: --token (-t)"
    fi
    
    if [[ -z "$SERVER_ADDRESS" ]]; then
        error "缺少参数: --server (-s)"
    fi
}

# 主函数
main() {
    echo "========================================"
    echo "  AWECloud Agent 安装脚本"
    echo "========================================"
    echo
    
     parse_args "$@"
     # 如果未指定 Server 地址，则使用默认值
     if [[ -z "$SERVER_ADDRESS" ]]; then
       SERVER_ADDRESS="https://signal.wodcloud.com"
     fi
     check_root
     validate_args
    
    if [[ "$UNINSTALL_MODE" == "true" ]]; then
        uninstall_agent
        exit 0
    fi
    
    if [[ "$UPGRADE_MODE" == "true" ]]; then
        upgrade_agent
        exit 0
    fi
    
    # 部署模式：先注册获取配置
    if [[ "$DEPLOY_MODE" == "true" ]]; then
        deploy_with_token
    fi

    detect_ssh_port
    
    # 全新安装
    download_agent
    create_directories

    if [[ "$DEPLOY_MODE" == "true" ]]; then
        prepare_redeploy_agent
    fi

    generate_config
    install_service
    
    echo
    info "安装完成！"
    echo
    echo "Agent 名称: ${AGENT_NAME}"
    echo "设备名称:   ${DEVICE_NAME}"
    echo "SSH 功能:   ${ENABLE_SSH}"
    echo "SSH 端口:   ${SSH_PORT}"
    echo
    echo "常用命令:"
    echo "  查看状态: systemctl status ${SERVICE_NAME}"
    echo "  查看日志: journalctl -u ${SERVICE_NAME} -f"
    echo "  重启服务: systemctl restart ${SERVICE_NAME}"
    echo
}

main "$@"
