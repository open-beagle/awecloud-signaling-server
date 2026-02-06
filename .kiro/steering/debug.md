# 调试指南

**AI 必须遵守：编译、构建、测试时必须使用本文档中的命令，不得自行编写命令。**

**如果需要执行本文档未包含的编译、构建、测试相关命令，AI 必须先询问用户，获得批准后再将新命令添加到本文档。**

## 编译命令

使用 `version` 文件记录版本号。

构建时读取：

```bash
# 生成 Protocol Buffers 代码
bash scripts/generate_proto.sh

# 生成 Desktop Protocol Buffers 代码
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative desktop/pkg/proto/desktop.proto

# Server&Agent编译
BUILD_VERSION=$(cat version) bash scripts/build.sh

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
BUILD_VERSION=$(cat version) bash scripts/build.sh

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
sleep 3 && kubectl --context aliyun --namespace beagle-access rollout restart deployment/awecloud-signal-server
```

## 容器调试

### 进入容器

```bash
kubectl --context aliyun -n beagle-access exec -it deployment/awecloud-signal-server -- /bin/sh
```

### 查看日志

```bash
kubectl --context aliyun -n beagle-access logs -f deployment/awecloud-signal-server
```

## 数据库位置

### Server 数据库

- Kubernetes：`~/data/server.db`
