# 构建阶段
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o stow-core ./cmd/server/

# 运行阶段
FROM gcr.io/distroless/static-debian12
COPY --from=builder /src/stow-core /stow-core
EXPOSE 8080
ENTRYPOINT ["/stow-core"]
