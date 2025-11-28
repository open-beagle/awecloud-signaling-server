ARG BASE

FROM ${BASE}

ARG AUTHOR
ARG VERSION

LABEL author=${AUTHOR} version=${VERSION}

ARG TARGETOS
ARG TARGETARCH

# Create user and directories
RUN set -ex && \
    apk add --no-cache ca-certificates sqlite-libs && \
    addgroup -g 1000 code && \
    adduser -D -u 1000 -G code code && \
    mkdir -p /home/code/data /home/code/logs /home/code/certs /home/code/config && \
    chown -R code:code /home/code

# Copy binary
COPY --chown=code:code bin/server-${TARGETOS}-${TARGETARCH} /usr/local/bin/server

# Copy frontend dist files
COPY --chown=code:code web/dist /home/code/web/dist

# Copy example config as default config
COPY --chown=code:code config/server.toml.example /home/code/config/server.toml

# Switch to non-root user
USER code

WORKDIR /home/code

# Declare volumes for persistent data
VOLUME ["/home/code/data", "/home/code/logs"]

EXPOSE 7000 8080

CMD ["server", "-c", "/home/code/config/server.toml"]
