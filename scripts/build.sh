#!/bin/bash
set -e

# 构建目标：all（默认）、server、agent、endpoint
# 支持逗号分隔的多目标：BUILD_TARGETS=server,agent,endpoint
BUILD_TARGET="${BUILD_TARGETS:-${1:-all}}"

# 参数配置
# 日常开发默认只构建当前架构，流水线传递完整参数
GOARCHS="${GOARCHS:-$(go env GOARCH)}"
GOOS="${GOOS:-linux}"

# 版本信息
BUILD_VERSION="${BUILD_VERSION:-dev}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || true)}"
if [[ ! "$GIT_COMMIT" =~ ^[0-9a-f]{40}$ ]]; then
    echo "GIT_COMMIT must be a full 40-character lowercase Git SHA" >&2
    exit 1
fi
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$GIT_COMMIT")}"
if [[ ! "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]]; then
    echo "SOURCE_DATE_EPOCH must be a Unix timestamp" >&2
    exit 1
fi
export SOURCE_DATE_EPOCH
BUILD_DATE=$(date -u -d "@${SOURCE_DATE_EPOCH}" '+%Y-%m-%dT%H:%M:%SZ')
BUILD_GO=$(go version | awk '{print $3}')

# Server 地址（可选，用于编译时注入）
BUILD_URL="${BUILD_URL:-}"
BUILD_DIR="$PWD"

if [ "${RELEASE_ARTIFACTS:-false}" = "true" ]; then
    : "${ARTIFACT_BASE_URL:?ARTIFACT_BASE_URL is required}"
fi

# 输出目录
BIN_DIR="${BUILD_DIR}/bin"
mkdir -p ${BIN_DIR}

# 分割架构列表
IFS=',' read -ra ARCH_ARRAY <<< "$GOARCHS"

# 构建 ldflags，注入版本信息
LDFLAGS="-w -s"
LDFLAGS="${LDFLAGS} -X 'main.version=${BUILD_VERSION}'"
LDFLAGS="${LDFLAGS} -X 'main.gitCommit=${GIT_COMMIT}'"
LDFLAGS="${LDFLAGS} -X 'main.buildDate=${BUILD_DATE}'"
LDFLAGS="${LDFLAGS} -X 'main.goVersion=${BUILD_GO}'"

# 如果设置了 BUILD_URL，注入到 Agent
if [ -n "${BUILD_URL}" ]; then
    AGENT_LDFLAGS="${LDFLAGS} -X 'main.BUILD_URL=${BUILD_URL}'"
else
    AGENT_LDFLAGS="${LDFLAGS}"
fi

# 构建 Server
build_server() {
    local ARCH=$1
    echo "Building Server for ${GOOS}/${ARCH}..."
    
    OUTPUT="${BIN_DIR}/signal_server-${GOOS}-${ARCH}"
    
    CGO_ENABLED=0 \
    GOOS=${GOOS} \
    GOARCH=${ARCH} \
	go build \
		-trimpath \
        -buildvcs=false \
        -ldflags="${LDFLAGS}" \
        -o ${OUTPUT} \
        ./cmd/server
    
    if [ -f "${OUTPUT}" ]; then
        echo "✓ Successfully built: ${OUTPUT}"
        ls -lh ${OUTPUT}
    else
        echo "✗ Failed to build: ${OUTPUT}"
        exit 1
    fi
    echo "---"
}

# 构建 Agent
build_agent() {
    local ARCH=$1
    echo "Building Agent for ${GOOS}/${ARCH}..."
    
    OUTPUT="${BIN_DIR}/signal_agent-${GOOS}-${ARCH}"
    
    CGO_ENABLED=0 \
    GOOS=${GOOS} \
    GOARCH=${ARCH} \
	go build \
		-trimpath \
        -buildvcs=false \
        -ldflags="${AGENT_LDFLAGS}" \
        -o ${OUTPUT} \
        ./cmd/agent
    
    if [ -f "${OUTPUT}" ]; then
        echo "✓ Successfully built: ${OUTPUT}"
        ls -lh ${OUTPUT}
    else
        echo "✗ Failed to build: ${OUTPUT}"
        exit 1
    fi
    echo "---"
}

# 构建 Endpoint
build_endpoint() {
    local ARCH=$1
    echo "Building Endpoint for ${GOOS}/${ARCH}..."
    
    OUTPUT="${BIN_DIR}/signal_endpoint-${GOOS}-${ARCH}"
    
    CGO_ENABLED=0 \
    GOOS=${GOOS} \
    GOARCH=${ARCH} \
	go build \
		-trimpath \
        -buildvcs=false \
        -ldflags="${LDFLAGS}" \
        -o ${OUTPUT} \
        ./cmd/endpoint
    
    if [ -f "${OUTPUT}" ]; then
        echo "✓ Successfully built: ${OUTPUT}"
        ls -lh ${OUTPUT}
    else
        echo "✗ Failed to build: ${OUTPUT}"
        exit 1
    fi
    echo "---"
}

# 分割构建目标列表
IFS=',' read -ra TARGET_ARRAY <<< "$BUILD_TARGET"

# 遍历每个架构进行编译
for ARCH in "${ARCH_ARRAY[@]}"; do
    for TARGET in "${TARGET_ARRAY[@]}"; do
        case ${TARGET} in
            server)
                build_server ${ARCH}
                ;;
            agent)
                build_agent ${ARCH}
                ;;
            endpoint)
                build_endpoint ${ARCH}
                ;;
            all)
                build_server ${ARCH}
                build_agent ${ARCH}
                build_endpoint ${ARCH}
                ;;
        esac
    done
done

# 创建当前平台的符号链接（方便本地开发）
CURRENT_ARCH=$(go env GOARCH)
if [ -f "${BIN_DIR}/signal_server-${GOOS}-${CURRENT_ARCH}" ]; then
    ln -sf "${BIN_DIR}/signal_server-${GOOS}-${CURRENT_ARCH}" "${BIN_DIR}/signal_server"
    echo "✓ Created symlink: bin/signal_server -> signal_server-${GOOS}-${CURRENT_ARCH}"
fi

if [ -f "${BIN_DIR}/signal_agent-${GOOS}-${CURRENT_ARCH}" ]; then
    ln -sf "${BIN_DIR}/signal_agent-${GOOS}-${CURRENT_ARCH}" "${BIN_DIR}/signal_agent"
    echo "✓ Created symlink: bin/signal_agent -> signal_agent-${GOOS}-${CURRENT_ARCH}"
fi

if [ -f "${BIN_DIR}/signal_endpoint-${GOOS}-${CURRENT_ARCH}" ]; then
    ln -sf "${BIN_DIR}/signal_endpoint-${GOOS}-${CURRENT_ARCH}" "${BIN_DIR}/signal_endpoint"
    echo "✓ Created symlink: bin/signal_endpoint -> signal_endpoint-${GOOS}-${CURRENT_ARCH}"
fi

if [ "${RELEASE_ARTIFACTS:-false}" = "true" ]; then
    BUILD_VERSION="${BUILD_VERSION}" GIT_COMMIT="${GIT_COMMIT}" SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH}" \
        ARTIFACT_BASE_URL="${ARTIFACT_BASE_URL}" \
        bash "${BUILD_DIR}/scripts/package-release.sh"
    echo "Release files are in: ${BIN_DIR}/publish/"
fi

echo ""
echo "Build completed successfully!"
echo "Binaries are in: ${BIN_DIR}/"
