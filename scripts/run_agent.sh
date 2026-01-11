#!/bin/bash

# AWECloud Signaling Agent 启动脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

# 默认配置文件
CONFIG_FILE="${1:-config/agent.toml}"

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "错误: 配置文件不存在: $CONFIG_FILE"
    echo "用法: $0 [配置文件路径]"
    echo "示例: $0 config/agent.toml"
    exit 1
fi

./bin/agent -c "$CONFIG_FILE"
