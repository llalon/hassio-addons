ARG BUILD_FROM=ghcr.io/home-assistant/base:3.24
FROM node:24-alpine3.24 AS webui

WORKDIR /webui
# Fork of rclone/rclone-web with Home Assistant ingress support (branch: hassio),
# vendored as the git submodule at rclone/webui.
COPY webui/package.json webui/package-lock.json ./
RUN npm ci
COPY webui/ ./
RUN npm run build

FROM golang:1.26-alpine3.24 AS mqttbridge

WORKDIR /src
COPY mqtt-bridge/go.mod mqtt-bridge/go.sum ./
RUN go mod download
COPY mqtt-bridge/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mqtt-bridge .

FROM ${BUILD_FROM}

ARG BUILD_ARCH
ARG RCLONE_VERSION=1.76.0

RUN apk add --no-cache nginx curl unzip \
    && case "${BUILD_ARCH}" in \
         amd64) RCLONE_ARCH=amd64 ;; \
         *) echo "Unsupported architecture: ${BUILD_ARCH}" >&2; exit 1 ;; \
       esac \
    && ARCHIVE="rclone-v${RCLONE_VERSION}-linux-${RCLONE_ARCH}.zip" \
    && curl -fsSLO "https://downloads.rclone.org/v${RCLONE_VERSION}/${ARCHIVE}" \
    && curl -fsSLO "https://downloads.rclone.org/v${RCLONE_VERSION}/SHA256SUMS" \
    && grep " ${ARCHIVE}$" SHA256SUMS | sha256sum -c - \
    && unzip "${ARCHIVE}" "rclone-v${RCLONE_VERSION}-linux-${RCLONE_ARCH}/rclone" \
    && install -m 0755 "rclone-v${RCLONE_VERSION}-linux-${RCLONE_ARCH}/rclone" /usr/local/bin/rclone \
    && rm -rf "${ARCHIVE}" SHA256SUMS "rclone-v${RCLONE_VERSION}-linux-${RCLONE_ARCH}"

COPY rootfs /
COPY --from=webui /webui/dist /usr/share/nginx/html
COPY --from=mqttbridge /mqtt-bridge /usr/local/bin/mqtt-bridge

RUN mkdir -p /run/nginx /var/lib/nginx/tmp/client_body /var/lib/nginx/tmp/proxy \
    && chmod 0755 \
       /etc/cont-init.d/00-rclone \
       /etc/services.d/rclone/run \
       /etc/services.d/nginx/run \
       /etc/services.d/mqtt-bridge/run
