#!/bin/bash
# 发布 Agent/Endpoint 到 S3
# 用法:
#   BUILD_VERSION=v0.2.3 bash scripts/push_to_s3.sh          # 全部上传
#   BUILD_VERSION=v0.2.3 bash scripts/push_to_s3.sh agent    # 只上传 Agent
#   BUILD_VERSION=v0.2.3 bash scripts/push_to_s3.sh endpoint # 只上传 Endpoint

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 配置
BUILD_VERSION="${BUILD_VERSION:-}"
BIN_DIR="./bin"
S3_BUCKET="aliyun/vscode/awecloud-signaling"
TARGET="${1:-all}"  # agent / endpoint / all（默认）
ARCHS="amd64 arm64"

# 检查版本号
if [ -z "$BUILD_VERSION" ]; then
    error "请设置 BUILD_VERSION 环境变量，如: BUILD_VERSION=v1.x.y bash scripts/push_to_s3.sh"
fi

# 检查 mc 命令
if ! command -v mc &> /dev/null; then
    error "mc 命令未找到，请先安装 MinIO Client"
fi

# 检查 S3 别名是否配置
if ! mc alias list aliyun &> /dev/null; then
    error "S3 别名 'aliyun' 未配置，请先运行: mc alias set aliyun --api=S3v4 https://s3.example.com <ACCESS_KEY> <SECRET_KEY>"
fi

# 验证参数
if [ "$TARGET" != "all" ] && [ "$TARGET" != "agent" ] && [ "$TARGET" != "endpoint" ]; then
    error "无效参数: $TARGET，可选值: agent / endpoint（不传则全部上传）"
fi

info "发布到 S3"
info "版本: ${BUILD_VERSION}"
info "目标: ${TARGET}"
info "S3: ${S3_BUCKET}"
echo "---"

# 上传二进制文件的通用函数
upload_binary() {
    local name="$1"
    for arch in $ARCHS; do
        local_file="${BIN_DIR}/${name}-linux-${arch}"
        remote_file="${S3_BUCKET}/${name}-${BUILD_VERSION}-linux-${arch}"
        
        if [ -f "$local_file" ]; then
            info "上传 ${name}-${BUILD_VERSION}-linux-${arch}..."
            mc cp "$local_file" "$remote_file"
            echo "  ✓ $remote_file"
        else
            warn "文件不存在: $local_file，跳过"
        fi
    done
}

# 上传安装脚本的通用函数
upload_install_script() {
    local script_path="$1"
    local script_name
    script_name=$(basename "$script_path")
    if [ -f "$script_path" ]; then
        info "上传 ${script_name}..."
        mc cp "$script_path" "${S3_BUCKET}/${script_name}"
        echo "  ✓ ${S3_BUCKET}/${script_name}"
    else
        warn "安装脚本不存在: $script_path"
    fi
}

# ========== Agent ==========
if [ "$TARGET" = "all" ] || [ "$TARGET" = "agent" ]; then
    info "--- Agent ---"
    upload_binary "signal_agent"
    upload_install_script "./scripts/install_agent.sh"

    # 更新 Agent 版本信息
    info "更新 Agent 版本信息..."
    VERSION_FILE=$(mktemp)
    cat > "$VERSION_FILE" << EOF
{
    "version": "${BUILD_VERSION}",
    "build_date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "files": {
        "linux-amd64": "signal_agent-${BUILD_VERSION}-linux-amd64",
        "linux-arm64": "signal_agent-${BUILD_VERSION}-linux-arm64"
    }
}
EOF
    mc cp "$VERSION_FILE" "${S3_BUCKET}/signal_agent-version.json"
    rm -f "$VERSION_FILE"
    echo "  ✓ ${S3_BUCKET}/signal_agent-version.json"
fi

# ========== Endpoint ==========
if [ "$TARGET" = "all" ] || [ "$TARGET" = "endpoint" ]; then
    info "--- Endpoint ---"
    upload_binary "signal_endpoint"
    upload_install_script "./scripts/install_endpoint.sh"

    # 更新 Endpoint 版本信息
    info "更新 Endpoint 版本信息..."
    VERSION_FILE=$(mktemp)
    cat > "$VERSION_FILE" << EOF
{
    "version": "${BUILD_VERSION}",
    "build_date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "files": {
        "linux-amd64": "signal_endpoint-${BUILD_VERSION}-linux-amd64",
        "linux-arm64": "signal_endpoint-${BUILD_VERSION}-linux-arm64"
    }
}
EOF
    mc cp "$VERSION_FILE" "${S3_BUCKET}/signal_endpoint-version.json"
    rm -f "$VERSION_FILE"
    echo "  ✓ ${S3_BUCKET}/signal_endpoint-version.json"
fi

# 设置公开访问权限
info "设置公开访问权限..."
if [ "$TARGET" = "all" ] || [ "$TARGET" = "agent" ]; then
    mc anonymous set download "${S3_BUCKET}/install_agent.sh" > /dev/null 2>&1
    mc anonymous set download "${S3_BUCKET}/signal_agent-version.json" > /dev/null 2>&1
    for arch in $ARCHS; do
        mc anonymous set download "${S3_BUCKET}/signal_agent-${BUILD_VERSION}-linux-${arch}" > /dev/null 2>&1
    done
fi
if [ "$TARGET" = "all" ] || [ "$TARGET" = "endpoint" ]; then
    mc anonymous set download "${S3_BUCKET}/install_endpoint.sh" > /dev/null 2>&1
    mc anonymous set download "${S3_BUCKET}/signal_endpoint-version.json" > /dev/null 2>&1
    for arch in $ARCHS; do
        mc anonymous set download "${S3_BUCKET}/signal_endpoint-${BUILD_VERSION}-linux-${arch}" > /dev/null 2>&1
    done
fi
echo "  ✓ 权限设置完成"

echo "---"
info "发布完成！"
