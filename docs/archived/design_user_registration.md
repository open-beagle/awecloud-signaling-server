# 用户注册审批

## 背景

当前登录回调自动创建用户并允许登录，没有审批机制。需要增加注册审批流程：来自 Logto 的新用户默认禁用，等待超管激活后才能使用 Desktop。

## User 模型变更

新增两个字段：

| 字段      | 类型   | 默认值   | 说明                                                      |
| --------- | ------ | -------- | --------------------------------------------------------- |
| `enabled` | bool   | true     | 是否启用。手动创建默认 true，Logto 注册默认 false         |
| `source`  | string | "manual" | 来源。"manual" = 管理员手动创建，"logto" = Logto 上游注册 |

GORM 自动迁移添加字段，已有用户默认 enabled=true、source="manual"，不影响现有数据。

## 登录回调变更

查找或创建用户逻辑调整：

- 已有用户 + enabled=true → 正常完成登录
- 已有用户 + enabled=false → 登录失败，返回"用户已禁用，请联系管理员"
- 新用户 → 创建（source=logto, enabled=false），返回"用户未注册，等待管理员审批"

## Proto 变更

新增枚举值 `WAIT_FOR_LOGIN_RESULT_STATUS_DISABLED = 4`，Desktop 收到后退回登录界面。

## Web 管理端变更

- 用户列表显示 enabled 状态和 source 来源标签
- 支持按 enabled 和 source 筛选
- 新增启用/禁用操作按钮

新增 API：

- PUT /api/v1/admin/users/:id/enable - 启用用户
- PUT /api/v1/admin/users/:id/disable - 禁用用户

## Desktop 行为

- 收到 STATUS_DISABLED → 提示"用户未注册或已禁用，请联系管理员审批"→ 退回登录界面
- "切换用户" → 等同于注销，清除 WebView Cookie/Session + 本地凭据，重新走完整登录流程

## 流程图

### 新用户首次登录

```
Desktop                    Server                     Logto
  │                          │                          │
  │── CreateLoginSession ──▶│                          │
  │◀── session_id ──────────│                          │
  │                          │                          │
  │── WebView 打开登录页 ──▶│── 重定向 ──────────────▶│
  │                          │◀── 回调（用户信息）─────│
  │                          │                          │
  │                          │ 用户不存在 → 创建
  │                          │ source=logto, enabled=false
  │                          │                          │
  │◀── STATUS_DISABLED ─────│                          │
  │    "等待管理员审批"      │                          │
  │── 退回登录界面           │                          │
```

### 管理员审批后登录

```
Web 管理端                  Server
  │── PUT /users/:id/enable ▶│
  │◀── 成功 ──────────────── │

Desktop                    Server                     Logto
  │── 重新登录 ────────────▶│── 重定向 ──────────────▶│
  │                          │◀── 回调 ────────────────│
  │                          │ 用户已存在, enabled=true │
  │◀── STATUS_SUCCESS ──────│                          │
```

## 设计决策

1. 手动创建的用户默认 enabled=true，不需要审批
2. 已禁用用户的现有连接在下次心跳/重连时拒绝
3. 不做邮件通知，Web 管理端列表可筛选待审批用户

## 实现状态

- 开发完成：2026-02-07
- 测试状态：待人工测试
- 涉及组件：Server、Web、Desktop 全部编译通过
