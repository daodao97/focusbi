# ---- 前端构建: web/ (Vue3 + Vite 多页) -> web/dist ----
# 前端产物与架构无关, 固定在构建机原生平台跑 (避免多架构时重复/模拟编译)。
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

WORKDIR /build/web

# 安装 pnpm (pin 到与 pnpm-lock.yaml lockfileVersion 9.0 兼容的版本)
RUN corepack enable && corepack prepare pnpm@10.32.1 --activate

# 先复制 manifest 利用缓存层
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# 复制源码并构建 (产物在 web/dist)。
# docs/ 也要带进来 (放在相对 web/ 的 ../docs): vite 插件会把 docs/SYNTAX.md、docs/MCP.md
# 复制进 dist 作为应用内文档与 AI prompt 来源; 缺了它们前端文档抽屉会 404。
COPY docs/ /build/docs/
COPY web/ ./
RUN pnpm build

# ---- 后端构建: 内嵌 web/dist 进单二进制 ----
# 固定在构建机原生平台编译 (BUILDPLATFORM), 通过 GOARCH 交叉编译到目标平台 ——
# 比在 arm64 上跑模拟快很多。多架构由 buildx 注入 TARGETOS/TARGETARCH。
FROM --platform=$BUILDPLATFORM golang:1.25.7-alpine AS builder

ARG BUILD_VERSION
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 前端产物必须就位再编译: web/embed.go 用 //go:embed all:dist 内嵌
COPY --from=frontend-builder /build/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-s -w -X main.Version=${BUILD_VERSION}" \
    -trimpath \
    -o app ./cmd

# ---- 运行镜像 ----
FROM alpine:3.19 AS final

WORKDIR /app
COPY --from=builder /build/app /app/
# 携带配置文件 (conf.dev.yaml 等); 生产可用挂载覆盖
COPY *.yaml /app/

RUN apk update && \
    apk add --no-cache tzdata ca-certificates && \
    rm -rf /var/cache/apk/*

ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

ENTRYPOINT ["/app/app"]
