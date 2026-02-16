#!/bin/bash

# 生成gRPC代码

set -e

echo "生成 Protocol Buffers 代码..."

# 检查protoc是否安装
if ! command -v protoc &> /dev/null; then
    echo "错误: protoc 未安装"
    echo "请安装 protoc: https://grpc.io/docs/protoc-installation/"
    exit 1
fi

# 检查protoc-gen-go是否安装
if ! command -v protoc-gen-go &> /dev/null; then
    echo "错误: protoc-gen-go 未安装"
    echo "安装命令: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
fi

# 检查protoc-gen-go-grpc是否安装
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "错误: protoc-gen-go-grpc 未安装"
    echo "安装命令: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    exit 1
fi

# 创建输出目录
mkdir -p pkg/proto

# 生成agent.proto
echo "生成 agent.proto..."
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pkg/proto/agent.proto

# 生成endpoint.proto
echo "生成 endpoint.proto..."
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pkg/proto/endpoint.proto

# 生成desktop.proto
echo "生成 desktop.proto..."
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    pkg/proto/desktop.proto

echo "✓ Protocol Buffers 代码生成完成"
