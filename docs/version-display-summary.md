# 版本展示功能实现总结

## 实现方案

采用客户端上报方案：
- Agent 已在心跳中上报版本号到 `nodes` 表的 `version` 字段
- Server 存储所有客户端版本信息
- 用户列表页面展示各用户的设备版本，并与最新版本比对提示升级

## 已完成的修改

### 后端

1. **internal/server/api/user.go**
   - 修改 `UserListItem` 结构，添加 `Versions` 和 `LatestVersion` 字段
   - 修改 `List` 方法，查询用户设备版本信息并去重
   - 从所有 Agent 节点统计最新版本号

2. **internal/server/api/version.go**
   - 已存在版本统计 API（`/api/v1/admin/version/latest`）
   - 统计 Agent 和 Endpoint 的最新版本

### 前端

1. **web/src/api/version.ts**
   - 创建版本 API 客户端
   - 定义 `VersionInfo` 接口

2. **web/src/api/user.ts**
   - 更新 `User` 接口，添加 `versions` 和 `latest_version` 字段

3. **web/src/types/models.ts**
   - 更新 `User` 类型定义，添加版本字段

4. **web/src/views/User/List.vue**
   - 添加版本列，显示用户所有设备的版本号
   - 实现版本比对逻辑（`compareVersion` 函数）
   - 低于最新版本时显示警告图标和升级提示

5. **web/src/locales/zh-CN.ts** 和 **web/src/locales/en-US.ts**
   - 添加版本相关的国际化翻译

## 功能特性

- 显示每个用户的所有设备版本（去重）
- 自动比对版本号，低于最新版本时显示警告
- 鼠标悬停显示升级提示信息
- 支持中英文国际化

## 注意事项

- Desktop 版本上报功能暂未实现（按用户要求）
- 版本比对采用语义化版本号规则（x.y.z 格式）
- 最新版本从所有已连接的 Agent 节点统计得出
