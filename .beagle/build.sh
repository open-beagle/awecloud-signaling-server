#!/bin/bash

set -e

# 参数配置
GOARCHS="${GOARCHS:-amd64,arm64}"
GOOS="${GOOS:-linux}"
BUILD_TARGETS="${BUILD_TARGETS:-server,agent}"  # 可选: server, agent, 或 server,agent

# 版本信息
BUILD_VERSION="${BUILD_VERSION:-dev}"
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

# 输出目录
BIN_DIR="./bin"
mkdir -p ${BIN_DIR}

# 分割架构列表
IFS=',' read -ra ARCH_ARRAY <<< "$GOARCHS"

echo "Building AWECloud Signaling (CI/CD)"
echo "Target OS: ${GOOS}"
echo "Target architectures: ${GOARCHS}"
echo "Build targets: ${BUILD_TARGETS}"
echo "Version: ${BUILD_VERSION}"
echo "Git Commit: ${GIT_COMMIT}"
echo "Build Date: ${BUILD_DATE}"
echo "---"

# 检查是否需要构建 server 和 agent
BUILD_SERVER=false
BUILD_AGENT=false
if [[ "${BUILD_TARGETS}" == *"server"* ]]; then
    BUILD_SERVER=true
fi
if [[ "${BUILD_TARGETS}" == *"agent"* ]]; then
    BUILD_AGENT=true
fi

# 构建 ldflags，注入版本信息
LDFLAGS="-w -s"
LDFLAGS="${LDFLAGS} -X 'main.version=${BUILD_VERSION}'"
LDFLAGS="${LDFLAGS} -X 'main.gitCommit=${GIT_COMMIT}'"
LDFLAGS="${LDFLAGS} -X 'main.buildDate=${BUILD_DATE}'"

# 安装跨架构编译依赖（需要 root 权限）
if [ "$(id -u)" -eq 0 ]; then
    echo "Installing cross-compilation dependencies..."
    
    # 检查是否需要安装 QEMU（用于交叉编译时运行测试）
    NEED_QEMU=false
    for ARCH in "${ARCH_ARRAY[@]}"; do
        if [ "${ARCH}" != "$(go env GOARCH)" ]; then
            NEED_QEMU=true
            break
        fi
    done
    
    if [ "$NEED_QEMU" = true ]; then
        echo "Installing QEMU for cross-compilation..."
        apk add --no-cache qemu-aarch64
    fi
    
    for ARCH in "${ARCH_ARRAY[@]}"; do
        echo "Installing dependencies for ${ARCH}..."
        export TARGETARCH=${ARCH}
        xx-apk add --no-cache gcc musl-dev sqlite-dev
        
        # 为交叉编译创建必要的符号链接（避免 QEMU 错误）
        if [ "${ARCH}" != "$(go env GOARCH)" ]; then
            XX_CC_TRIPLE=$(xx-info triple)
            XX_MARCH=$(xx-info march)
            if [ -f "/${XX_CC_TRIPLE}/lib/ld-musl-${XX_MARCH}.so.1" ] && [ ! -f "/lib/ld-musl-${XX_MARCH}.so.1" ]; then
                ln -sf "/${XX_CC_TRIPLE}/lib/ld-musl-${XX_MARCH}.so.1" "/lib/ld-musl-${XX_MARCH}.so.1"
                echo "Created symlink: /lib/ld-musl-${XX_MARCH}.so.1"
            fi
        fi
    done
    echo "---"
else
    echo "Running as non-root user, skipping dependency installation"
    echo "Make sure cross-compilation tools are already installed"
    echo "---"
fi

# 遍历每个架构进行编译
for ARCH in "${ARCH_ARRAY[@]}"; do
    # 构建 Server（如果需要）
    if [ "$BUILD_SERVER" = true ]; then
        echo "Building Server for ${GOOS}/${ARCH}..."
        
        OUTPUT="${BIN_DIR}/server-${GOOS}-${ARCH}"
        
        # 设置目标架构
        export TARGETARCH=${ARCH}
        
        # 获取交叉编译器信息
        XX_CC_TRIPLE=$(xx-info triple)
        
        # 设置正确的交叉编译器路径和环境变量
        if [ "${ARCH}" != "$(go env GOARCH)" ]; then
            export CC="/${XX_CC_TRIPLE}/usr/bin/gcc"
            export CXX="/${XX_CC_TRIPLE}/usr/bin/g++"
            export AR="/${XX_CC_TRIPLE}/usr/bin/ar"
            export PKG_CONFIG_PATH="/${XX_CC_TRIPLE}/usr/lib/pkgconfig"
            export CGO_CFLAGS="-I/${XX_CC_TRIPLE}/usr/include"
            export CGO_LDFLAGS="-L/${XX_CC_TRIPLE}/usr/lib"
            # 设置 QEMU 的库搜索路径
            export QEMU_LD_PREFIX="/${XX_CC_TRIPLE}"
            echo "Using cross-compiler: ${CC}"
            echo "QEMU_LD_PREFIX: ${QEMU_LD_PREFIX}"
        fi
        
        # 使用标准 go build 进行跨架构编译（支持 CGO）
        CGO_ENABLED=1 \
        GOOS=${GOOS} \
        GOARCH=${ARCH} \
        go build \
            -trimpath \
            -buildvcs=false \
            -ldflags="${LDFLAGS}" \
            -o ${OUTPUT} \
            ./cmd/server
        
        # 验证二进制文件架构
        xx-verify ${OUTPUT}
        
        # 检查编译是否成功
        if [ -f "${OUTPUT}" ]; then
            echo "✓ Successfully built: ${OUTPUT}"
            ls -lh ${OUTPUT}
            file ${OUTPUT}
        else
            echo "✗ Failed to build: ${OUTPUT}"
            exit 1
        fi
        
        echo "---"
    fi
    
    # 构建 Agent（如果需要）
    if [ "$BUILD_AGENT" = true ]; then
        echo "Building Agent for ${GOOS}/${ARCH}..."
        
        OUTPUT="${BIN_DIR}/agent-${GOOS}-${ARCH}"
        
        # Agent 不需要 CGO，使用标准 Go 交叉编译
        CGO_ENABLED=0 \
        GOOS=${GOOS} \
        GOARCH=${ARCH} \
        go build \
            -buildvcs=false \
            -ldflags="${LDFLAGS}" \
            -o ${OUTPUT} \
            ./cmd/agent
        
        # 检查编译是否成功
        if [ -f "${OUTPUT}" ]; then
            echo "✓ Successfully built: ${OUTPUT}"
            ls -lh ${OUTPUT}
            file ${OUTPUT}
        else
            echo "✗ Failed to build: ${OUTPUT}"
            exit 1
        fi
        
        echo "---"
    fi
done

echo ""
echo "All builds completed successfully!"
echo "Binaries are in: ${BIN_DIR}/"
ls -lh ${BIN_DIR}/
