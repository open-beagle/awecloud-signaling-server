---
inclusion: manual
---

# OpenTelemetry 应用接入指南

## 快速开始

根据你的应用所在集群，选择对应的 Endpoint：

| 集群      | Endpoint                     | 集群标识 |
| --------- | ---------------------------- | -------- |
| 默认/本地 | `otel.example.com:443`       | default  |
| 项目A     | `otel.proja.example.com:443` | proja    |
| 项目B     | `otel.projb.example.com:443` | projb    |

## 环境变量配置

### 互联网/外网接入

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="https://otel.proja.example.com:443"
export OTEL_EXPORTER_OTLP_PROTOCOL="grpc"
export OTEL_SERVICE_NAME="your-service-name"
export OTEL_RESOURCE_ATTRIBUTES="service.namespace=your-namespace"
```

### K8s Pod 内部接入

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector.beagle-devops:4317"
  - name: OTEL_SERVICE_NAME
    value: "your-service-name"
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: "service.namespace=your-namespace"
```

## 各语言接入

### Java (自动注入)

1. 下载 Agent:

```bash
wget https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/download/v2.10.0/opentelemetry-javaagent.jar
```

2. 启动应用:

```bash
java -javaagent:opentelemetry-javaagent.jar -jar your-app.jar
```

Dockerfile 示例:

```dockerfile
ADD https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/download/v2.10.0/opentelemetry-javaagent.jar /opt/opentelemetry-javaagent.jar
ENV JAVA_OPTS="-javaagent:/opt/opentelemetry-javaagent.jar"
```

### Go

```bash
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
```

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
)
```

### Node.js

```bash
npm install @opentelemetry/auto-instrumentations-node
```

在应用入口最前面引入:

```javascript
require("@opentelemetry/auto-instrumentations-node/register");
```

### Python

```bash
pip install opentelemetry-distro opentelemetry-exporter-otlp
opentelemetry-bootstrap -a install
```

启动应用:

```bash
opentelemetry-instrument python app.py
```

## 查看 Trace

访问 Jaeger UI: <https://jaeger.example.com>

可通过 `k8s.cluster.name` tag 筛选不同集群的 trace 数据。

## 验证接入

发送测试 trace:

```bash
# 使用 otel-cli 测试
otel-cli span \
  --endpoint https://otel.proja.example.com:443 \
  --service "test-service" \
  --name "test-span" \
  --attrs "test.key=test-value"
```

或在应用中添加简单的 span 后访问 Jaeger 查看是否有数据。
