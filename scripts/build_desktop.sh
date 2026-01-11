#!/bin/bash

set -e

# AWECloud Signaling Desktop 构建脚本（项目根目录）
# 此脚本调用 desktop/scripts/build.sh 进行实际构建，然后复制结果到 bin 目录

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 版本信息（日常开发使用默认值，流水线传递完整参数）
BUILD_VERSION="${BUILD_VERSION:-v0.2.0}"
BUILD_ADDRESS="${BUILD_ADDRESS:-${SIGNALING_ADDRESS}}"
PLATFORMS="${PLATFORMS:-$(go env GOOS)/$(go env GOARCH)}"

# 目录配置
DESKTOP_DIR="./desktop"
OUTPUT_DIR="./bin"

# 检查 Desktop 目录是否存在
if [ ! -d "${DESKTOP_DIR}" ]; then
    echo -e "${RED}Error: Desktop directory not found: ${DESKTOP_DIR}${NC}"
    echo "Please run this script from the project root directory."
    exit 1
fi

# 检查 desktop/scripts/build.sh 是否存在
if [ ! -f "${DESKTOP_DIR}/scripts/build.sh" ]; then
    echo -e "${RED}Error: Build script not found: ${DESKTOP_DIR}/scripts/build.sh${NC}"
    exit 1
fi

# 创建输出目录
mkdir -p "${OUTPUT_DIR}"

# 进入 Desktop 目录
cd "${DESKTOP_DIR}"

# 调用 desktop/scripts/build.sh 进行构建
echo "Calling desktop/scripts/build.sh..."
echo ""
BUILD_VERSION="${BUILD_VERSION}" \
BUILD_ADDRESS="${BUILD_ADDRESS}" \
PLATFORMS="${PLATFORMS}" \
bash scripts/build.sh

# 返回项目根目录
cd ..

# 复制构建结果到 bin 目录
echo ""
echo -e "${GREEN}Copying build artifacts to ${OUTPUT_DIR}/...${NC}"

# 解析平台列表
IFS=',' read -ra PLATFORM_ARRAY <<< "$PLATFORMS"

for PLATFORM in "${PLATFORM_ARRAY[@]}"; do
    # 解析平台和架构
    IFS='/' read -r OS ARCH <<< "$PLATFORM"
    
    if [ "$OS" = "darwin" ]; then
        # macOS 构建产物是 .app 包（已打包为 zip）
        SOURCE_FILE="${DESKTOP_DIR}/build/bin/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip"
        DEST_FILE="${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip"
        
        if [ -f "${SOURCE_FILE}" ]; then
            cp "${SOURCE_FILE}" "${DEST_FILE}"
            FILE_SIZE=$(ls -lh "${DEST_FILE}" | awk '{print $5}')
            echo -e "${GREEN}✓ ${DEST_FILE} (${FILE_SIZE})${NC}"
        else
            echo -e "${YELLOW}⚠ ${SOURCE_FILE} not found, skipping${NC}"
        fi
    else
        # Linux/Windows 构建产物是可执行文件
        if [ "$OS" = "windows" ]; then
            SOURCE_FILE="${DESKTOP_DIR}/build/bin/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.exe"
            DEST_FILE="${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.exe"
        else
            SOURCE_FILE="${DESKTOP_DIR}/build/bin/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}"
            DEST_FILE="${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}"
        fi
        
        if [ -f "${SOURCE_FILE}" ]; then
            cp "${SOURCE_FILE}" "${DEST_FILE}"
            FILE_SIZE=$(ls -lh "${DEST_FILE}" | awk '{print $5}')
            echo -e "${GREEN}✓ ${DEST_FILE} (${FILE_SIZE})${NC}"
        else
            echo -e "${YELLOW}⚠ ${SOURCE_FILE} not found, skipping${NC}"
        fi
    fi
done

echo ""
echo -e "${GREEN}Build complete!${NC}"
