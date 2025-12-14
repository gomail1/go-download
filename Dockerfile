# 使用Go 1.21作为构建基础镜像
FROM golang:1.21-alpine AS builder

# 安装git
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 从GitHub拉取代码
RUN git clone https://github.com/gomail1/go-download.git .

# 下载依赖
RUN go mod tidy

# 构建Go应用，使用静态链接
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o go-download-server main.go

# 使用Alpine作为最终运行镜像
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 复制构建好的二进制文件
COPY --from=builder /app/go-download-server .

# 复制配置文件
COPY --from=builder /app/config.example.json .

# 创建必要的目录
RUN mkdir -p downloads pending logs

# 设置执行权限
RUN chmod +x go-download-server

# 暴露端口
EXPOSE 9980

# 启动命令
CMD ["/app/go-download-server"]