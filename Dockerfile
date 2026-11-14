# syntax=docker/dockerfile:1
# 多阶段构建 → scratch 最终层，镜像体积 = 二进制本体（约 12–13 MB，amd64）
#
# 构建阶段固定运行在宿主架构（$BUILDPLATFORM），Go 原生交叉编译到 $TARGETARCH，
# 避免在 QEMU 模拟下编译 ARM 版本导致的长时间等待。
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src

# 依赖层缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -tags netgo,osusergo \
    -ldflags "-s -w" \
    -o /out/port-ledger .

FROM scratch

ARG VERSION=dev
LABEL org.opencontainers.image.title="port-ledger" \
      org.opencontainers.image.description="群晖 Docker 端口盘点与用途管理工具" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"

# 数据、日志、备份统一落在 /data，由 compose 映射到群晖 volume
WORKDIR /data
COPY --from=build /out/port-ledger /port-ledger

ENV TZ=Asia/Shanghai \
    DATA_DIR=/data \
    ADMIN_PORT=443 \
    SCAN_HOST_PROC=true \
    SCAN_DOCKER_SOCKET=true \
    SCAN_COMPOSE_DIR=/compose \
    SCAN_RANGES=1-65535 \
    SCAN_ON_START=true \
    SCAN_INTERVAL=300 \
    LOG_LEVEL=info

# 容器内固定监听 443；宿主/外网端口由使用方自行映射
EXPOSE 443
VOLUME ["/data", "/compose"]

# scratch 内无 shell 与 curl，健康探针由二进制自身完成
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/port-ledger", "-health"]

ENTRYPOINT ["/port-ledger"]
