ARG BASE=alpine:3

FROM ${BASE}

ARG AUTHOR=mengkzhaoyun@gmail.com
ARG VERSION
ARG TARGETOS
ARG TARGETARCH

LABEL author=${AUTHOR} version=${VERSION}

RUN set -ex && \
    apk add --no-cache ca-certificates sqlite-libs sqlite && \
    addgroup -g 1000 code && \
    adduser -D -u 1000 -G code code && \
    mkdir -p /app/data /app/web /app/logs && \
    chown -R code:code /app

# 二进制
COPY --chown=code:code bin/signal_server-${TARGETOS}-${TARGETARCH} /usr/local/bin/signal_server

# 前端静态文件
COPY --chown=code:code web/dist /app/web

# 默认配置（可被 ConfigMap 覆盖）
COPY --chown=code:code config/server.toml.example /app/config/server.toml
COPY --chown=code:code config/network.toml.example /app/config/network.toml

USER code
WORKDIR /app

ENV SIGNAL_WEB_ROOT=./web

VOLUME ["/app/data"]

EXPOSE 8080

CMD ["signal_server", "-c", "./config/server.toml"]
