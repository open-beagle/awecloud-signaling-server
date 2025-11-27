#!/bin/bash

set -e

# 参数配置
GOARCHS="${GOARCHS:-amd64,arm64}"
GOOS="${GOOS:-linux}"

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
echo "Version: ${BUILD_VERSION}"
echo "Git Commit: ${GIT_COMMIT}"
echo "Build Date: ${BUILD_DATE}"
echo "---"

# 构建 ldflags，注入版本信息
LDFLAGS="-w -s"
LDFLAGS="${LDFLAGS} -X 'main.version=${BUILD_VERSION}'"
LDFLAGS="${LDFLAGS} -X 'main.gitCommit=${GIT_COMMIT}'"
LDFLAGS="${LDFLAGS} -X 'main.buildDate=${BUILD_DATE}'"

# 安装跨架构编译依赖
echo "Installing cross-compilation dependencies..."
for ARCH in "${ARCH_ARRAY[@]}"; do
    if [ "$ARCH" != "$(go env GOARCH)" ]; then
        echo "Installing dependencies for ${ARCH}..."
        TARGETARCH=${ARCH} sudo -E xx-apk add --no-cache gcc musl-dev sqlite-dev
    fi
done
echo "---"

# 遍历每个架构进行编译
for ARCH in "${ARCH_ARRAY[@]}"; do
    echo "Building Server for ${GOOS}/${ARCH}..."
    
    OUTPUT="${BIN_DIR}/server-${GOOS}-${ARCH}"
    
    # 设置目标架构
    export TARGETARCH=${ARCH}
    
    # 使用 xx-go 进行跨架构编译（支持 CGO）
    CGO_ENABLED=1 \
    xx-go build -a -installsuffix cgo \
        -buildvcs=false \
        -ldflags="${LDFLAGS}" \
        -o ${OUTPUT} \
        ./cmd/server
    
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
    
    echo "Building Agent for ${GOOS}/${ARCH}..."
    
    OUTPUT="${BIN_DIR}/agent-${GOOS}-${ARCH}"
    
    # Agent 不需要 CGO，使用标准 Go 交叉编译
    CGO_ENABLED=0 \
    GOOS=${GOOS} \
    GOARCH=${ARCH} \
    go build -a -installsuffix cgo \
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
done

echo ""
echo "All builds completed successfully!"
echo "Binaries are in: ${BIN_DIR}/"
ls -lh ${BIN_DIR}/
