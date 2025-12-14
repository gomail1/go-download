# 第一阶段：构建Go应用
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go.mod文件
COPY go.mod .

# 下载依赖
RUN go mod download

# 复制源代码
COPY main.go .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -o go-download-server main.go

# 第二阶段：创建最终镜像
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 安装必要的依赖
RUN apk --no-cache add ca-certificates tzdata

# 创建所需目录
RUN mkdir -p /app/downloads /app/pending /app/logs

# 从构建阶段复制应用程序
COPY --from=builder /app/go-download-server .

# 复制配置文件
COPY config.example.json /app/config.example.json
COPY config.json /app/config.json

# 设置环境变量
ENV TZ=Asia/Shanghai

# 暴露端口
EXPOSE 9980

# 运行应用
CMD ["./go-download-server"]