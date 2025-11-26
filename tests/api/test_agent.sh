#!/bin/bash

# Agent管理测试

# 加载公共函数
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common.sh"

# 全局变量
AGENT_ID=""
AGENT_TOKEN=""

# 测试：创建Agent
test_create_agent() {
    token=$(get_admin_token)
    if [ -z "$token" ]; then
        echo "无法获取管理员Token"
        return 1
    fi
    
    response=$(curl -s -X POST "$API_BASE_URL/api/agents" \
        -H "Authorization: Bearer $token" \
        -H "Content-Type: application/json" \
        -d '{"agent_name":"test-agent"}')
    
    if check_success "$response" && check_field_exists "$response" "agent.id"; then
        AGENT_ID=$(echo "$response" | jq -r '.agent.id')
        AGENT_TOKEN=$(echo "$response" | jq -r '.agent.agent_token')
        return 0
    else
        echo "响应: $response"
        return 1
    fi
}

# 测试：获取Agent列表
test_list_agents() {
    token=$(get_admin_token)
    if [ -z "$token" ]; then
        echo "无法获取管理员Token"
        return 1
    fi
    
    response=$(curl -s -X GET "$API_BASE_URL/api/agents" \
        -H "Authorization: Bearer $token")
    
    if check_success "$response" && check_field_exists "$response" "agents"; then
        return 0
    else
        echo "响应: $response"
        return 1
    fi
}

# 测试：重新生成Agent Token
test_regenerate_token() {
    if [ -z "$AGENT_ID" ]; then
        echo "Agent ID未设置"
        return 1
    fi
    
    token=$(get_admin_token)
    if [ -z "$token" ]; then
        echo "无法获取管理员Token"
        return 1
    fi
    
    response=$(curl -s -X POST "$API_BASE_URL/api/agents/$AGENT_ID/regenerate-token" \
        -H "Authorization: Bearer $token")
    
    if check_success "$response" && check_field_exists "$response" "agent.agent_token"; then
        new_token=$(echo "$response" | jq -r '.agent.agent_token')
        if [ "$new_token" != "$AGENT_TOKEN" ]; then
            return 0
        else
            echo "Token未改变"
            return 1
        fi
    else
        echo "响应: $response"
        return 1
    fi
}

# 测试：删除Agent
test_delete_agent() {
    if [ -z "$AGENT_ID" ]; then
        echo "Agent ID未设置"
        return 1
    fi
    
    token=$(get_admin_token)
    if [ -z "$token" ]; then
        echo "无法获取管理员Token"
        return 1
    fi
    
    response=$(curl -s -X DELETE "$API_BASE_URL/api/agents/$AGENT_ID" \
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
    start_test "Agent管理测试"
    
    run_test_case "创建Agent" test_create_agent
    run_test_case "获取Agent列表" test_list_agents
    run_test_case "重新生成Agent Token" test_regenerate_token
    run_test_case "删除Agent" test_delete_agent
    
    end_test
}

main
exit $?
