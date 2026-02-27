# Windows 端口保留问题分析与解决方案

## 问题现象

Desktop 客户端在 Windows 上启动 K8S Service 代理时，部分端口（如 PostgreSQL 的 5432）无法绑定，报错：

```
listen tcp 127.1.0.1:5432: bind: An attempt was made to access a socket in a way forbidden by its access permissions.
```

## 根本原因

### Windows 动态端口保留机制

Windows 的端口保留机制分为两类：

1. **动态端口范围保留**（Dynamic Port Range）
   - 用于出站连接的临时端口分配
   - Windows 10/11 默认范围：49152-65535
   - 可通过 `netsh int ipv4 set dynamicport` 调整

2. **排除端口范围**（Excluded Port Range）
   - 由系统组件（Hyper-V、WSL2、Docker、winnat）动态保留
   - 用于 NAT 网络转换、虚拟机网络等
   - **这是导致本问题的根本原因**

### 端口保留的作用范围

根据 [Microsoft 官方文档](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/reserve-a-range-of-ephemeral-ports)，Windows 的端口保留机制：

**端口保留是基于端口号的，不区分 IP 地址。**

这意味着：

- 如果端口 5432 被保留，则**所有 IP 地址**上的 5432 端口都无法绑定
- 包括 `0.0.0.0:5432`、`127.0.0.1:5432`、`127.1.0.1:5432`、物理网卡 IP 等
- 使用 `127.1.x.x` 等非标准 loopback 地址**无法绕过**端口保留限制

### 查看当前保留的端口

使用管理员权限执行：

```powershell
netsh interface ipv4 show excludedportrange protocol=tcp
```

输出示例：

```
Protocol tcp Port Exclusion Ranges

Start Port    End Port
----------    --------
      1024        1123
      4640        4739
      4865        4964
      5041        5140
      5141        5240
      5357        5357
      5426        5426
      5427        5526      # ← PostgreSQL 5432 在此范围内
      7681        7780
     50000       50059     *

* - Administered port exclusions.
```

### 谁保留了这些端口

