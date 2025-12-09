# STCP访问功能升级指南

## 概述

本次升级改进了STCP访问的用户体验和功能：

1. **自动获取密钥**：创建STCP访问时不再需要手动输入密钥，系统自动从目标STCP实例获取
2. **局域网访问**：默认绑定地址改为0.0.0.0，支持局域网内的设备访问

## 升级步骤

### 1. 更新代码

```bash
# 拉取最新代码
git pull origin main

# 重新编译后端
cd cmd/server
go build -o ../../bin/server

# 重新编译Desktop（如果使用）
cd ../../desktop
go build -o ../bin/desktop
```

### 2. 更新数据库

运行迁移脚本更新现有的STCP访问记录：

```bash
# SQLite数据库
sqlite3 data/signaling.db < scripts/update_stcp_visitor_bind_addr.sql

# PostgreSQL数据库
psql -d signaling -f scripts/update_stcp_visitor_bind_addr.sql

# MySQL数据库
mysql -u root -p signaling < scripts/update_stcp_visitor_bind_addr.sql
```

### 3. 重启服务

```bash
# 停止现有服务
pkill -f bin/server

# 启动新版本
./bin/server -c config/server.toml
```

### 4. 更新前端（如果单独部署）

```bash
cd web
npm install
npm run build
```

## 功能测试

### 测试1：自动获取密钥

1. 登录管理后台
2. 进入"服务管理" -> "STCP访问"
3. 点击"新建STCP访问"
4. 填写表单（注意：不再有密钥字段）
   - 访问名称：test-visitor
   - 所属Agent：选择一个Agent
   - 目标服务名称：填写已存在的STCP实例名称
   - 绑定地址：0.0.0.0（默认值）
   - 绑定端口：13306
5. 点击"确定"
6. 验证创建成功

### 测试2：局域网访问

1. 在Desktop客户端上开放一个端口（例如3306）
2. 从局域网内的另一台设备尝试连接：
   ```bash
   # 假设Desktop所在机器IP为192.168.1.100
   mysql -h 192.168.1.100 -P 3306 -u user -p
   ```
3. 验证连接成功

### 自动化测试

运行测试脚本验证功能：

```bash
# 设置环境变量
export SERVER_URL="http://localhost:8080"
export ADMIN_TOKEN="your_admin_token"

# 运行测试
./scripts/test_stcp_visitor_auto_key.sh
```

## 兼容性说明

### 向后兼容

- ✅ 现有的STCP访问记录仍然有效
- ✅ 运行迁移脚本后，现有记录会自动更新绑定地址
- ✅ API保持向后兼容（仍接受secret_key字段，但会被忽略）

### 注意事项

1. **绑定地址变更**
   - 旧版本：默认127.0.0.1（仅本机访问）
   - 新版本：默认0.0.0.0（允许局域网访问）
   - 如果需要限制为本机访问，可以手动修改为127.0.0.1

2. **防火墙配置**
   - 绑定到0.0.0.0后，确保防火墙规则允许相应端口的访问
   - 建议根据实际需求配置防火墙规则

3. **安全性**
   - 数据传输仍通过STCP隧道加密
   - 需要正确的密钥才能建立连接
   - 局域网访问不会降低安全性

## 回滚方案

如果需要回滚到旧版本：

```bash
# 1. 回滚代码
git checkout <previous_commit>

# 2. 重新编译
go build -o bin/server cmd/server/main.go

# 3. 回滚数据库（可选）
# 将bind_addr改回127.0.0.1
sqlite3 data/signaling.db "UPDATE stcp_visitors SET bind_addr = '127.0.0.1' WHERE bind_addr = '0.0.0.0';"

# 4. 重启服务
pkill -f bin/server
./bin/server -c config/server.toml
```

## 常见问题

### Q1: 创建STCP访问时提示"目标STCP服务不存在"

**A**: 确保填写的服务名称与已存在的STCP实例名称完全一致（区分大小写）。

### Q2: 局域网内的设备无法连接

**A**: 检查以下几点：
1. 确认绑定地址为0.0.0.0
2. 检查防火墙是否允许相应端口
3. 确认STCP访问已启用
4. 验证Agent在线状态

### Q3: 如何限制为本机访问

**A**: 在创建或编辑STCP访问时，将绑定地址设置为127.0.0.1。

## 技术支持

如有问题，请：
1. 查看日志文件：`logs/server.log`
2. 运行测试脚本诊断问题
3. 提交Issue到项目仓库

## 更新日志

### v1.1.0 (2024-12-09)

**新增功能**
- STCP访问自动获取密钥
- 支持局域网访问（默认绑定0.0.0.0）

**改进**
- 简化STCP访问创建流程
- 提升用户体验

**修复**
- 无

**文档**
- 更新设计文档
- 添加升级指南
- 添加测试脚本
