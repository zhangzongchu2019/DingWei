# WorkPulse —— 多阶段构建；纯 Go(modernc.org/sqlite 无 cgo) → distroless 极小镜像（规范 §13.5）
FROM golang:1.22 AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN mkdir -p /data
ARG SOURCE_COMMIT
RUN test -n "$SOURCE_COMMIT" || (echo "SOURCE_COMMIT is required" >&2; exit 2)
# build.sh 是闸、来源注入和可复现 flags 的单一实现；python3 只存在于构建层。
RUN SOURCE_COMMIT="$SOURCE_COMMIT" ./build.sh /out
RUN actual="$(/out/workpulse version)" && test "${actual##* }" = "$SOURCE_COMMIT"

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_COMMIT
LABEL org.opencontainers.image.revision=$SOURCE_COMMIT
WORKDIR /app
COPY --from=build /out/workpulse /app/workpulse
COPY --from=build --chown=65532:65532 /data /data
# 数据落持久卷（SQLite 主库 + 月度归档），容器重启不丢
VOLUME ["/data"]
EXPOSE 8080
ENV WP_DB_PATH=/data/workpulse.db WP_DATA_DIR=/data WP_ADDR=:8080
ENTRYPOINT ["/app/workpulse"]
