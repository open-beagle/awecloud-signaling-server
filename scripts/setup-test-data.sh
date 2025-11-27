#!/bin/bash

# STCP实例创建和授权测试脚本
# 用法: ./scripts/setup-test-data.sh

set -e

SERVER_URL="http://localhost:8080"
ADMIN_USER="admin"
ADMIN_PASS="admin123"

echo "=== AWECloud Signaling Server - 测试数据设置 ==="
echo ""

# 1. 管理员登录
echo "1. 管理员登录..."
LOGIN_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/admin/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}")

TOKEN=$(echo $LOGIN_RESPONSE | grep -o '"token":"[^"]*' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo "❌ 登录失败！"
  echo "响应: $LOGIN_RESPONSE"
  exit 1
fi

echo "✅ 登录成功"
echo ""

# 2. 获取Agent列表
echo "2. 获取Agent列表..."
AGENTS_RESPONSE=$(curl -s -X GET "${SERVER_URL}/api/agents" \
  -H "Authorization: Bearer ${TOKEN}")

echo "Agent列表: $AGENTS_RESPONSE"
echo ""

# 提取第一个在线Agent的ID
AGENT_ID=$(echo $AGENTS_RESPONSE | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

if [ -z "$AGENT_ID" ]; then
  echo "❌ 没有找到Agent！请先创建并启动Agent"
  exit 1
fi

echo "✅ 找到Agent ID: $AGENT_ID"
echo ""

# 3. 获取Client列表
echo "3. 获取Client列表..."
CLIENTS_RESPONSE=$(curl -s -X GET "${SERVER_URL}/api/clients" \
  -H "Authorization: Bearer ${TOKEN}")

echo "Client列表: $CLIENTS_RESPONSE"
echo ""

# 提取第一个Client的ID
CLIENT_ID=$(echo $CLIENTS_RESPONSE | grep -o '"id":[0-9]*' | head -1 | cut -d':' -f2)

if [ -z "$CLIENT_ID" ]; then
  echo "⚠️  没有找到Client，正在创建..."
  
  # 创建Client
  CREATE_CLIENT_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/clients" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -d '{}')
  
  echo "创建Client响应: $CREATE_CLIENT_RESPONSE"
  CLIENT_ID=$(echo $CREATE_CLIENT_RESPONSE | grep -o '"id":[0-9]*' | cut -d':' -f2)
  
  if [ -z "$CLIENT_ID" ]; then
    echo "❌ 创建Client失败！"
    exit 1
  fi
fi

echo "✅ 找到Client ID: $CLIENT_ID"
echo ""

# 4. 创建STCP实例
echo "4. 创建STCP实例..."
INSTANCE_NAME="test-web-service-$(date +%s)"
CREATE_STCP_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/stcp-instances" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{
    \"agent_id\": ${AGENT_ID},
    \"instance_name\": \"${INSTANCE_NAME}\",
    \"local_ip\": \"127.0.0.1\",
    \"local_port\": 8080,
    \"description\": \"测试Web服务\"
  }")

echo "创建STCP实例响应: $CREATE_STCP_RESPONSE"
echo ""

INSTANCE_ID=$(echo $CREATE_STCP_RESPONSE | grep -o '"id":[0-9]*' | cut -d':' -f2)

if [ -z "$INSTANCE_ID" ]; then
  echo "❌ 创建STCP实例失败！"
  exit 1
fi

echo "✅ 创建STCP实例成功，ID: $INSTANCE_ID"
echo ""

# 5. 授权Client访问
echo "5. 授权Client访问STCP实例..."
GRANT_RESPONSE=$(curl -s -X POST "${SERVER_URL}/api/stcp-instances/${INSTANCE_ID}/grant" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{\"client_id\": ${CLIENT_ID}}")

echo "授权响应: $GRANT_RESPONSE"
echo ""

if echo $GRANT_RESPONSE | grep -q '"success":true'; then
  echo "✅ 授权成功！"
else
  echo "❌ 授权失败！"
  exit 1
fi

echo ""
echo "=== 设置完成 ==="
echo ""
echo "📋 测试数据摘要："
echo "  - Agent ID: $AGENT_ID"
echo "  - Client ID: $CLIENT_ID"
echo "  - STCP实例 ID: $INSTANCE_ID"
echo "  - STCP实例名称: $INSTANCE_NAME"
echo ""
echo "🎉 现在可以使用Desktop客户端登录并查看服务列表了！"
echo ""
echo "💡 提示："
echo "  1. 启动Desktop客户端"
echo "  2. 使用Client ID和Secret登录"
echo "  3. 应该能看到服务: $INSTANCE_NAME"
echo ""
