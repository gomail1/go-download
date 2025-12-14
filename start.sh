#!/bin/bash

# Go下载站启动脚本
# 适用于Linux/macOS系统

echo "🚀 Go下载站启动脚本"
echo "======================="

# 检查Go是否已安装
if ! command -v go &> /dev/null; then
    echo "❌ 错误: 未检测到Go语言环境"
    echo "请先安装Go语言: https://golang.org/dl/"
    exit 1
fi

echo "✅ Go语言环境检测成功"

# 初始化Go模块（如果需要）
if [ ! -f "go.sum" ]; then
    echo "📦 初始化Go模块..."
    go mod tidy
fi

# 创建必要的目录
echo "📁 创建目录结构..."
mkdir -p downloads uploads static pending logs

# 检查配置文件
if [ ! -f "config.json" ]; then
    if [ -f "config.example.json" ]; then
        echo "📄 配置文件不存在，从示例文件复制..."
        cp config.example.json config.json
        echo "✅ 已创建默认配置文件，请根据需要修改config.json"
    else
        echo "❌ 错误: 未找到config.json和config.example.json文件"
        exit 1
    fi
else
    echo "✅ 配置文件检查成功"
fi

echo "🌐 启动服务器..."
echo "访问地址: http://localhost:8080"
echo "按 Ctrl+C 停止服务器"
echo "======================="

# 启动服务器
go run main.go