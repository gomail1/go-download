# 使用官方Go镜像作为构建环境
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod tidy

# 复制源代码
COPY . .

# 构建应用程序
RUN go build -o go-download-server ./

# 使用轻量级镜像作为运行环境
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 复制构建好的应用程序
COPY --from=builder /app/go-download-server ./

# 复制配置文件和启动脚本
COPY start.sh ./
COPY config.example.json ./config/config.example.json

# 创建必要的目录
RUN mkdir -p downloads pending logs ssl

# 设置可执行权限
RUN chmod +x start.sh

# 暴露端口
EXPOSE 9980 9443

# 启动应用程序
CMD ["./start.sh"]