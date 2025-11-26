#!/bin/bash

# 运行所有测试

# 加载公共函数
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

# 检查Server是否运行
check_server() {
    if ! curl -s "$API_BASE_URL/health" > /dev/null 2>&1; then
        fail "Server未运行，请先启动Server"
        echo ""
        echo "启动命令："
        echo "  ./bin/server -c config/server.toml"
        echo ""
        exit 1
    fi
    success "Server正在运行"
}

# 运行测试脚本
run_test_script() {
    local script=$1
    local script_name=$(basename "$script")
    
    echo ""
    echo "========================================="
    echo "测试: $script_name"
    echo "========================================="
    
    if bash "$script"; then
        success "$script_name 通过"
        return 0
    else
        fail "$script_name 失败"
        return 1
    fi
}

# 主函数
main() {
    echo "========================================="
    echo "运行 API 测试"
    echo "========================================="
    
    # 检查Server
    check_server
    
    # 测试脚本列表
    local test_scripts=(
        "$SCRIPT_DIR/api/test_admin.sh"
        "$SCRIPT_DIR/api/test_agent.sh"
    )
    
    local total=0
    local passed=0
    local failed=0
    
    # 运行所有测试
    for script in "${test_scripts[@]}"; do
        if [ -f "$script" ]; then
            total=$((total + 1))
            if run_test_script "$script"; then
                passed=$((passed + 1))
            else
                failed=$((failed + 1))
            fi
        else
            warn "测试脚本不存在: $script"
        fi
    done
    
    # 输出总结
    echo ""
    echo "========================================="
    echo "测试结果"
    echo "========================================="
    echo "总计: $total"
    echo "通过: $passed"
    echo "失败: $failed"
    echo "========================================="
    
    if [ $failed -gt 0 ]; then
        exit 1
    else
        exit 0
    fi
}

main
