ARG BASE

FROM ${BASE}

ARG AUTHOR
ARG VERSION
ARG TARGETOS
ARG TARGETARCH

LABEL author=${AUTHOR} version=${VERSION}

# Create user and directories
RUN set -ex && \
    apk add --no-cache ca-certificates && \
    addgroup -g 1000 code && \
    adduser -D -u 1000 -G code code && \
    mkdir -p /home/code/logs /home/code/config /home/code/.config/awecloud-signaling && \
    chown -R code:code /home/code

# Copy binary
COPY --chown=code:code bin/agent-${TARGETOS}-${TARGETARCH} /usr/local/bin/agent

# Copy example config as default config
COPY --chown=code:code config/agent.toml.example /home/code/config/agent.toml

# Switch to non-root user
USER code

WORKDIR /home/code

# Declare volume for logs
VOLUME ["/home/code/logs"]

# Declare volume for state (optional, for persistent state)
VOLUME ["/home/code/.config/awecloud-signaling"]

CMD ["agent", "-c", "/home/code/config/agent.toml"]
