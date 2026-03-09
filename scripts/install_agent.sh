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
UPGRADE_MODE="false"
UNINSTALL_MODE="false"
DEPLOY_MODE="false"  # 部署模式：使用 Token 自动注册

# 安装路径
DOWNLOAD_DIR="/etc/kubernetes/downloads"
INSTALL_DIR="/opt/bin"
BINARY_NAME="signal_agent"
CONFIG_DIR="/etc/kubernetes/config"
DATA_DIR="/etc/kubernetes/data/signaling"
CONFIG_FILE="${CONFIG_DIR}/k8s-signaling.toml"
SERVICE_NAME="k8s-signaling"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

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

# 下载 Agent 二进制
download_agent() {
    local arch=$(detect_arch)
    
    info "检测到架构: ${arch}"
    info "获取最新版本..."
    
    # 获取版本信息
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
        error "无法获取 Agent 版本信息（URL: ${version_url}，响应: ${version_response:-空}）"
    fi
    
    info "最新版本: ${version}"
    
    # 构建下载 URL
    local download_url="${SERVER_ADDRESS}/api/v1/download/agent?os=linux&arch=${arch}&version=${version}"
    
    # 二进制文件名（带版本和架构）
    local binary_filename="${BINARY_NAME}-${version}-linux-${arch}"
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
state_dir = "${DATA_DIR}/tunnel"
state_sync_interval = 5
enable_ssh = ${ENABLE_SSH}

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

    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"
    
    info "服务已启动"
}

# 升级 Agent
upgrade_agent() {
    if [[ -z "$SERVER_ADDRESS" ]]; then
        # 从现有配置读取 Server 地址（兼容新旧配置文件名）
        for cfg in "$CONFIG_FILE" "${CONFIG_DIR}/k8s-signaling.toml"; do
            if [[ -f "$cfg" ]]; then
                SERVER_ADDRESS=$(grep -E '^address\s*=' "$cfg" | head -1 | sed 's/.*=\s*"\(.*\)"/\1/')
                [[ -n "$SERVER_ADDRESS" ]] && break
            fi
        done
    fi
    
    if [[ -z "$SERVER_ADDRESS" ]]; then
        error "升级需要指定 Server 地址 (-s)"
    fi
    
    info "升级 Agent..."
    
    # 停止服务
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    
    # 下载新版本（会自动更新软链接）
    download_agent
    
    # 安装新的 systemd 服务（覆盖旧的）
    install_service
    
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
    
    # 解析响应（Server 返回格式: {"success":true,"data":{"user_role":"agent","config":{"agent":{"name":"..."}}}}）
    local success=$(echo "$response" | grep -o '"success":\s*true' || true)
    if [[ -z "$success" ]]; then
        local message=$(echo "$response" | grep -o '"message":"[^"]*"' | cut -d'"' -f4)
        error "注册失败: ${message:-$response}"
    fi
    
    # 从 data.config.agent.name 提取 Agent 名称
    AGENT_NAME=$(echo "$response" | grep -o '"name":"[^"]*"' | head -1 | cut -d'"' -f4)
    
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
    
    # 全新安装
    download_agent
    create_directories
    generate_config
    install_service
    
    echo
    info "安装完成！"
    echo
    echo "Agent 名称: ${AGENT_NAME}"
    echo "设备名称:   ${DEVICE_NAME}"
    echo "SSH 功能:   ${ENABLE_SSH}"
    echo
    echo "常用命令:"
    echo "  查看状态: systemctl status ${SERVICE_NAME}"
    echo "  查看日志: journalctl -u ${SERVICE_NAME} -f"
    echo "  重启服务: systemctl restart ${SERVICE_NAME}"
    echo
}

main "$@"
