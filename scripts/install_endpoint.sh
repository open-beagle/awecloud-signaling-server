#!/bin/bash
# AWECloud Endpoint 一键安装/升级/卸载脚本
#
# 安装:
#   curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
#     sudo bash -s -- -a 192.168.1.1:50052 -t ep_xxxxxxxx -s https://signal.example.com
#
# 升级:
#   curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
#     sudo bash -s -- --upgrade -s https://signal.example.com
#
# 卸载:
#   curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
#     sudo bash -s -- --uninstall

# 注意：不使用 set -e，因为管道命令（curl | grep | cut）在 set -e 下
# 任何一步失败都会导致脚本静默退出，无法输出错误信息

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 参数
AGENT_ADDRESS=""       # Agent 内网 gRPC 地址
ENDPOINT_TOKEN=""      # 注册令牌（ep_ 前缀）
ENDPOINT_NAME=""       # Endpoint 名称（默认 hostname）
SERVER_ADDRESS=""      # Server 地址（用于下载二进制）
SSH_ENABLED="true"     # 默认启用 SSH 能力
SSH_HOST="127.0.0.1"   # SSH 目标地址
SSH_PORT="22"          # SSH 端口
K8S_ENABLED="false"    # K8S API 能力
K8S_API_SERVER=""      # K8S API Server 地址
SVC_ENABLED="false"    # K8S Service 能力
UPGRADE_MODE="false"
UNINSTALL_MODE="false"

# 路径
DOWNLOAD_DIR="/etc/kubernetes/downloads"
INSTALL_DIR="/opt/bin"
BINARY_NAME="signal_endpoint"
CONFIG_DIR="/etc/kubernetes/config"
CONFIG_FILE="${CONFIG_DIR}/endpoint.toml"
SERVICE_NAME="signal-endpoint"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

show_help() {
    cat << 'EOF'
AWECloud Endpoint 安装脚本

用法:
  install_endpoint.sh [选项]

选项:
  -a, --agent <addr>      Agent 内网 gRPC 地址（必填，如 192.168.1.1:50052）
  -t, --token <token>     注册令牌（必填，从 Web Agent 详情页复制）
  -s, --server <url>      Server 地址（必填，用于下载二进制）
  -n, --name <name>       Endpoint 名称（可选，默认 hostname）
  --ssh-host <host>       SSH 目标地址（默认 127.0.0.1）
  --ssh-port <port>       SSH 端口（默认 22）
  --no-ssh                禁用 SSH 能力
  --k8s                   启用 K8S API 能力
  --k8s-api-server <url>  K8S API Server 地址（默认自动检测）
  --svc                   启用 K8S Service 能力
  -u, --upgrade           升级模式，保留现有配置
  -U, --uninstall         卸载 Endpoint
  -h, --help              显示帮助

示例:
  # 安装（默认启用 SSH）
  curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
    sudo bash -s -- -a 192.168.1.1:50052 -t ep_xxxx -s https://signal.example.com

  # 安装并指定 SSH 端口
  curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
    sudo bash -s -- -a 192.168.1.1:50052 -t ep_xxxx -s https://signal.example.com --ssh-port 2222

  # 安装并启用 K8S API 能力
  curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
    sudo bash -s -- -a 192.168.1.1:50052 -t ep_xxxx -s https://signal.example.com --k8s

  # 升级
  curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
    sudo bash -s -- --upgrade -s https://signal.example.com

  # 卸载
  curl -fsSL https://server/api/v1/download/install_endpoint.sh | \
    sudo bash -s -- --uninstall
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -a|--agent)    AGENT_ADDRESS="$2"; shift 2 ;;
            -t|--token)    ENDPOINT_TOKEN="$2"; shift 2 ;;
            -s|--server)   SERVER_ADDRESS="$2"; shift 2 ;;
            -n|--name)     ENDPOINT_NAME="$2"; shift 2 ;;
            --ssh-host)    SSH_HOST="$2"; shift 2 ;;
            --ssh-port)    SSH_PORT="$2"; shift 2 ;;
            --no-ssh)      SSH_ENABLED="false"; shift ;;
            --k8s)         K8S_ENABLED="true"; shift ;;
            --k8s-api-server) K8S_API_SERVER="$2"; shift 2 ;;
            --svc)         SVC_ENABLED="true"; shift ;;
            -u|--upgrade)  UPGRADE_MODE="true"; shift ;;
            -U|--uninstall) UNINSTALL_MODE="true"; shift ;;
            -h|--help)     show_help; exit 0 ;;
            *)             error "未知参数: $1" ;;
        esac
    done

    if [[ -z "$ENDPOINT_NAME" ]]; then
        ENDPOINT_NAME=$(hostname)
    fi
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "此脚本需要 root 权限运行，请使用 sudo"
    fi
}

detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)   echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)        error "不支持的架构: $arch" ;;
    esac
}

