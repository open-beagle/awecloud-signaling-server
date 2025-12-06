#!/bin/bash

# 测试下载 API

SERVER_URL="${1:-http://localhost:8080}"

echo "测试下载 API: $SERVER_URL"
echo "================================"

echo ""
echo "1. 测试获取下载列表"
curl -s "$SERVER_URL/api/v1/public/download/desktop" | jq '.'

echo ""
echo "2. 测试直接下载（Windows amd64）"
curl -I "$SERVER_URL/api/v1/public/download/desktop/direct?platform=windows&arch=amd64"

echo ""
echo "3. 测试列出所有版本"
curl -s "$SERVER_URL/api/v1/public/download/desktop/versions" | jq '.'

echo ""
echo "4. 测试访问下载页面"
curl -I "$SERVER_URL/download"

echo ""
echo "================================"
echo "测试完成"
