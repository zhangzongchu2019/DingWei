# WorkPulse —— 多阶段构建；纯 Go(modernc.org/sqlite 无 cgo) → distroless 极小镜像（规范 §13.5）
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN mkdir -p /data
# CGO_ENABLED=0：纯静态二进制（modernc.org/sqlite 无需 cgo）
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/workpulse ./cmd/workpulse

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/workpulse /app/workpulse
COPY --from=build --chown=65532:65532 /data /data
# 数据落持久卷（SQLite 主库 + 月度归档），容器重启不丢
VOLUME ["/data"]
EXPOSE 8080
ENV WP_DB_PATH=/data/workpulse.db WP_DATA_DIR=/data WP_ADDR=:8080
ENTRYPOINT ["/app/workpulse"]
