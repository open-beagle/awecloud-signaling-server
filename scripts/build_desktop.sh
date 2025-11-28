#!/bin/bash

set -e

# AWECloud Signaling Desktop 构建脚本
# 用于构建跨平台桌面应用

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 版本信息
BUILD_VERSION="${BUILD_VERSION:-dev}"
BUILD_ADDRESS="${BUILD_ADDRESS:-}"  # 默认 Server 地址（可选）
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

# 目标平台
PLATFORMS="${PLATFORMS:-windows/amd64}"  # 默认仅构建 Windows amd64
# 可选值：linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

# Desktop 项目目录
DESKTOP_DIR="./desktop"
OUTPUT_DIR="./bin"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}AWECloud Signaling Desktop Builder${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Version:    ${BUILD_VERSION}"
echo "Address:    ${BUILD_ADDRESS:-<not set>}"
echo "Git Commit: ${GIT_COMMIT}"
echo "Build Date: ${BUILD_DATE}"
echo "Platforms:  ${PLATFORMS}"
echo ""

# 检查 Desktop 目录是否存在
if [ ! -d "${DESKTOP_DIR}" ]; then
    echo -e "${RED}Error: Desktop directory not found: ${DESKTOP_DIR}${NC}"
    echo "Please run this script from the project root directory."
    exit 1
fi

# 检查 wails 是否安装
if ! command -v wails &> /dev/null; then
    echo -e "${RED}Error: wails command not found${NC}"
    echo ""
    echo "Please install Wails first:"
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    echo ""
    echo "Or visit: https://wails.io/docs/gettingstarted/installation"
    exit 1
fi

# 检查 Node.js 是否安装
if ! command -v node &> /dev/null; then
    echo -e "${RED}Error: node command not found${NC}"
    echo "Please install Node.js first: https://nodejs.org/"
    exit 1
fi

# 进入 Desktop 目录
cd "${DESKTOP_DIR}"

# 安装前端依赖
echo -e "${YELLOW}Installing frontend dependencies...${NC}"
cd frontend
if [ ! -d "node_modules" ]; then
    npm install
else
    echo "Frontend dependencies already installed, skipping..."
fi
cd ..

# 创建输出目录
mkdir -p "../${OUTPUT_DIR}"

# 解析平台列表
IFS=',' read -ra PLATFORM_ARRAY <<< "$PLATFORMS"

# 构建每个平台
for PLATFORM in "${PLATFORM_ARRAY[@]}"; do
    # 解析平台和架构
    IFS='/' read -r OS ARCH <<< "$PLATFORM"
    
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Building for ${OS}/${ARCH}${NC}"
    echo -e "${GREEN}========================================${NC}"
    
    # 设置输出文件名
    OUTPUT_NAME="awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}"
    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME="${OUTPUT_NAME}.exe"
    fi
    
    # 构建参数
    BUILD_FLAGS="-clean -platform ${OS}/${ARCH}"
    
    # 添加 ldflags
    LDFLAGS="-w -s"
    LDFLAGS="${LDFLAGS} -X 'main.version=${BUILD_VERSION}'"
    LDFLAGS="${LDFLAGS} -X 'main.gitCommit=${GIT_COMMIT}'"
    LDFLAGS="${LDFLAGS} -X 'main.buildDate=${BUILD_DATE}'"
    if [ -n "${BUILD_ADDRESS}" ]; then
        LDFLAGS="${LDFLAGS} -X 'main.defaultServerAddress=${BUILD_ADDRESS}'"
    fi
    BUILD_FLAGS="${BUILD_FLAGS} -ldflags \"${LDFLAGS}\""
    
    # 执行构建
    echo "Building with: wails build ${BUILD_FLAGS}"
    eval "wails build ${BUILD_FLAGS}"
    
    # 检查构建结果
    if [ "$OS" = "darwin" ]; then
        # macOS 构建产物是 .app 包
        BUILD_OUTPUT="build/bin/awecloud-signaling.app"
        if [ -d "${BUILD_OUTPUT}" ]; then
            echo -e "${GREEN}✓ Build successful: ${BUILD_OUTPUT}${NC}"
            # 创建 zip 包
            cd build/bin
            zip -r "../../${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip" "awecloud-signaling.app"
            cd ../..
            echo -e "${GREEN}✓ Created: ${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip${NC}"
        else
            echo -e "${RED}✗ Build failed for ${OS}/${ARCH}${NC}"
            exit 1
        fi
    else
        # Linux/Windows 构建产物是可执行文件
        if [ "$OS" = "windows" ]; then
            BUILD_OUTPUT="build/bin/awecloud-signaling.exe"
        else
            BUILD_OUTPUT="build/bin/awecloud-signaling"
        fi
        
        if [ -f "${BUILD_OUTPUT}" ]; then
            echo -e "${GREEN}✓ Build successful: ${BUILD_OUTPUT}${NC}"
            # 复制到输出目录（去掉 desktop 子目录）
            cp "${BUILD_OUTPUT}" "../${OUTPUT_DIR}/${OUTPUT_NAME}"
            echo -e "${GREEN}✓ Copied to: ${OUTPUT_DIR}/${OUTPUT_NAME}${NC}"
            # 显示文件大小
            FILE_SIZE=$(ls -lh "../${OUTPUT_DIR}/${OUTPUT_NAME}" | awk '{print $5}')
            echo "  File size: ${FILE_SIZE}"
        else
            echo -e "${RED}✗ Build failed for ${OS}/${ARCH}${NC}"
            exit 1
        fi
    fi
done

# 返回项目根目录
cd ..

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}All builds completed successfully!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Output directory: ${OUTPUT_DIR}/"
ls -lh "${OUTPUT_DIR}/"

echo ""
echo -e "${YELLOW}Usage examples:${NC}"
echo ""
echo "  # Build for current platform"
echo "  bash scripts/build_desktop.sh"
echo ""
echo "  # Build for specific platform"
echo "  PLATFORMS=linux/amd64 bash scripts/build_desktop.sh"
echo ""
echo "  # Build for multiple platforms"
echo "  PLATFORMS=linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64 bash scripts/build_desktop.sh"
echo ""
echo "  # Build with version"
echo "  BUILD_VERSION=v0.1.0 bash scripts/build_desktop.sh"
echo ""
echo "  # Build with default server address"
echo "  BUILD_VERSION=v0.1.0 BUILD_ADDRESS=https://signaling.example.com bash scripts/build_desktop.sh"
echo ""
