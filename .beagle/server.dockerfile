ARG BASE

FROM ${BASE}

ARG AUTHOR
ARG VERSION

LABEL author=${AUTHOR} version=${VERSION}

ARG TARGETOS
ARG TARGETARCH

COPY bin/server-${TARGETOS}-${TARGETARCH} /usr/local/bin/server

RUN set -ex && \
    apk add --no-cache ca-certificates sqlite && \
    mkdir -p /app/data /app/logs /app/certs /app/config

WORKDIR /app

COPY config/server.toml /app/config/

EXPOSE 7000 8080 8081

CMD ["server", "-c", "/app/config/server.toml"]
