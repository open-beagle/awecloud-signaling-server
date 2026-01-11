ARG BASE=alpine:3

FROM ${BASE}

ARG AUTHOR=mengkzhaoyun@gmail.com
ARG VERSION=v0.1.2
ARG TARGETOS
ARG TARGETARCH

LABEL author=${AUTHOR} version=${VERSION}

# Create user and directories
RUN set -ex && \
    apk add --no-cache ca-certificates sqlite-libs sqlite && \
    addgroup -g 1000 code && \
    adduser -D -u 1000 -G code code && \
    mkdir -p /home/code/data /home/code/logs /home/code/certs /home/code/config && \
    chown -R code:code /home/code

# Copy binary
COPY --chown=code:code bin/server-${TARGETOS}-${TARGETARCH} /usr/local/bin/awecloud-signaling-server

# Copy frontend dist files
COPY --chown=code:code web/dist /home/code/web/dist

# Copy static config (design-time configuration)
# network.toml: Tailscale network segment planning, rarely changes
COPY --chown=code:code config/network.toml.example /home/code/config/network.toml

# Copy example config as default config (runtime configuration)
# server.toml: Can be overridden by ConfigMap or environment variables
COPY --chown=code:code config/server.toml.example /home/code/config/server.toml

# Switch to non-root user
USER code

WORKDIR /home/code

# Declare volumes for persistent data
VOLUME ["/home/code/data", "/home/code/logs"]

EXPOSE 7000 8080

CMD ["awecloud-signaling-server", "-c", "/home/code/config/server.toml"]