根据社区分析（[SuperUser](https://superuser.com/questions/1579346)、[Benyamin Limanto 博客](https://blog.benyamin.xyz/2023/06/11/windows-hyper-v-reserved-ports-how-to-disable-it-partially/)）：

- **Windows NAT Driver (winnat)** 服务在启动时预留大量端口范围
- 用于支持 Hyper-V、WSL2、Docker Desktop、Windows 容器的 NAT 网络
- 保留的端口范围是动态分配的，每次系统启动或服务重启时可能变化
- 通常以 100 个端口为一组进行保留

### 实验验证

测试代码验证了端口保留的全局性：

```
测试未被保留的端口 8888：
✅ 绑定成功 127.0.0.1:8888
✅ 绑定成功 127.1.0.1:8888
✅ 绑定成功 0.0.0.0:8888

测试被保留的端口 5432：
❌ 绑定失败 127.0.0.1:5432
❌ 绑定失败 127.1.0.1:5432
❌ 绑定失败 0.0.0.0:5432
```

**结论**：Windows 端口保留是基于端口号的，与 IP 地址无关。使用 `127.1.x.x` 网段无法绕过端口保留限制。

## 影响范围

### 常见被保留的服务端口

- **MySQL**: 3306
- **PostgreSQL**: 5432
- **Redis**: 6379
- **Kubernetes API Server**: 6443
- **MongoDB**: 27017
- **SQL Server**: 1433
- **其他自定义端口**

### 受影响的场景

1. 本地开发环境（MySQL, PostgreSQL 等数据库）
2. 微服务本地调试（多个服务监听不同端口）
3. **Desktop 客户端的 K8S Service 代理**（本项目场景）

## 解决方案

### 方案 1：永久排除特定端口（⭐ 强烈推荐）

为需要的端口添加管理员排除规则，这是最安全、最可靠的方案。

```powershell
# 以管理员身份运行 PowerShell

# 排除 MySQL 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=3306 numberofports=1

# 排除 PostgreSQL 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=5432 numberofports=1

# 排除 Redis 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=6379 numberofports=1

# 排除 Kubernetes API Server 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=6443 numberofports=1

# 排除 MongoDB 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=27017 numberofports=1
```

验证配置：

```powershell
netsh interface ipv4 show excludedportrange protocol=tcp
```

输出中会显示：

```
     3306        3306     *
     5432        5432     *
     6379        6379     *
     6443        6443     *
    27017       27017     *
* - Administered port exclusions.
```

**优点：**

- ✅ 永久生效，重启后仍然有效
- ✅ 精确控制，只影响指定端口
- ✅ 不改变系统默认行为
- ✅ 对其他应用无影响
- ✅ 适用于所有环境（开发、生产、企业）

**缺点：**

- ❌ 需要为每个端口单独配置
- ❌ 需要管理员权限

**适用场景：**

- 生产环境
- 企业域环境
- 需要稳定性的场景
- 所有推荐使用此方案

### 方案 2：临时释放端口（用于快速测试）

重启 winnat 服务会清除动态保留的端口，适合快速验证问题：

```powershell
# 以管理员身份运行
net stop winnat
net start winnat
```

**优点：**

- ✅ 立即生效
- ✅ 不修改系统配置

**缺点：**

- ❌ 临时方案，重启后可能再次被保留
- ❌ 会短暂中断 WSL2/Docker/Hyper-V 网络

**适用场景：**

- 快速验证端口冲突问题
- 临时测试
- 不推荐作为长期解决方案

### 方案 3：调整动态端口范围（⚠️ 需谨慎评估）

将动态端口起始位置设置为更高的值（如 49152），避开常用服务端口。

**警告：此方案可能影响系统稳定性，仅在充分评估后使用。**

```powershell
# 以管理员身份运行
netsh int ipv4 set dynamicport tcp start=49152 num=16384

# 重启电脑使配置生效
```

**优点：**

- ✅ 一次配置，避开所有常用端口
- ✅ 符合 IANA 标准（49152-65535 为动态端口范围）

**缺点和风险：**

- ❌ 需要重启系统
- ⚠️ 可能影响 Active Directory、Exchange 等企业服务
- ⚠️ 可能影响防火墙规则配置
- ⚠️ 可能影响 RPC 服务通信
- ⚠️ 可能影响某些 P2P 应用和 VPN 客户端

**使用前检查：**

```powershell
# 检查是否在域环境
systeminfo | findstr /B /C:"Domain"

# 检查是否运行企业服务
Get-Service | Where-Object {$_.Status -eq "Running"} | Select-Object Name, DisplayName
```

**适用场景：**

- 仅限个人开发机
- 不在企业域环境
- 没有运行 Active Directory、Exchange 等企业服务
- 愿意承担兼容性风险

**不推荐使用于：**

- 企业环境
- 域控制器或域成员机器
- 运行企业服务的服务器
- 生产环境

## Desktop 客户端的处理策略

### 架构限制

由于 Windows 端口保留是基于端口号的（不区分 IP 地址），Desktop 客户端使用 `127.1.x.x` 网段的设计**无法绕过端口保留限制**。

即使为每个域名分配不同的 VIP（如 `127.1.0.1`, `127.1.0.2`），如果服务端口（如 5432）被 Windows 保留，仍然无法在任何 IP 地址上绑定该端口。

### 当前实现

1. 尝试绑定服务端口（如 `127.1.0.1:5432`）
2. 如果失败，记录警告日志并提示用户
3. 不阻止程序运行，继续处理其他端口

### 代码示例

```go
if err := a.svcProxyMgr.StartSVCProxy(svcTarget); err != nil {
    log.Printf("[App] Warning: SVCProxy 启动失败 (%s:%d): %v", domain, port, err)
    log.Printf("[App] 提示: Windows 端口 %d 可能被系统保留，请使用 'netsh int ipv4 show excludedportrange protocol=tcp' 检查", port)
    // 不返回错误，继续处理其他端口
}
```

### 为什么不能使用不同端口

K8S Service 的端口是由集群管理员配置的，Desktop 客户端无法修改。例如：

- PostgreSQL 默认使用 5432
- MySQL 默认使用 3306
- Redis 默认使用 6379

客户端工具（如 `psql`, `mysql`, `redis-cli`）默认连接这些标准端口，修改端口会影响用户体验。

### 用户指引

在用户手册中添加以下内容：

**Windows 端口被保留的解决方法（推荐方案 1）：**

1. 以管理员身份打开 PowerShell
2. 执行以下命令永久排除端口：

```powershell
# 排除 MySQL 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=3306 numberofports=1

# 排除 PostgreSQL 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=5432 numberofports=1

# 排除 Redis 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=6379 numberofports=1

# 排除 Kubernetes API Server 端口
netsh int ipv4 add excludedportrange protocol=tcp startport=6443 numberofports=1
```

3. 重新启动 Desktop 客户端

**临时测试方法（方案 2）：**

如果只是临时测试，可以重启 winnat 服务：

```powershell
net stop winnat
net start winnat
```

注意：此方法重启后可能再次出现端口冲突。

## 参考资料

1. [Microsoft: Reserve a range of ephemeral ports](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/reserve-a-range-of-ephemeral-ports)
2. [SuperUser: Many excludedportranges how to delete](https://superuser.com/questions/1579346/many-excludedportranges-how-to-delete-hyper-v-is-disabled)
3. [Benyamin Limanto: Windows Hyper-V Reserved Ports](https://blog.benyamin.xyz/2023/06/11/windows-hyper-v-reserved-ports-how-to-disable-it-partially/)
4. [InterSystems: Docker Containers on Windows unable to get ports](https://community.intersystems.com/post/docker-containers-windows-sometimes-unable-get-ports-during-startup)

## 总结

Windows 端口保留问题是由 **winnat 服务**（支持 Hyper-V、WSL2、Docker）动态保留端口导致的。

**关键发现：**

1. 端口保留是**基于端口号的**，不区分 IP 地址
2. 使用 `127.1.x.x` 网段**无法绕过**端口保留限制
3. 唯一的解决方案是清除端口保留或调整动态端口范围

**对于 Desktop 客户端：**

- **推荐方案**：方案 1（永久排除端口）- 适用于所有环境
- **临时测试**：方案 2（重启 winnat）- 仅用于快速验证
- **不推荐**：方案 3（调整动态端口范围）- 可能影响系统稳定性
- **代码层面**：已添加友好的错误提示，不阻止程序运行

用户需要根据实际情况选择合适的解决方案，优先使用方案 1。
