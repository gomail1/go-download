# 使用轻量级镜像作为运行环境
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 直接复制本地构建好的应用程序
COPY go-download-server ./

# 复制static目录内容
COPY static ./static

# 创建必要的目录
RUN mkdir -p config downloads pending logs ssl

# 暴露端口
EXPOSE 9980 1443

# 启动应用程序
CMD ["./go-download-server"]