#!/bin/bash

# AWECloud Signaling Server 启动脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

# 默认配置文件
CONFIG_FILE="${1:-config/server.toml}"

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "错误: 配置文件不存在: $CONFIG_FILE"
    echo "用法: $0 [配置文件路径]"
    echo "示例: $0 config/server.toml"
    exit 1
fi

echo "=========================================="
echo "AWECloud Signaling Server"
echo "=========================================="
echo "配置文件: $CONFIG_FILE"
echo ""

# 运行Server
echo "启动Server..."
echo ""
echo "端口说明:"
echo "  - 8080: HTTP/2统一端口（Web + RESTful API + gRPC）"
echo "  - 7000: FRP信令服务（WebSocket）"
echo ""
./bin/server -c "$CONFIG_FILE"
