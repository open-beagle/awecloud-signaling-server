#!/bin/bash
# 发布 Agent 到 S3
# 用法: BUILD_VERSION=v0.2.3 bash scripts/publish_agent_s3.sh

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

# 检查版本号
if [ -z "$BUILD_VERSION" ]; then
    error "请设置 BUILD_VERSION 环境变量，如: BUILD_VERSION=v1.x.y bash scripts/publish_agent_s3.sh"
fi

# 检查 mc 命令
if ! command -v mc &> /dev/null; then
    error "mc 命令未找到，请先安装 MinIO Client"
fi

# 检查 S3 别名是否配置
if ! mc alias list aliyun &> /dev/null; then
    error "S3 别名 'aliyun' 未配置，请先运行: mc alias set aliyun --api=S3v4 https://s3.example.com <ACCESS_KEY> <SECRET_KEY>"
fi

info "发布 Agent 到 S3"
info "版本: ${BUILD_VERSION}"
info "目标: ${S3_BUCKET}"
echo "---"

# 上传 Agent 二进制
ARCHS="amd64 arm64"
for arch in $ARCHS; do
    local_file="${BIN_DIR}/agent-linux-${arch}"
    remote_file="${S3_BUCKET}/agent-${BUILD_VERSION}-linux-${arch}"
    
    if [ -f "$local_file" ]; then
        info "上传 agent-${BUILD_VERSION}-linux-${arch}..."
        mc cp "$local_file" "$remote_file"
        echo "  ✓ $remote_file"
    else
        warn "文件不存在: $local_file，跳过"
    fi
done

# 上传安装脚本
INSTALL_SCRIPT="./scripts/install_agent.sh"
if [ -f "$INSTALL_SCRIPT" ]; then
    info "上传 install.sh..."
    mc cp "$INSTALL_SCRIPT" "${S3_BUCKET}/install.sh"
    echo "  ✓ ${S3_BUCKET}/install.sh"
else
    warn "安装脚本不存在: $INSTALL_SCRIPT"
fi

# 创建并上传版本信息
info "更新版本信息..."
VERSION_FILE=$(mktemp)
cat > "$VERSION_FILE" << EOF
{
    "version": "${BUILD_VERSION}",
    "build_date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "files": {
        "linux-amd64": "agent-${BUILD_VERSION}-linux-amd64",
        "linux-arm64": "agent-${BUILD_VERSION}-linux-arm64"
    }
}
EOF
mc cp "$VERSION_FILE" "${S3_BUCKET}/agent-version.json"
rm -f "$VERSION_FILE"
echo "  ✓ ${S3_BUCKET}/agent-version.json"

# 设置公开访问权限
info "设置公开访问权限..."
mc anonymous set download "${S3_BUCKET}/install.sh" > /dev/null 2>&1
mc anonymous set download "${S3_BUCKET}/agent-version.json" > /dev/null 2>&1
for arch in $ARCHS; do
    mc anonymous set download "${S3_BUCKET}/agent-${BUILD_VERSION}-linux-${arch}" > /dev/null 2>&1
done
echo "  ✓ 权限设置完成"

echo "---"
info "发布完成！"
