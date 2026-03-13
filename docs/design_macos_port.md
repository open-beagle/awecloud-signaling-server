# macOS Loopback 地址配置方案

## 问题背景

Desktop 应用使用 VIP（Virtual IP）机制，为每个服务域名分配 `127.1.x.x` 地址段的虚拟 IP，避免端口冲突。

在 Windows 和 Linux 上，整个 `127.0.0.0/8` 地址段默认可用。但 macOS 默认只有 `127.0.0.1` 可用，其他地址需要手动添加到 loopback 接口。

## 错误现象

```
监听 127.1.0.1:22 失败: listen tcp 127.1.0.1:22: bind: can't assign requested address
```

## 解决方案

### 方案 1：批量添加地址（开发测试用）

每次重启 macOS 后执行：

```bash
# 添加 127.1.0.1 - 127.1.0.254（约 30 秒）
for i in {1..254}; do
  sudo ifconfig lo0 alias 127.1.0.$i
done

# 验证
ifconfig lo0 | grep "inet 127.1" | wc -l
# 应该显示 254
```

**优点**：简单直接，支持最多 254 个并发服务

**缺点**：重启后失效，需要重新执行

### 方案 2：创建启动脚本（生产环境用）

创建 launchd 配置，系统启动时自动添加地址：

```bash
# 创建脚本
sudo tee /usr/local/bin/setup-signal-loopback.sh << 'EOF'
#!/bin/bash
for i in {1..254}; do
  ifconfig lo0 alias 127.1.0.$i 2>/dev/null
done
EOF

sudo chmod +x /usr/local/bin/setup-signal-loopback.sh

# 创建 launchd 配置
sudo tee /Library/LaunchDaemons/com.beagle.signal.loopback.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.beagle.signal.loopback</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/setup-signal-loopback.sh</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
</dict>
</plist>
EOF

# 加载并启动
sudo launchctl load /Library/LaunchDaemons/com.beagle.signal.loopback.plist
sudo launchctl start com.beagle.signal.loopback
```

**优点**：重启后自动生效，一次配置永久有效

**缺点**：配置稍复杂

## DNS 配置

除了 loopback 地址，macOS 还需要配置 DNS 解析：

```bash
# 创建 /etc/resolver/beagle（需要 sudo）
sudo mkdir -p /etc/resolver
sudo tee /etc/resolver/beagle << 'EOF'
nameserver 127.0.0.1
port 15353
EOF
```

这个配置是持久的，重启后不会丢失。

## 清理配置

### 删除 loopback alias

```bash
# 删除单个地址
sudo ifconfig lo0 -alias 127.1.0.1

# 批量删除
for i in {1..254}; do
  sudo ifconfig lo0 -alias 127.1.0.$i 2>/dev/null
done
```

### 删除 launchd 配置

```bash
sudo launchctl unload /Library/LaunchDaemons/com.beagle.signal.loopback.plist
sudo rm /Library/LaunchDaemons/com.beagle.signal.loopback.plist
sudo rm /usr/local/bin/setup-signal-loopback.sh
```

### 删除 DNS 配置

```bash
sudo rm /etc/resolver/beagle
```

## 核心原则

**VIP 机制不可改变**：

- Windows/Linux/macOS 统一使用 `127.1.x.x` VIP 地址段
- 保持跨平台一致的架构设计
- macOS 通过配置 loopback 地址来适配 VIP 机制
- 不因 macOS 的特殊性而改变整体架构
