# 构建和部署说明

## 目录结构

```
.beagle/
├── server.dockerfile        # Server Dockerfile
├── agent.dockerfile         # Agent Dockerfile
├── build.sh                 # CI/CD 构建脚本（使用 xx-go 跨架构编译）
└── README.md                # 本文档

scripts/
└── build.sh                 # 本地开发构建脚本
```

## 本地构建

### 方式一：直接编译（推荐用于本地开发）

适用于 Ubuntu/Debian 等 glibc 系统的本地开发：

```bash
# 1. 构建前端
bash scripts/build_frontend.sh

# 2. 构建后端（当前架构）
bash scripts/build.sh

# 输出在 bin/ 目录
# bin/server-linux-amd64
# bin/agent-linux-amd64
# bin/server -> server-linux-amd64 (符号链接)
# bin/agent -> agent-linux-amd64 (符号链接)
```

### 方式二：Docker 容器编译（用于生产部署）

使用 Alpine 容器编译，生成 musl libc 版本（适合 Docker 部署）：

```bash
# 构建前端代码
BUILD_VERSION=v0.2.3 bash scripts/build_frontend.sh

# 构建后端代码（在 Alpine 容器中）
docker run --rm \
   -v $(pwd):/go/src/github.com/open-beagle/awecloud-signaling-server \
   -v $HOME/go/pkg:/go/pkg \
   -w /go/src/github.com/open-beagle/awecloud-signaling-server \
   -e BUILD_VERSION=v0.2.3 \
   -e GOARCHS=amd64 \
   registry.cn-qingdao.aliyuncs.com/wod/golang:1.25-alpine \
   bash ./.beagle/build.sh

# 注意：此方式编译的二进制需要 musl libc，不能直接在 Ubuntu 上运行
# 仅用于构建 Docker 镜像
```

### 查看版本信息

```bash
# Server 版本
./bin/server -v

# Agent 版本
./bin/agent -v

# 输出示例：
# AWECloud Signaling Server
# Version:    v0.2.3
# Git Commit: abc1234
# Build Date: 2025-11-26_10:30:00
```

### 2. 构建 Docker 镜像

**前提**：

- 已使用**方式二（Docker 容器编译）**完成二进制文件构建
- 已完成前端构建（`bash scripts/build_frontend.sh`）

```bash
# 设置版本和镜像仓库
export BUILD_VERSION=v0.2.3
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

# 构建 Agent 镜像
docker build --no-cache -f .beagle/agent.dockerfile \
  --build-arg BASE=${REGISTRY}/alpine:3 \
  --build-arg AUTHOR=${AUTHOR} \
  --build-arg VERSION=${BUILD_VERSION} \
  -t ${REGISTRY}/awecloud-signaling-agent:${BUILD_VERSION} \
  . && \
docker push ${REGISTRY}/awecloud-signaling-agent:${BUILD_VERSION}
```

### 3. 部署到 Kubernetes

推送镜像后，更新 Kubernetes 部署：

```bash
# 重启部署
kubectl --context aliyun --namespace beagle-access rollout restart deployment/awecloud-signal-server

# 更新镜像
kubectl --context aliyun --namespace beagle-access set image deployment/awecloud-signal-server server=${REGISTRY}/awecloud-signaling-server:${BUILD_VERSION}

# 查看日志
kubectl --context aliyun --namespace beagle-access logs -f deployment/awecloud-signal-server
```

## GitHub Actions 自动构建

### 触发方式

1. **自动触发**：推送到 `dev` 或 `main` 分支
2. **手动触发**：在 GitHub Actions 页面手动运行

### 构建流程

1. 使用 `golang:1.25-alpine` 镜像和 `xx-go` 工具交叉编译（amd64 + arm64）
   - Server: CGO_ENABLED=1（支持 SQLite）
   - Agent: CGO_ENABLED=0（纯 Go）
2. 构建 Docker 镜像（amd64 + arm64）
3. 创建 multi-arch manifest
4. 推送到阿里云容器镜像服务

### 镜像标签

- Server: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v0.2.3`
- Agent: `registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-agent:v0.2.3`

## 架构支持

- **amd64**: x86_64 架构
- **arm64**: ARM64 架构（如 Apple Silicon）

## 镜像说明

### Server 镜像

- 基础镜像：Alpine Linux
- 包含：ca-certificates, sqlite
- 暴露端口：7000, 8080, 8081
- 工作目录：/app
- 配置文件：/app/config/server.toml

### Agent 镜像

- 基础镜像：Alpine Linux
- 包含：ca-certificates
- 工作目录：/app
- 配置文件：/app/config/agent.toml

## 版本管理

版本号格式：`vX.Y.Z`

- X: 主版本号（重大变更）
- Y: 次版本号（功能增加）
- Z: 修订号（bug 修复）

当前版本：`v0.2.3`

## Agent 发布到 S3

用于发布 Agent 二进制和安装脚本到 S3 存储：

```bash
# 1. 构建 Agent（多架构，Agent 不需要 CGO 可直接交叉编译）
GOARCHS=amd64,arm64 BUILD_VERSION=v0.2.3 bash scripts/build.sh agent

# 2. 发布到 S3
BUILD_VERSION=v0.2.3 bash scripts/publish_agent_s3.sh
```

发布内容：

- `agent-v0.2.3-linux-amd64` - Agent 二进制（amd64）
- `agent-v0.2.3-linux-arm64` - Agent 二进制（arm64）
- `install.sh` - 一键安装脚本
- `agent-version.json` - 版本信息

注意：Server 需要 CGO（SQLite），本地无法交叉编译 arm64，需要使用 Docker 容器或 CI/CD。

## 注意事项

1. **CGO**：
   - Server 需要 CGO_ENABLED=1（使用 mattn/go-sqlite3）
   - Agent 不需要 CGO（纯 Go）
2. **跨架构编译**：
   - 本地开发：使用 `scripts/build.sh`（标准 Go 交叉编译）
   - CI/CD：使用 `.beagle/build.sh`（xx-go 工具，支持 CGO 跨架构）
3. **构建时间**：交叉编译需要几分钟
4. **镜像大小**：
   - Server: ~50MB
   - Agent: ~30MB
5. **权限**：确保脚本有执行权限（`chmod +x`）

## 故障排查

### 构建失败

```bash
# 检查 Go 版本
go version

# 检查依赖
go mod tidy

# 清理缓存
go clean -cache
```

### Docker 构建失败

```bash
# 检查 Docker 版本
docker version

# 清理构建缓存
docker builder prune

# 查看构建日志
docker build --progress=plain ...
```

### 推送失败

```bash
# 检查登录状态
docker login registry.cn-qingdao.aliyuncs.com

# 检查镜像标签
docker images | grep awecloud-signaling

# 手动推送
docker push <image:tag>
```
