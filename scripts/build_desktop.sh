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
BUILD_NUMBER=$(git rev-list --count HEAD 2>/dev/null || echo "0")
BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

# 目标平台
PLATFORMS="${PLATFORMS:-windows/amd64}"  # 默认仅构建 Windows amd64
# 可选值：linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

# Desktop 项目目录
DESKTOP_DIR="./desktop"
OUTPUT_DIR="./bin"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Building AWECloud Signaling Desktop${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "Version:      ${YELLOW}${BUILD_VERSION}${NC}"
echo -e "Git Commit:   ${YELLOW}${GIT_COMMIT}${NC}"
echo -e "Build Number: ${YELLOW}${BUILD_NUMBER}${NC}"
echo -e "Build Date:   ${YELLOW}${BUILD_DATE}${NC}"
[ -n "${BUILD_ADDRESS}" ] && echo -e "Server Addr:  ${YELLOW}${BUILD_ADDRESS}${NC}"
echo -e "Platforms:    ${YELLOW}${PLATFORMS}${NC}"
echo -e "${GREEN}========================================${NC}"
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

# installLinuxDeps 安装Linux构建依赖
installLinuxDeps() {
    echo -e "${YELLOW}Installing Linux build dependencies...${NC}"
    
    # 检查 pkg-config
    if ! command -v pkg-config &> /dev/null; then
        echo -e "${RED}Error: pkg-config not found${NC}"
        echo "Please install: sudo apt-get install pkg-config"
        exit 1
    fi
    
    # 检查并安装 GTK3
    if ! pkg-config --exists gtk+-3.0; then
        echo "Installing GTK3 development libraries..."
        sudo apt-get update
        sudo apt-get install -y libgtk-3-dev
    fi
    
    # 检查并安装 WebKit2GTK
    if ! pkg-config --exists webkit2gtk-4.1 && ! pkg-config --exists webkit2gtk-4.0; then
        echo "Installing WebKit2GTK development libraries..."
        sudo apt-get install -y libwebkit2gtk-4.1-dev || sudo apt-get install -y libwebkit2gtk-4.0-dev
    fi
    
    # 如果系统有 webkit2gtk-4.1 但 Wails 需要 webkit2gtk-4.0，创建软链接
    if pkg-config --exists webkit2gtk-4.1 && ! pkg-config --exists webkit2gtk-4.0; then
        echo -e "${YELLOW}Creating webkit2gtk-4.0.pc symlink for compatibility...${NC}"
        # 查找 webkit2gtk-4.1.pc 的实际位置
        WEBKIT_PC=$(pkg-config --variable=pcfiledir webkit2gtk-4.1 2>/dev/null)
        if [ -z "$WEBKIT_PC" ]; then
            # 尝试常见位置
            for dir in /usr/lib/x86_64-linux-gnu/pkgconfig /usr/lib/pkgconfig /usr/local/lib/pkgconfig; do
                if [ -f "$dir/webkit2gtk-4.1.pc" ]; then
                    WEBKIT_PC="$dir"
                    break
                fi
            done
        fi
        
        if [ -n "$WEBKIT_PC" ] && [ -f "$WEBKIT_PC/webkit2gtk-4.1.pc" ]; then
            sudo ln -sf "$WEBKIT_PC/webkit2gtk-4.1.pc" "$WEBKIT_PC/webkit2gtk-4.0.pc"
            echo -e "${GREEN}✓ Created symlink: $WEBKIT_PC/webkit2gtk-4.0.pc${NC}"
        else
            echo -e "${YELLOW}Warning: Could not create webkit2gtk-4.0.pc symlink${NC}"
        fi
    fi
    
    echo -e "${GREEN}✓ Linux build dependencies installed${NC}"
}

# installWindowsDeps 安装Windows交叉编译依赖
installWindowsDeps() {
    echo -e "${YELLOW}Installing Windows cross-compilation dependencies...${NC}"
    
    # 检查并安装 MinGW-w64
    if ! command -v x86_64-w64-mingw32-gcc &> /dev/null; then
        echo "Installing MinGW-w64 compiler..."
        sudo apt-get update
        sudo apt-get install -y gcc-mingw-w64-x86-64
    fi
    
    # 检查并安装 NSIS (用于创建安装程序)
    if ! command -v makensis &> /dev/null; then
        echo "Installing NSIS..."
        sudo apt-get install -y nsis
    fi
    
    echo -e "${GREEN}✓ Windows cross-compilation dependencies installed${NC}"
}

# installMacOSDeps 安装macOS构建依赖
installMacOSDeps() {
    # 检查是否在macOS上
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo -e "${YELLOW}Installing macOS build dependencies...${NC}"
        
        # 检查 Xcode Command Line Tools
        if ! xcode-select -p &> /dev/null; then
            echo "Installing Xcode Command Line Tools..."
            xcode-select --install
            echo "Please complete the Xcode Command Line Tools installation and run this script again."
            exit 1
        fi
        
        echo -e "${GREEN}✓ macOS build dependencies OK${NC}"
    else
        # 在Linux上交叉编译macOS
        echo -e "${YELLOW}Warning: Cross-compiling macOS from Linux requires osxcross${NC}"
        echo -e "${YELLOW}This is complex and not recommended. Consider:${NC}"
        echo -e "${YELLOW}  1. Building on macOS directly${NC}"
        echo -e "${YELLOW}  2. Using GitHub Actions with macos-latest runner${NC}"
        echo ""
        
        # 检查是否安装了osxcross
        if command -v x86_64-apple-darwin20.4-clang &> /dev/null; then
            echo -e "${GREEN}✓ osxcross detected, will attempt cross-compilation${NC}"
        else
            echo -e "${RED}Error: osxcross not found${NC}"
            echo "Skipping macOS build. To set up osxcross, visit:"
            echo "  https://github.com/tpoechtrager/osxcross"
            return 1
        fi
    fi
}

# 进入 Desktop 目录（使用 desktop 的 go.mod）
cd "${DESKTOP_DIR}"

# 安装前端依赖
cd frontend
if [ ! -d "node_modules" ]; then
    echo "Installing frontend dependencies..."
    npm install > /dev/null 2>&1
fi
cd ..

# 创建输出目录（相对于项目根目录）
mkdir -p "../${OUTPUT_DIR}"

# 解析平台列表
IFS=',' read -ra PLATFORM_ARRAY <<< "$PLATFORMS"

# 构建每个平台
for PLATFORM in "${PLATFORM_ARRAY[@]}"; do
    # 解析平台和架构
    IFS='/' read -r OS ARCH <<< "$PLATFORM"
    
    echo ""
    echo -e "${GREEN}Building ${OS}/${ARCH}...${NC}"
    
    # 根据平台检查依赖（静默）
    case "$OS" in
        linux)
            installLinuxDeps > /dev/null 2>&1 || true
            ;;
        windows)
            installWindowsDeps > /dev/null 2>&1 || true
            ;;
        darwin)
            if ! installMacOSDeps > /dev/null 2>&1; then
                echo -e "${YELLOW}Skipping macOS build${NC}"
                continue
            fi
            ;;
    esac
    
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
    LDFLAGS="${LDFLAGS} -X 'main.buildNumber=${BUILD_NUMBER}'"
    if [ -n "${BUILD_ADDRESS}" ]; then
        # 使用 desktop/go.mod 中的 module 路径
        LDFLAGS="${LDFLAGS} -X 'github.com/open-beagle/awecloud-signaling-desktop/internal/config.buildAddress=${BUILD_ADDRESS}'"
    fi
    BUILD_FLAGS="${BUILD_FLAGS} -ldflags \"${LDFLAGS}\""
    
    # 执行构建（隐藏详细输出）
    eval "wails build ${BUILD_FLAGS}" > /tmp/wails_build.log 2>&1 || {
        echo -e "${RED}Build failed, showing log:${NC}"
        cat /tmp/wails_build.log
        exit 1
    }
    
    # 检查构建结果
    if [ "$OS" = "darwin" ]; then
        # macOS 构建产物是 .app 包

        BUILD_OUTPUT="build/bin/awecloud-signaling-desktop.app"
        if [ -d "${BUILD_OUTPUT}" ]; then
            # 创建 zip 包
            cd build/bin
            zip -r "../../${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip" "awecloud-signaling-desktop.app" > /dev/null 2>&1
            cd ../..
            FILE_SIZE=$(du -h "${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip" | cut -f1)
            echo -e "${GREEN}✓ ${OUTPUT_DIR}/awecloud-signaling-${BUILD_VERSION}-${OS}-${ARCH}.zip (${FILE_SIZE})${NC}"
        else
            echo -e "${RED}✗ Build failed for ${OS}/${ARCH}${NC}"
            echo -e "${RED}Expected output: ${BUILD_OUTPUT}${NC}"
            echo -e "${YELLOW}Checking build/bin directory:${NC}"
            ls -la build/bin/ || echo "build/bin directory not found"
            exit 1
        fi
    else
        # Linux/Windows 构建产物是可执行文件
        if [ "$OS" = "windows" ]; then
            BUILD_OUTPUT="build/bin/awecloud-signaling-desktop.exe"
        else
            BUILD_OUTPUT="build/bin/awecloud-signaling-desktop"
        fi
        
        if [ -f "${BUILD_OUTPUT}" ]; then
            # 复制到输出目录
            cp "${BUILD_OUTPUT}" "../${OUTPUT_DIR}/${OUTPUT_NAME}"
            FILE_SIZE=$(ls -lh "../${OUTPUT_DIR}/${OUTPUT_NAME}" | awk '{print $5}')
            echo -e "${GREEN}✓ ${OUTPUT_DIR}/${OUTPUT_NAME} (${FILE_SIZE})${NC}"
        else
            echo -e "${RED}✗ Build failed${NC}"
            cat /tmp/wails_build.log
            exit 1
        fi
    fi
done

# 返回项目根目录
cd ..

echo -e "${GREEN}Build complete!${NC}"
