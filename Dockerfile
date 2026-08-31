# 使用官方Go镜像作为构建环境
FROM golang:1.27-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 设置Go模块代理
ENV GOPROXY=https://goproxy.cn,direct

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用程序（禁用CGO，减小体积，启用优化）
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o go-download-server ./

# 使用轻量级镜像作为运行环境
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 更新系统包，修复漏洞，安装必要的运行时依赖
RUN apk update && apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

# 复制构建好的应用程序
COPY --from=builder /app/go-download-server ./

# 复制static目录内容
COPY static ./static

# 注意：所有运行时目录（config, downloads, pending, logs, ssl, data, config/icons/cache）
# 都会由代码自动创建（os.MkdirAll），无需在这里创建

# 设置环境变量
ENV TZ=Asia/Shanghai

# 暴露端口
EXPOSE 9980 1443

# 启动应用程序
CMD ["./go-download-server"]