#!/bin/bash

# AWECloud-Signaling 测试公共函数

# 配置
API_BASE_URL="http://localhost:8080"
ADMIN_USERNAME="admin"
ADMIN_PASSWORD="admin123"

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 成功输出
success() {
    echo -e "${GREEN}✅ $1${NC}"
}

# 失败输出
fail() {
    echo -e "${RED}❌ $1${NC}"
}

# 信息输出
info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# 警告输出
warn() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

# 获取管理员Token
get_admin_token() {
    response=$(curl -s -X POST "$API_BASE_URL/api/admin/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}")
    
    token=$(echo "$response" | jq -r '.token')
    if [ "$token" != "null" ] && [ -n "$token" ]; then
        echo "$token"
        return 0
    else
        echo ""
        return 1
    fi
}

# 验证JSON响应成功
check_success() {
    local response=$1
    local success=$(echo "$response" | jq -r '.success')
    [ "$success" = "true" ]
}

# 验证JSON字段存在
check_field_exists() {
    local response=$1
    local field=$2
    local value=$(echo "$response" | jq -r ".$field")
    [ "$value" != "null" ] && [ -n "$value" ]
}

# 验证JSON字段值
check_field_value() {
    local response=$1
    local field=$2
    local expected=$3
    local actual=$(echo "$response" | jq -r ".$field")
    [ "$actual" = "$expected" ]
}

# 等待Server启动
wait_for_server() {
    local max_attempts=30
    local attempt=0
    
    info "等待Server启动..."
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "$API_BASE_URL/health" > /dev/null 2>&1; then
            success "Server已启动"
            return 0
        fi
        
        attempt=$((attempt + 1))
        sleep 1
    done
    
    fail "Server启动超时"
    return 1
}

# 测试计数器
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 开始测试
start_test() {
    local test_name=$1
    echo ""
    echo "---"
    echo "$test_name"
    echo "---"
}

# 结束测试
end_test() {
    echo "---"
    echo "结果: $PASSED_TESTS/$TOTAL_TESTS 通过"
    if [ $FAILED_TESTS -gt 0 ]; then
        echo "失败: $FAILED_TESTS"
    fi
    echo "---"
    echo ""
    
    if [ $FAILED_TESTS -gt 0 ]; then
        return 1
    else
        return 0
    fi
}

# 运行测试用例
run_test_case() {
    local test_name=$1
    local test_func=$2
    
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    
    echo ""
    info "运行: $test_name"
    
    if $test_func; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
        success "$test_name"
        return 0
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
        fail "$test_name"
        return 1
    fi
}