# 下载 Endpoint 二进制
download_endpoint() {
    local arch
    arch=$(detect_arch) || exit 1

    info "检测到架构: ${arch}"
    info "获取最新版本..."

    # 获取版本信息（复用 Agent 的版本接口，Endpoint 和 Agent 同版本）
    local version_url="${SERVER_ADDRESS}/api/v1/download/agent/version"
    local version_response=""
    local version=""

    # 先获取完整响应，再解析，避免管道失败导致脚本退出
    if command -v curl &> /dev/null; then
        version_response=$(curl -fsSL "$version_url" 2>&1) || true
    elif command -v wget &> /dev/null; then
        version_response=$(wget -qO- "$version_url" 2>&1) || true
    fi

    if [[ -n "$version_response" ]]; then
        version=$(echo "$version_response" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || true)
    fi

    if [[ -z "$version" ]]; then
        error "无法获取版本信息（URL: ${version_url}，响应: ${version_response:-空}）"
    fi

    info "最新版本: ${version}"

    # 下载 URL（和 Agent 同一个 S3 目录）
    local download_url="${SERVER_ADDRESS}/api/v1/download/endpoint?os=linux&arch=${arch}&version=${version}"
    local binary_filename="${BINARY_NAME}-${version}-linux-${arch}"
    local binary_path="${DOWNLOAD_DIR}/${binary_filename}"
    local symlink_path="${INSTALL_DIR}/${BINARY_NAME}"

    info "下载 Endpoint..."

    mkdir -p "$DOWNLOAD_DIR" || error "创建目录失败: $DOWNLOAD_DIR"
    mkdir -p "$INSTALL_DIR" || error "创建目录失败: $INSTALL_DIR"

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

    mv "$tmp_file" "$binary_path"
    chmod +x "$binary_path"
    ln -sf "$binary_path" "$symlink_path"

    info "Endpoint ${version} 已安装"
    info "  二进制: ${binary_path}"
    info "  软链接: ${symlink_path}"
}

# 生成配置文件
generate_config() {
    info "生成配置文件..."
    mkdir -p "$CONFIG_DIR"

    cat > "$CONFIG_FILE" << EOF
# AWECloud Endpoint 配置文件（由安装脚本自动生成）

[agent]
address = "${AGENT_ADDRESS}"
token = "${ENDPOINT_TOKEN}"
name = "${ENDPOINT_NAME}"

[ssh]
enabled = ${SSH_ENABLED}
host = "${SSH_HOST}"
port = ${SSH_PORT}

[k8s]
enabled = ${K8S_ENABLED}
api_server = "${K8S_API_SERVER}"

[svc]
enabled = ${SVC_ENABLED}
EOF

    chmod 600 "$CONFIG_FILE"
    info "配置文件: ${CONFIG_FILE}"
}

# 安装 systemd 服务
install_service() {
    info "安装 systemd 服务..."

    cat > "$SERVICE_FILE" << EOF
[Unit]
Description=AWECloud Signaling Endpoint
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=root
Group=root
ExecStart=${INSTALL_DIR}/${BINARY_NAME} -c ${CONFIG_FILE}
Restart=always
RestartSec=10s
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"

    info "服务已启动"
}

# 升级
upgrade_endpoint() {
    if [[ -z "$SERVER_ADDRESS" ]]; then
        # 尝试从现有配置读取（配置里没有 Server 地址，需要用户传入）
        error "升级需要指定 Server 地址 (-s)"
    fi

    info "升级 Endpoint..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    download_endpoint
    systemctl start "$SERVICE_NAME"
    info "升级完成"
}

# 卸载
uninstall_endpoint() {
    info "卸载 Endpoint..."

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true

    rm -f "$SERVICE_FILE"
    rm -f "${INSTALL_DIR}/${BINARY_NAME}"
    rm -rf "${DOWNLOAD_DIR}/${BINARY_NAME}-"*

    systemctl daemon-reload

    info "Endpoint 已卸载"
    info "配置已保留: ${CONFIG_FILE}"
    info "如需完全删除: rm -f ${CONFIG_FILE}"
}

# 验证参数
validate_args() {
    if [[ "$UNINSTALL_MODE" == "true" ]]; then return; fi
    if [[ "$UPGRADE_MODE" == "true" ]]; then
        [[ -z "$SERVER_ADDRESS" ]] && error "升级需要指定 Server 地址 (-s)"
        return
    fi
    [[ -z "$AGENT_ADDRESS" ]] && error "缺少参数: --agent (-a)"
    [[ -z "$ENDPOINT_TOKEN" ]] && error "缺少参数: --token (-t)"
    [[ -z "$SERVER_ADDRESS" ]] && error "缺少参数: --server (-s)"
}

main() {
    echo "========================================"
    echo "  AWECloud Endpoint 安装脚本"
    echo "========================================"
    echo

    parse_args "$@"
    check_root
    validate_args

    if [[ "$UNINSTALL_MODE" == "true" ]]; then
        uninstall_endpoint
        exit 0
    fi

    if [[ "$UPGRADE_MODE" == "true" ]]; then
        upgrade_endpoint
        exit 0
    fi

    # 全新安装
    download_endpoint
    generate_config
    install_service

    echo
    info "安装完成！"
    echo
    echo "Endpoint 名称:  ${ENDPOINT_NAME}"
    echo "Agent 地址:     ${AGENT_ADDRESS}"
    echo "配置文件:       ${CONFIG_FILE}"
    echo
    echo "常用命令:"
    echo "  查看状态: systemctl status ${SERVICE_NAME}"
    echo "  查看日志: journalctl -u ${SERVICE_NAME} -f"
    echo "  重启服务: systemctl restart ${SERVICE_NAME}"
    echo
}

main "$@"
