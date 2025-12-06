#!/bin/bash

# 测试下载 API 的操作系统检测功能

SERVER_URL="${1:-http://localhost:8080}"

echo "测试下载 API 操作系统检测"
echo "================================"

echo ""
echo "1. 测试自动检测（当前系统）"
curl -s "$SERVER_URL/api/v1/public/download/desktop" | jq '.'

echo ""
echo "2. 测试 Windows 检测"
curl -s -H "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)" \
  "$SERVER_URL/api/v1/public/download/desktop" | jq '.'

echo ""
echo "3. 测试 macOS 检测"
curl -s -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)" \
  "$SERVER_URL/api/v1/public/download/desktop" | jq '.'

echo ""
echo "4. 测试 Linux 检测"
curl -s -H "User-Agent: Mozilla/5.0 (X11; Linux x86_64)" \
  "$SERVER_URL/api/v1/public/download/desktop" | jq '.'

echo ""
echo "5. 测试指定操作系统（os=windows）"
curl -s "$SERVER_URL/api/v1/public/download/desktop?os=windows" | jq '.'

echo ""
echo "6. 测试指定操作系统（os=macos）"
curl -s "$SERVER_URL/api/v1/public/download/desktop?os=macos" | jq '.'

echo ""
echo "7. 测试获取所有平台"
curl -s "$SERVER_URL/api/v1/public/download/desktop/versions" | jq '.'

echo ""
echo "8. 测试直接下载重定向（Windows）"
curl -I -H "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64)" \
  "$SERVER_URL/api/v1/public/download/desktop/direct"

echo ""
echo "================================"
echo "测试完成"
