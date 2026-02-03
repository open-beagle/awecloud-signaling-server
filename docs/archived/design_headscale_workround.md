# Headscale ACL Tag 解析问题

## 问题现象

Desktop 客户端连接 Headscale 后收到 0 peers、0 filters，无法看到 Agent 节点，导致无法建立连接。

## 根本原因

Headscale 的 ACL 策略解析器 parseAlias 函数中，类型判断顺序如下：

```
parseAlias 判断顺序：
┌─────────────────────────────────────────┐
│ 1. isWildcard - 检查 *                  │
│ 2. isUser - 检查包含 @        ← 先执行  │
│ 3. isGroup - 检查 group: 前缀           │
│ 4. isTag - 检查 tag: 前缀     ← 后执行  │
└─────────────────────────────────────────┘
```

当 Tag 格式为 `tag:client-shucheng@bd-apaas.com` 时：

1. isUser 检测到 @ 符号，返回 true
2. Tag 被错误解析为 Username 类型
3. ACL 规则中的 src/dst 无法匹配到任何节点
4. compileFilterRules 返回空，导致 filter 为 null

## 影响范围

所有包含 @ 符号的 Tag 都会被错误解析，包括：

- tag:client-user@domain.com
- tag:agent-name@company.com

## 解决方案

### 方案一：避免在用户名中使用 @ 符号（推荐）

修改用户命名规范，使用其他分隔符替代 @：

| 原格式                | 新格式                |
| --------------------- | --------------------- |
| shucheng@bd-apaas.com | shucheng.bd-apaas.com |
| user@domain.com       | user_domain_com       |

### 方案二：向 Headscale 提交 PR

修改 parseAlias 函数，将 isTag 检查移到 isUser 之前：

```
建议的判断顺序：
┌─────────────────────────────────────────┐
│ 1. isWildcard - 检查 *                  │
│ 2. isTag - 检查 tag: 前缀     ← 移到前面│
│ 3. isGroup - 检查 group: 前缀           │
│ 4. isUser - 检查包含 @                  │
└─────────────────────────────────────────┘
```

## 验证方法

1. 检查 /debug/filter 端点，确认返回非 null 的 filter rules
2. 检查 /debug/policy-manager 端点，确认 Matchers 部分有内容
3. Desktop 连接后检查 peers 数量是否大于 0

## 相关文件

- Headscale 源码位置：hscontrol/policy/v2/types.go 中的 parseAlias 函数
- 本项目 ACL 同步位置：internal/server/headscale/acl_sync.go
