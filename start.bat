@echo off
chcp 65001 >nul

echo 🚀 Go下载站启动脚本 (Windows)
echo =======================

REM 检查Go是否已安装
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo ❌ 错误: 未检测到Go语言环境
    echo 请先安装Go语言: https://golang.org/dl/
    pause
    exit /b 1
)

echo ✅ Go语言环境检测成功

REM 初始化Go模块（如果需要）
if not exist "go.sum" (
    echo 📦 初始化Go模块...
    go mod tidy
)

REM 创建必要的目录
echo 📁 创建目录结构...
if not exist "downloads" mkdir downloads
if not exist "uploads" mkdir uploads
if not exist "static" mkdir static
if not exist "pending" mkdir pending
if not exist "logs" mkdir logs

REM 检查配置文件
if not exist "config.json" (
    if exist "config.example.json" (
        echo 📄 配置文件不存在，从示例文件复制...
        copy config.example.json config.json
        echo ✅ 已创建默认配置文件，请根据需要修改config.json
    ) else (
        echo ❌ 错误: 未找到config.json和config.example.json文件
        pause
        exit /b 1
    )
) else (
    echo ✅ 配置文件检查成功
)

echo 🌐 启动服务器...
echo 访问地址: http://localhost:8080
echo 按 Ctrl+C 停止服务器
echo =======================

REM 启动服务器
go run main.go

pause