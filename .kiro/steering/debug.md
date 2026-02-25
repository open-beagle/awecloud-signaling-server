# 调试指南

**AI 必须遵守：编译、构建、测试时必须使用本文档中的命令，不得自行编写命令。**

**如果需要执行本文档未包含的编译、构建、测试相关命令，AI 必须先询问用户，获得批准后再将新命令添加到本文档。**

## 版本管理规范

**严格禁止 AI 未经用户明确批准私自升级版本号或发布新版本。**

版本号修改和发布流程必须遵循：

1. **版本号修改**：AI 不得修改 `version` 或 `desktop/version` 文件，除非用户明确要求
2. **编译发布**：AI 不得执行 `scripts/push_to_s3.sh` 发布到 S3，除非用户明确批准
3. **镜像发布**：AI 不得执行 Docker 镜像构建和推送命令，除非用户明确批准
4. **节点升级**：AI 不得在远程节点上执行升级命令，除非用户明确要求

如果 AI 认为需要升级版本，必须：

- 先向用户说明原因和影响
- 等待用户明确批准
- 获得批准后才能执行相关操作

## 节点与环境信息

详细的节点信息（IP、域名注册表、端口分配、安装/升级命令、Token）见 `.tmp/debug.md`。

### 关键架构速查

- Agent（beijing）：beagle-242 节点，应使用 Service 部署（需要 SSH + Endpoint 全功能），Endpoint gRPC Server 监听 0.0.0.0:50052
- Endpoint（beagle-241）：systemd 二进制部署，连接 beagle-242 的 Agent 50052 端口
- Endpoint SSH/K8SAPI 代理使用 tsnet FallbackTCPHandler（不是物理端口监听，只在 Tailscale 网络可达）

## 编译命令

使用 `version` 文件记录版本号。

构建时读取：

```bash
# 生成 Protocol Buffers 代码
bash scripts/generate_proto.sh

# 生成 Desktop Protocol Buffers 代码
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative desktop/pkg/proto/desktop.proto

# Server&Agent&Endpoint 全量编译
BUILD_VERSION=$(cat version) bash scripts/build.sh

# 单独编译 Endpoint
BUILD_VERSION=$(cat version) bash scripts/build.sh endpoint

# Endpoint 多架构编译
GOARCHS=amd64,arm64 BUILD_VERSION=$(cat version) bash scripts/build.sh endpoint

# Web编译
BUILD_VERSION=$(cat version) bash scripts/build_frontend.sh

# Desktop编译
BUILD_VERSION=$(cat desktop/version) bash scripts/build_desktop.sh
```

## 发布命令

### Server

```bash
# 构建前端代码
BUILD_VERSION=$(cat version) bash scripts/build_frontend.sh

# 构建Server代码
BUILD_VERSION=$(cat version) bash scripts/build.sh server

# 设置版本和镜像仓库
export BUILD_VERSION=$(cat version)
export REGISTRY=registry.cn-qingdao.aliyuncs.com/wod
export AUTHOR=open-beagle

# 构建 Server 镜像
docker build --no-cache -f .beagle/server.dockerfile \
  --build-arg BASE=${REGISTRY}/alpine:3 \
  --build-arg AUTHOR=${AUTHOR} \
  --build-arg VERSION=${BUILD_VERSION} \
  -t ${REGISTRY}/awecloud-signaling-server:${BUILD_VERSION} \
  . && \
docker push ${REGISTRY}/awecloud-signaling-server:${BUILD_VERSION}

# 重启部署
sleep 3 && kubectl --context aliyun --namespace beagle-access rollout restart deployment/signal-server
```

### Agent

#### 容器

```bash
# 构建 Agent 代码
BUILD_VERSION=$(cat version) bash scripts/build.sh agent

# 设置版本和镜像仓库
export BUILD_VERSION=$(cat version)
export REGISTRY=registry.cn-qingdao.aliyuncs.com/wod
export AUTHOR=open-beagle

# 构建 Agent 镜像
docker build --no-cache -f .beagle/agent.dockerfile \
  --build-arg BASE=${REGISTRY}/alpine:3 \
  --build-arg AUTHOR=${AUTHOR} \
  --build-arg VERSION=${BUILD_VERSION} \
  -t ${REGISTRY}/awecloud-signaling-agent:${BUILD_VERSION} \
  . && \
docker push ${REGISTRY}/awecloud-signaling-agent:${BUILD_VERSION}

# 重启部署
sleep 3 && kubectl --context beagle --namespace beagle-access rollout restart deployment/signal-agent
```

#### 二进制

```bash
# 1. 构建 Agent（多架构，Agent 不需要 CGO 可直接交叉编译）
GOARCHS=amd64,arm64 BUILD_VERSION=$(cat version) bash scripts/build.sh agent

# 2. 发布到 S3（只上传 Agent 二进制及安装脚本）
BUILD_VERSION=$(cat version) bash scripts/push_to_s3.sh agent
```

### Endpoint

```bash
# 1. 构建 Endpoint（多架构）
GOARCHS=amd64,arm64 BUILD_VERSION=$(cat version) bash scripts/build.sh endpoint

# 2. 发布到 S3（只上传 Endpoint 二进制及安装脚本）
BUILD_VERSION=$(cat version) bash scripts/push_to_s3.sh endpoint
```

## 调试命令

### Server 与 Agent 容器

```bash
# Server
kubectl --context aliyun -n beagle-access exec -it deployment/signal-server -- /bin/sh

# Agent
kubectl --context beagle -n beagle-access exec -it deployment/signal-agent -- /bin/sh
```

### 本地 CloudIDE 调试

```bash
# 停止 Agent
sudo pkill -f signal_agent

# 编译 Agent
BUILD_VERSION=$(cat version) bash scripts/build.sh agent

# 启动 Agent（使用环境变量）
mkdir -p $HOME/.local/share/signal/logs $HOME/.local/share/signal/tunnel
nohup sudo env SIGNAL_TOKEN=$SIGNAL_TOKEN SIGNAL_SERVER=$SIGNAL_SERVER SIGNAL_STATE_DIR=$HOME/.local/share/signal/tunnel SIGNAL_LOG_LEVEL=debug ./bin/signal_agent > $HOME/.local/share/signal/logs/agent.log 2>&1 &

# 查看日志
tail -f $HOME/.local/share/signal/logs/agent.log

# 检查进程
ps aux | grep signal_agent | grep -v grep

# 检查 DNS 配置
sudo cat /etc/resolv.conf

# 检查 SSH 配置
cat ~/.ssh/config

# 测试 DNS 解析
nslookup beagle-242.beijing.beagle 127.0.0.1

# 测试 SSH 连接
ssh beagle-242.beijing.beagle
```

### 节点 SSH

见 `.tmp/debug.md` 中的具体 IP 和端口。

## 数据库位置

### Server 数据库

- Kubernetes：`/app/data/server.db`
