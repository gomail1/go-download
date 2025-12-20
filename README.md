# Go HTTP服务器下载站-v0.0.5 gomail1

使用Go语言开发的高性能文件下载站，提供文件上传、下载、浏览、审核和管理功能，支持基于角色的用户权限控制。

**GitHub仓库链接**: [https://github.com/gomail1/go-download](https://github.com/gomail1/go-download)
**Docker仓库链接**: [https://hub.docker.com/r/gomail1/go_downloader](https://hub.docker.com/r/gomail1/go_downloader)

##  最新更新

### v0.0.5 版本内容

#### 🚀 核心功能优化
- ✅ **日志系统彻底重构**：实现智能日志级别分类系统，支持success、error、warning、debug和info五种日志级别
- ✅ **性能与稳定性优化**：优化文件处理和数据查询逻辑，提高系统响应速度和稳定性
- ✅ **安全与权限增强**：改进权限验证逻辑，加强对敏感操作的访问控制
- ✅ **用户体验改进**：添加日志筛选功能，优化错误提示和界面交互
- ✅ **统计系统升级**：新增下载统计、带宽监控和热力图功能，提供更全面的系统监控

#### 🎯 最新功能更新
- ✅ **搜索栏支持**：添加文件搜索功能，方便用户快速查找文件
- ✅ **目录递归**：实现目录递归功能，支持多级目录的完整展示和操作
- ✅ **删除按钮优化**：移除单个删除按钮，优化文件管理界面
- ✅ **分享功能**：在下载按钮旁新增分享按钮，支持生成静态URL分享链接
- ✅ **独立统计模块**：新增独立统计模块，支持统计分享次数、下载次数和热力图展示
- ✅ **日志优化**：进一步优化日志记录和展示，提高系统可维护性
- ✅ **多级管理员系统**：新增二级管理员角色，实现更细粒度的权限控制
- ✅ **超级管理员保护**：默认超级管理员账号不可删除，对二级管理员隐藏
- ✅ **用户权限细化**：普通用户默认无删除文件/修改文件权限
- ✅ **权限描述修复**：修正管理员服务器页面中有关角色权限的描述

### 历史版本概述

- **v0.0.4**：实现每日上传限制功能、优化错误处理机制、改进界面布局
- **v0.0.3**：添加管理员批量操作功能、增强文件上传体验、优化界面设计
- **v0.0.2**：优化日志系统、增强服务器信息页面、改进项目结构
- **v0.0.1**：实现基础功能、用户角色系统、文件审核机制

## 🎯 主要功能特性

### 🏗️ 基础架构
- 🚀 **高性能**: 基于Go语言的HTTP服务器，高并发处理能力
- 🎨 **响应式设计**: 适配各种设备的现代Web界面
- 🔒 **安全防护**: 文件名清理，防止路径遍历攻击
- 📝 **配置文件**: 基于JSON的用户和服务器配置管理
- 🔒 **HTTPS支持**: 支持HTTPS安全访问，可配置HTTPS端口和SSL证书
- ⚡ **双协议支持**: 同时支持HTTP和HTTPS访问，灵活选择

### 📦 文件管理
- 📁 **文件浏览**: 美观的Web界面浏览可下载文件，支持目录导航
- ⬆️ **文件上传**: 支持选择上传目录，多用户角色权限控制
- 🗑️ **文件管理**: 在线删除文件，文件信息查看，目录创建和管理
- ✅ **文件审核**: 普通用户上传的文件需要管理员审核
- 📁 **目录选择**: 上传和审核时可选择目标目录
- 🔒 **待审核文件隔离**: 普通用户上传的待审核文件进行隔离

### 👥 用户系统
- 👤 **多级用户角色**: 支持超级管理员、二级管理员和普通用户三种角色
- 👥 **细粒度权限控制**:
  - **超级管理员(admin)**: 拥有全部权限，不可删除，对二级管理员隐藏
  - **二级管理员**: 拥有管理员权限，但不能查看日志、服务器信息和热力图，可管理普通用户
  - **普通用户**: 可上传下载文件，默认无删除和修改权限
- 👥 **用户管理**: 管理员可添加、删除用户，配置角色和权限
- 🔧 **密码管理**: 支持修改用户密码
- 📊 **用户列表**: 表格形式展示用户信息，方便管理
- 🔒 **管理员保护**: 超级管理员账号默认不可删除，二级管理员不可创建或修改超级管理员

### 📊 统计与监控
- 📊 **统计信息**: 服务器运行状态、文件统计等
- 📊 **服务器信息**: 详细的系统信息、配置信息和运行状态
- 📋 **服务器日志**: 结构化日志记录，支持搜索和筛选功能
- 📝 **结构化日志**: 包含时间戳、级别、用户名、角色、操作和详情
- 🔍 **日志搜索**: 支持日志内容搜索和级别筛选
- 📋 **日志统计**: 显示总日志数和当前可见日志数
- 📈 **下载统计**: 统计每个文件的下载次数和总下载量
- 📊 **带宽监控**: 实时监控和统计服务器总流量消耗
- 🌍 **地理热力图**: 基于用户IP的地理分布热力图，可视化用户访问情况
- 📊 **文件统计展示**: 在文件列表中直观显示每个文件的下载次数和流量消耗

### 💡 其他功能
- 📈 **实时更新**: 文件列表实时刷新，搜索过滤功能
- ⚠️ **消息提醒**: 5秒后自动消失的消息提醒

## 安全特性

- 基于容器的隔离运行环境
- 支持HTTPS安全访问
- 基于角色的权限控制
- 最大文件大小限制
- 完整的操作日志记录
- 文件名清理和验证
- 路径遍历攻击防护


## 项目结构

本项目仅提供Docker部署方案，仓库已经删除可执行的源代码文件。

```
go-download-server/
├── Dockerfile           # Docker构建文件
├── docker-compose.yml   # Docker Compose配置
├── LICENSE              # 许可证文件
├── README.md            # 项目说明文档
├── config/              # 配置目录
│   ├── config.go        # 配置加载代码
│   ├── config.json      # 主配置文件
│   ├── daily_upload.json # 每日上传限制配置
│   └── stats.json       # 统计数据配置
├── .github/workflows/   # GitHub Actions工作流
└── 图片说明/            # 功能界面截图
```



## 部署指南

### 常规Docker部署

使用Docker Compose可以更方便地管理和部署应用。创建`docker-compose.yml`文件，内容如下：

```yaml
version: '3.8'
services:
  go-download-server:
    # Docker Hub镜像
    image: gomail1/go_downloader:latest
    # 备选镜像源：GitHub Container Registry
    # image: ghcr.io/gomail1/go-download:latest
    container_name: go-download-server
    restart: unless-stopped
    ports:
      - "9980:9980"
      - "1443:1443"
    volumes:
      - ./downloads:/app/downloads
      - ./pending:/app/pending
      - ./logs:/app/logs
      - ./config:/app/config
      - ./ssl:/app/ssl
    environment:
      - TZ=Asia/Shanghai
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

### 飞牛专用部署

飞牛系统推荐使用docker-compose进行部署，配置文件示例：

```yaml
version: '3.8'
services:
  go-download-server:
    image: gomail1/go_downloader:latest
    container_name: go-download-server
    restart: unless-stopped
    ports:
      - "9980:9980"
      - "1443:1443"
    volumes:
      - /vol1/1000/docker/go-download/downloads:/app/downloads
      - /vol1/1000/docker/go-download/pending:/app/pending
      - /vol1/1000/docker/go-download/logs:/app/logs
      - /vol1/1000/docker/go-download/config:/app/config
      - /vol1/1000/docker/go-download/ssl:/app/ssl
    environment:
      - TZ=Asia/Shanghai
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

**持久化配置**：

```
/vol1/1000/docker/go-download/
├── downloads/    # 下载文件目录
├── pending/      # 待处理文件目录
├── logs/         # 日志文件目录
└── config/       # 配置目录
    ├── config.json       # 主配置文件
    ├── daily_upload.json  # 每日上传限制配置
    └── stats.json        # 统计数据配置
```

### 1panel专用部署

1panel推荐使用docker-compose进行部署，配置文件示例：

```yaml
version: '3.8'
services:
  go-download-server:
    # Docker Hub镜像
    image: gomail1/go_downloader:latest
    # 备选镜像源：GitHub Container Registry
    # image: ghcr.io/gomail1/go-download:latest
    container_name: go-download-server
    restart: unless-stopped
    ports:
      - "9980:9980"
      - "1443:1443"
    volumes:
      - ./downloads:/app/downloads
      - ./pending:/app/pending
      - ./logs:/app/logs
      - ./config:/app/config
      - ./ssl:/app/ssl
    environment:
      TZ: Asia/Shanghai
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

### 部署注意事项

- 配置文件将在首次运行时自动生成
- 服务启动后可访问：
  - HTTP: `http://localhost:9980`
  - HTTPS: `https://localhost:1443`
- 所有数据将自动持久化到配置的目录中
- 如需修改端口或其他配置，可直接编辑`docker-compose.yml`文件后重启服务
- SSL证书放置在`ssl`目录下，如证书不存在，HTTPS服务将无法启动
- 确保数据目录权限设置正确，以便容器能够正常读写数据

## ⚠️ 风险警示

### 重要安全提示

- 本项目作为公开文件下载站，存在被恶意扫描、刷流量、滥用下载等风险
- 建议部署在受保护的网络环境中，或添加IP访问控制
- 定期检查服务器日志，及时发现异常访问
- 考虑添加访问速率限制，防止恶意刷流量
- 建议启用HTTPS，增强数据传输安全性
- 定期更新服务器系统和依赖，修复安全漏洞

## 操作界面演示-v0.0.3版

以下是系统主要功能的操作界面演示：

### 1. 公众主界面
![公众主界面](./操作演示/1公众主界面.png)

### 2. 上传界面
![上传界面](./操作演示/2上传.png)

### 3. 管理员界面
![管理员界面](./操作演示/3管理员.png)

### 4. 创建目录
![创建目录](./操作演示/4创建目录.png)

### 5. 用户上传界面
![用户上传界面](./操作演示/5用户上传界面.png)

### 6. 管理员审核提醒
![管理员审核提醒](./操作演示/6管理员审核提醒.png)

### 7. 管理员审核目录
![管理员审核目录](./操作演示/7管理员审核目录.png)

### 8. 用户管理界面
![用户管理界面](./操作演示/8用户管理界面.png)

### 9. 详细日志
![详细日志](./操作演示/9详细日志.png)

### 10. 服务器信息
![服务器信息](./操作演示/10服务器信息.png)

## 技术实现

本项目提供基于Docker的容器化部署方案，无需直接编译源代码。

- **容器化部署**: 使用Docker容器化技术，简化部署和管理
- **跨平台支持**: 支持Windows、Linux和Mac系统
- **双协议支持**: 同时支持HTTP和HTTPS访问
- **持久化存储**: 支持数据持久化到主机文件系统
- **自动配置**: 首次运行自动生成配置文件

## 开发说明

此项目使用纯Go标准库开发，无需额外的数据库依赖。所有文件操作都是直接文件系统操作，适合中小型文件分享场景。

## 许可证

MIT License
