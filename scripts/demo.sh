#!/bin/bash

# AWECloud Signaling 演示脚本

set -e

echo "========================================="
echo "AWECloud Signaling 演示"
echo "========================================="
echo ""

# 颜色
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

success() {
    echo -e "${GREEN}✅ $1${NC}"
}

# 1. 检查Server是否运行
info "检查Server状态..."
if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "❌ Server未运行，请先启动Server:"
    echo "   ./bin/server -c config/server.toml"
    exit 1
fi
success "Server正在运行"
echo ""

# 2. 登录获取Token
info "管理员登录..."
TOKEN=$(curl -s -X POST http://localhost:8080/api/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.token')

if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
    echo "❌ 登录失败"
    exit 1
fi
success "登录成功"
echo ""

# 3. 创建Agent
info "创建Agent..."
AGENT_RESPONSE=$(curl -s -X POST http://localhost:8080/api/agents \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"agent_name":"demo-agent"}')

AGENT_ID=$(echo "$AGENT_RESPONSE" | jq -r '.agent.id')
AGENT_TOKEN=$(echo "$AGENT_RESPONSE" | jq -r '.agent.agent_token')

if [ -z "$AGENT_TOKEN" ] || [ "$AGENT_TOKEN" = "null" ]; then
    echo "❌ 创建Agent失败"
    echo "$AGENT_RESPONSE" | jq .
    exit 1
fi

success "Agent创建成功"
echo "   Agent ID: $AGENT_ID"
echo "   Agent Token: $AGENT_TOKEN"
echo ""

# 4. 创建Client
info "创建Client..."
CLIENT_RESPONSE=$(curl -s -X POST http://localhost:8080/api/clients \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"client_id":"demo@example.com"}')

CLIENT_ID=$(echo "$CLIENT_RESPONSE" | jq -r '.client.id')
CLIENT_SECRET=$(echo "$CLIENT_RESPONSE" | jq -r '.client.client_secret')

success "Client创建成功"
echo "   Client ID: $CLIENT_ID"
echo "   Client Secret: $CLIENT_SECRET"
echo ""

# 5. 创建STCP实例
info "创建STCP实例..."
STCP_RESPONSE=$(curl -s -X POST http://localhost:8080/api/stcp-instances \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"instance_name\":\"demo-mysql\",
    \"agent_id\":$AGENT_ID,
    \"local_ip\":\"127.0.0.1\",
    \"local_port\":3306,
    \"description\":\"演示MySQL数据库\"
  }")

STCP_ID=$(echo "$STCP_RESPONSE" | jq -r '.instance.id')
SECRET_KEY=$(echo "$STCP_RESPONSE" | jq -r '.instance.secret_key')

success "STCP实例创建成功"
echo "   Instance ID: $STCP_ID"
echo "   Secret Key: $SECRET_KEY"
echo ""

# 6. 授权Client访问
info "授权Client访问STCP实例..."
curl -s -X POST "http://localhost:8080/api/stcp-instances/$STCP_ID/grant-access" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"client_id\":$CLIENT_ID}" > /dev/null

success "授权成功"
echo ""

# 7. 显示配置信息
echo "========================================="
echo "演示环境配置完成"
echo "========================================="
echo ""
echo "Agent配置 (config/agent.toml):"
echo "  agent_name = \"demo-agent\""
echo "  agent_token = \"$AGENT_TOKEN\""
echo ""
echo "启动Agent命令:"
echo "  ./bin/agent -c config/agent.toml"
echo ""
echo "Client认证信息:"
echo "  client_id = \"demo@example.com\""
echo "  client_secret = \"$CLIENT_SECRET\""
echo ""
echo "STCP实例信息:"
echo "  instance_name = \"demo-mysql\""
echo "  secret_key = \"$SECRET_KEY\""
echo ""
echo "========================================="
echo "下一步："
echo "1. 更新config/agent.toml中的agent_token"
echo "2. 启动Agent: ./bin/agent -c config/agent.toml"
echo "3. 观察Agent日志，应该能看到CREATE_STCP命令"
echo "========================================="
