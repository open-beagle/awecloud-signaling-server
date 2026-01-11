#!/bin/bash

set -e

# 版本信息（日常开发使用默认值，流水线传递完整参数）
BUILD_VERSION="${BUILD_VERSION:-v0.2.0}"

echo "Building frontend..."
echo "Version: ${BUILD_VERSION}"
echo "---"

# 进入web目录
cd web

# 检查Node.js版本
NODE_VERSION=$(node -v | cut -d'v' -f2 | cut -d'.' -f1)
if [ "$NODE_VERSION" -lt 16 ]; then
    echo "⚠️  Warning: Node.js version should be >= 16"
fi

# 检查node_modules是否存在
if [ ! -d "node_modules" ]; then
    echo "Installing dependencies..."
    npm install --legacy-peer-deps
else
    echo "Dependencies already installed, skipping..."
fi

# 清理旧的构建产物
if [ -d "dist" ]; then
    echo "Cleaning old build..."
    rm -rf dist
fi

# 构建前端
echo "Building Vue application..."
npm run build

# 检查构建是否成功
if [ -d "dist" ]; then
    echo "✓ Frontend build successful!"
    echo "Output: web/dist/"
    
    # 显示构建产物大小
    du -sh dist
    
    # 列出主要文件
    echo ""
    echo "Build files:"
    ls -lh dist/
else
    echo "✗ Frontend build failed!"
    exit 1
fi

echo "Done!"
