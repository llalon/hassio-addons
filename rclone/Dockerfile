ARG BUILD_FROM=ghcr.io/home-assistant/base:3.24
FROM node:24-alpine3.24 AS webui

ARG RCLONE_WEB_VERSION=1.1.13

WORKDIR /webui
RUN apk add --no-cache curl tar \
    && curl -fsSL "https://codeload.github.com/rclone/rclone-web/tar.gz/refs/tags/${RCLONE_WEB_VERSION}" \
       | tar -xz --strip-components=1
COPY patch-webui.mjs /tmp/patch-webui.mjs
RUN npm ci \
    && node /tmp/patch-webui.mjs \
    && npm run build

FROM ${BUILD_FROM}

ARG BUILD_ARCH
ARG RCLONE_VERSION=1.76.0

ENV RCLONE_CONFIG=/config/rclone.conf

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

RUN mkdir -p /run/nginx /var/lib/nginx/tmp/client_body /var/lib/nginx/tmp/proxy \
    && chmod 0755 \
       /etc/cont-init.d/00-rclone \
       /etc/services.d/rclone/run \
       /etc/services.d/nginx/run
