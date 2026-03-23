# Build go
FROM golang:1.25.0-alpine AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=0
RUN GOEXPERIMENT=jsonv2 go mod download
RUN GOEXPERIMENT=jsonv2 go build -v -o FNode -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor"

# Release
FROM  alpine
# 安装必要的工具包
RUN  apk --update --no-cache add tzdata ca-certificates \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
RUN mkdir /etc/FNode/
COPY --from=builder /app/FNode /usr/local/bin

ENTRYPOINT [ "FNode", "server", "--config", "/etc/FNode/config.json"]
