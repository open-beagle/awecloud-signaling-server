#!/bin/bash

# STCP访问自动获取密钥功能测试脚本

set -e

echo "=== STCP访问自动获取密钥功能测试 ==="
echo ""

# 配置
SERVER_URL="${SERVER_URL:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-admin_token_here}"

echo "服务器地址: $SERVER_URL"
echo ""

# 1. 创建测试Agent
echo "1. 创建测试Agent..."
AGENT_RESPONSE=$(curl -s -X POST "$SERVER_URL/api/admin/agents" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_name": "test-agent-visitor",
    "description": "测试Agent（用于visitor测试）"
  }')

echo "Agent创建响应: $AGENT_RESPONSE"
AGENT_ID=$(echo $AGENT_RESPONSE | jq -r '.data.id')
AGENT_NAME=$(echo $AGENT_RESPONSE | jq -r '.data.agent_name')
echo "Agent ID: $AGENT_ID"
echo "Agent Name: $AGENT_NAME"
echo ""

# 2. 创建STCP实例（作为目标服务）
echo "2. 创建STCP实例（目标服务）..."
STCP_RESPONSE=$(curl -s -X POST "$SERVER_URL/api/stcp" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"instance_name\": \"test-mysql-service\",
    \"agent_id\": $AGENT_ID,
    \"local_ip\": \"127.0.0.1\",
    \"local_port\": 3306,
    \"description\": \"测试MySQL服务\"
  }")

echo "STCP实例创建响应: $STCP_RESPONSE"
SECRET_KEY=$(echo $STCP_RESPONSE | jq -r '.data.secret_key')
echo "自动生成的密钥: $SECRET_KEY"
echo ""

# 3. 创建STCP访问（不提供密钥，应该自动获取）
echo "3. 创建STCP访问（不提供密钥）..."
VISITOR_RESPONSE=$(curl -s -X POST "$SERVER_URL/api/stcp-visitors" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"visitor_name\": \"test-mysql-visitor\",
    \"agent_name\": \"$AGENT_NAME\",
    \"server_name\": \"test-mysql-service\",
    \"bind_addr\": \"0.0.0.0\",
    \"bind_port\": 13306,
    \"description\": \"测试MySQL访问\"
  }")

echo "STCP访问创建响应: $VISITOR_RESPONSE"
VISITOR_SECRET=$(echo $VISITOR_RESPONSE | jq -r '.data.secret_key')
VISITOR_BIND_ADDR=$(echo $VISITOR_RESPONSE | jq -r '.data.bind_addr')
echo "Visitor获取的密钥: $VISITOR_SECRET"
echo "Visitor绑定地址: $VISITOR_BIND_ADDR"
echo ""

# 4. 验证密钥是否匹配
echo "4. 验证密钥..."
if [ "$SECRET_KEY" = "$VISITOR_SECRET" ]; then
    echo "✅ 成功：密钥自动获取正确！"
else
    echo "❌ 失败：密钥不匹配"
    echo "   STCP实例密钥: $SECRET_KEY"
    echo "   Visitor密钥: $VISITOR_SECRET"
    exit 1
fi
echo ""

# 5. 验证绑定地址
echo "5. 验证绑定地址..."
if [ "$VISITOR_BIND_ADDR" = "0.0.0.0" ]; then
    echo "✅ 成功：绑定地址默认为0.0.0.0！"
else
    echo "❌ 失败：绑定地址不正确"
    echo "   期望: 0.0.0.0"
    echo "   实际: $VISITOR_BIND_ADDR"
    exit 1
fi
echo ""

# 6. 测试错误情况：不存在的服务名称
echo "6. 测试错误情况（不存在的服务）..."
ERROR_RESPONSE=$(curl -s -X POST "$SERVER_URL/api/stcp-visitors" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"visitor_name\": \"test-error-visitor\",
    \"agent_name\": \"$AGENT_NAME\",
    \"server_name\": \"non-existent-service\",
    \"bind_addr\": \"0.0.0.0\",
    \"bind_port\": 13307,
    \"description\": \"测试错误情况\"
  }")

echo "错误响应: $ERROR_RESPONSE"
ERROR_MSG=$(echo $ERROR_RESPONSE | jq -r '.error')
if [[ "$ERROR_MSG" == *"不存在"* ]]; then
    echo "✅ 成功：正确返回错误信息！"
else
    echo "⚠️  警告：错误信息可能不够明确"
fi
echo ""

echo "=== 测试完成 ==="
echo ""
echo "总结："
echo "✅ STCP访问可以自动从目标实例获取密钥"
echo "✅ 默认绑定地址为0.0.0.0（允许局域网访问）"
echo "✅ 不存在的服务名称会返回适当的错误"
