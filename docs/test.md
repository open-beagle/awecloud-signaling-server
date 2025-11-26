# AWECloud-Signaling 测试规范

## 测试原则

1. **手动启动程序**：测试前需要手动启动Server程序
2. **脚本测试**：使用Shell脚本进行API测试
3. **独立测试**：每个测试脚本应该独立可运行
4. **清理数据**：测试前清理数据库，确保测试环境干净

## 测试目录结构

```
tests/
├── api/                    # API测试脚本
│   ├── test_admin.sh      # 管理员认证测试
│   ├── test_agent.sh      # Agent管理测试
│   ├── test_client.sh     # Client管理测试
│   ├── test_stcp.sh       # STCP实例管理测试
│   └── test_client_auth.sh # Client认证和服务查询测试
├── common.sh              # 公共函数和变量
└── run_all.sh             # 运行所有测试
```

## 测试流程

### 1. 准备测试环境

```bash
# 清理旧数据
rm -f data/awecloud.db

# 启动Server（手动）
./bin/server -c config/server.toml
```

### 2. 运行测试

```bash
# 运行所有测试
./tests/run_all.sh

# 或运行单个测试
./tests/api/test_admin.sh
./tests/api/test_agent.sh
```

### 3. 查看结果

测试脚本会输出：
- ✅ 测试通过
- ❌ 测试失败（包含错误信息）

## 测试用例

### 管理员认证测试 (test_admin.sh)

- [x] 管理员登录成功
- [x] 管理员登录失败（错误密码）
- [x] 管理员登出

### Agent管理测试 (test_agent.sh)

- [x] 创建Agent
- [x] 获取Agent列表
- [x] 重新生成Agent Token
- [x] 删除Agent

### Client管理测试 (test_client.sh)

- [x] 创建Client
- [x] 获取Client列表
- [x] 禁用Client
- [x] 启用Client
- [x] 重新生成Client Secret
- [x] 删除Client

### STCP实例管理测试 (test_stcp.sh)

- [x] 创建STCP实例
- [x] 获取STCP实例列表
- [x] 授权Client访问
- [x] 撤销Client访问
- [x] 删除STCP实例

### Client认证测试 (test_client_auth.sh)

- [x] Client认证成功
- [x] Client认证失败（错误密钥）
- [x] Client认证失败（已禁用）
- [x] 获取可访问服务列表
- [x] 验证会话Token

## 测试脚本规范

### 脚本模板

```bash
#!/bin/bash

# 加载公共函数
source "$(dirname "$0")/../common.sh"

# 测试名称
TEST_NAME="测试名称"

# 测试函数
test_case_1() {
    echo "测试用例1..."
    
    # 发送请求
    response=$(curl -s -X POST http://localhost:8080/api/endpoint \
        -H "Content-Type: application/json" \
        -d '{"key":"value"}')
    
    # 验证响应
    success=$(echo "$response" | jq -r '.success')
    if [ "$success" = "true" ]; then
        echo "✅ 测试用例1通过"
        return 0
    else
        echo "❌ 测试用例1失败"
        echo "响应: $response"
        return 1
    fi
}

# 运行测试
run_tests() {
    echo "========================================="
    echo "开始测试: $TEST_NAME"
    echo "========================================="
    
    test_case_1
    
    echo "========================================="
    echo "测试完成: $TEST_NAME"
    echo "========================================="
}

# 执行测试
run_tests
```

### 公共函数 (common.sh)

```bash
#!/bin/bash

# 配置
API_BASE_URL="http://localhost:8080"
ADMIN_USERNAME="admin"
ADMIN_PASSWORD="admin123"

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 成功输出
success() {
    echo -e "${GREEN}✅ $1${NC}"
}

# 失败输出
fail() {
    echo -e "${RED}❌ $1${NC}"
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

# 验证JSON响应
check_success() {
    local response=$1
    local success=$(echo "$response" | jq -r '.success')
    [ "$success" = "true" ]
}
```

## 注意事项

1. **依赖工具**：
   - `curl`：发送HTTP请求
   - `jq`：解析JSON响应
   - `bash`：运行测试脚本

2. **测试前准备**：
   - 确保Server已启动
   - 确保端口8080可访问
   - 确保数据库已清理

3. **测试隔离**：
   - 每个测试脚本应该独立
   - 不依赖其他测试的执行顺序
   - 测试数据应该在脚本内创建和清理

4. **错误处理**：
   - 测试失败时输出详细错误信息
   - 返回非零退出码表示测试失败

## 持续集成

未来可以集成到CI/CD流程：

```yaml
# .github/workflows/test.yml
name: API Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Build
        run: ./scripts/build.sh
      - name: Start Server
        run: ./bin/server -c config/server.toml &
      - name: Wait for Server
        run: sleep 5
      - name: Run Tests
        run: ./tests/run_all.sh
```

---

**文档版本**: 1.0  
**最后更新**: 2025-11-25
