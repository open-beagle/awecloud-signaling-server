#!/bin/bash

# 管理员认证测试

# 加载公共函数
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common.sh"

# 测试：管理员登录成功
test_admin_login_success() {
    response=$(curl -s -X POST "$API_BASE_URL/api/admin/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}")
    
    if check_success "$response" && check_field_exists "$response" "token"; then
        return 0
    else
        echo "响应: $response"
        return 1
    fi
}

# 测试：管理员登录失败（错误密码）
test_admin_login_fail() {
    response=$(curl -s -X POST "$API_BASE_URL/api/admin/login" \
        -H "Content-Type: application/json" \
        -d '{"username":"admin","password":"wrongpassword"}')
    
    success=$(echo "$response" | jq -r '.success')
    if [ "$success" = "false" ]; then
        return 0
    else
        echo "响应: $response"
        return 1
    fi
}

# 测试：管理员登出
test_admin_logout() {
    token=$(get_admin_token)
    if [ -z "$token" ]; then
        echo "无法获取Token"
        return 1
    fi
    
    response=$(curl -s -X POST "$API_BASE_URL/api/admin/logout" \
        -H "Authorization: Bearer $token")
    
    if check_success "$response"; then
        return 0
    else
        echo "响应: $response"
        return 1
    fi
}

# 运行所有测试
main() {
    start_test "管理员认证测试"
    
    run_test_case "管理员登录成功" test_admin_login_success
    run_test_case "管理员登录失败（错误密码）" test_admin_login_fail
    run_test_case "管理员登出" test_admin_logout
    
    end_test
}

main
exit $?
