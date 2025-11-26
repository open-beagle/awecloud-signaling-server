ARG BASE

FROM ${BASE}

ARG AUTHOR
ARG VERSION

LABEL author=${AUTHOR} version=${VERSION}

ARG TARGETOS
ARG TARGETARCH

COPY bin/agent-${TARGETOS}-${TARGETARCH} /usr/local/bin/agent

RUN set -ex && \
    apk add --no-cache ca-certificates && \
    mkdir -p /app/logs /app/config

WORKDIR /app

COPY config/agent.toml /app/config/

CMD ["agent", "-c", "/app/config/agent.toml"]
