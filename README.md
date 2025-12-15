# Go HTTP服务器下载站

一个使用Go语言开发的高性能文件下载站，提供文件上传、下载、浏览、审核和管理功能，支持基于角色的用户权限控制。

## 功能特性

- 🚀 **高性能**: 基于Go语言的HTTP服务器，高并发处理能力
- 📁 **文件浏览**: 美观的Web界面浏览可下载文件，支持目录导航
- ⬆️ **文件上传**: 支持选择上传目录，多用户角色权限控制
- 🗑️ **文件管理**: 在线删除文件，文件信息查看，目录创建和管理
- 📊 **统计信息**: 服务器运行状态、文件统计等
- 🎨 **响应式设计**: 适配各种设备的现代Web界面
- 🔒 **安全防护**: 文件名清理，防止路径遍历攻击
- 📈 **实时更新**: 文件列表实时刷新，搜索过滤功能
- 👤 **用户角色**: 支持管理员、普通用户、测试用户三种角色
- ✅ **文件审核**: 普通用户和测试用户上传的文件需要管理员审核
- 📁 **目录选择**: 上传和审核时可选择目标目录
- 📝 **配置文件**: 基于JSON的用户和服务器配置管理
- 📋 **服务器日志**: 查看服务器运行日志和详细信息
- 👥 **用户管理**: 管理员可添加、删除用户，配置角色和权限
- 🔧 **密码管理**: 支持修改用户密码
- 📊 **用户列表**: 表格形式展示用户信息，方便管理

## 项目结构

```
go-download-server/
├── main.go              # 主程序文件
├── go.mod               # Go模块文件
├── config.example.json  # 配置文件示例（不含真实密码）
├── config.json          # 配置文件（自动生成，包含真实密码，不上传GitHub）
├── downloads/           # 下载文件存储目录
├── pending/             # 待审核文件目录
├── logs/                # 服务器日志目录
├── test-files/          # 测试文件目录
├── start.bat            # Windows启动脚本
├── start.sh             # Linux/Mac启动脚本
└── README.md            # 项目说明文档
```

## GO语言直接部署

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置文件

在运行前，需要根据示例配置文件创建 `config.json` 文件：

1. 复制 `config.example.json` 文件为 `config.json`
2. 编辑 `config.json` 文件，修改用户名和密码等敏感信息
3. 保存配置文件

```bash
# Linux/Mac
cp config.example.json config.json

# Windows
copy config.example.json config.json
```

示例配置文件内容：

```json
{
  "users": [
    {
      "username": "admin",
      "password": "your_admin_password",
      "role": "admin",
      "max_file_size": 0
    },
    {
      "username": "user",
      "password": "your_user_password",
      "role": "normal",
      "max_file_size": 10737418240
    },
    {
      "username": "test",
      "password": "your_test_password",
      "role": "test",
      "max_file_size": 1073741824
    }
  ],
  "server": {
    "port": 9980,
    "download_dir": "./downloads",
    "pending_dir": "./pending",
    "log_dir": "./logs",
    "log_file": "server.log"
  }
}
```

**注意事项：**
- `config.json` 文件包含敏感的密码信息，Hub提示风险，不建议上传到GitHub或其他公共代码仓库
- `config.example.json` 文件是示例配置，不含真实密码，GitHub允许上传，用户需要根据示例创建自己的 `config.json` 文件
- 用户管理功能会自动更新 `config.json` 文件

### 3. 运行服务器

```bash
go run main.go
```

服务器将在 `http://localhost:9980` 启动

### 4. 访问服务

打开浏览器访问: `http://localhost:9980`

## 常规部署方案

### 1. 构建Docker镜像

```bash
docker build -t go-download .
```

### 2. 运行Docker容器

```bash
docker run -d -p 9980:9980 --name go-download go-download:latest
```

### 3. 映射数据目录（可选）

如果需要持久化存储下载文件、待审核文件和日志，可以映射数据目录：

```bash
docker run -d -p 9980:9980 \
  -v ./downloads:/app/downloads \
  -v ./pending:/app/pending \
  -v ./logs:/app/logs \
  -v ./config.json:/app/config.json \
  --name go-download go-download:latest
```

### 4. 查看容器日志

```bash
docker logs go-download
```

### 5. 停止和删除容器

```bash
docker stop go-download
docker rm go-download
```

## 飞牛专用部署方案

### 1. 配置文件说明

飞牛系统推荐使用docker-compose进行部署，已创建好的`docker-compose.yml`文件提供了两种镜像源的配置选项（GitHub Container Registry和Docker Hub），您可以根据需要选择使用：

```bash
cat docker-compose.yml
```

配置内容：
```yaml
version: '3.8'

services:
  go-download-server:
    image: gomail1/go_downloader:latest
    container_name: go-download-server
    restart: unless-stopped
    ports:
      - "9980:9980"
    volumes:
      - /vol1/1000/docker/go-download/downloads:/app/downloads
      - /vol1/1000/docker/go-download/pending:/app/pending
      - /vol1/1000/docker/go-download/logs:/app/logs
      - /vol1/1000/docker/go-download/config.json:/app/config.json
    environment:
      - TZ=Asia/Shanghai
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

**镜像源说明：**
- **GitHub Container Registry**: `ghcr.io/gomail1/go-download:latest`
- **Docker Hub**: `gomail1/go_downloader:latest`

您可以根据网络环境和访问偏好选择其中一个镜像源，默认使用Docker Hub镜像。

注意事项：
1：因拉取镜像时会把config.json文件创建成目录。这样需要自己新建一个config.json文件。
2：飞牛系统中，所有数据将持久化存储在`/vol1/1000/docker/go-download/`目录下。
3：Windows电脑可以新建一个TXT文本，将config.json文件内容复制到文本中，保存为config.json文件。（参考：示例配置文件内容）

### 2. 飞牛系统持久化配置

飞牛系统中，所有数据将持久化存储在以下目录：

```
/vol1/1000/docker/go-download/
├── downloads/    # 下载文件目录
├── pending/      # 待处理文件目录
├── logs/         # 日志文件目录
└── config.json   # 配置文件
```

确保该目录权限设置正确，以便容器能够正常读写数据。

## 用户角色和权限

### 1. 管理员 (admin)
- **账号**: admin / admin123
- **权限**: 
  - 浏览、下载、上传文件（无大小限制）
  - 直接上传文件到下载目录（无需审核）
  - 创建、管理下载目录
  - 审核普通用户和测试用户上传的文件
  - 查看服务器日志和详细信息
  - 删除文件和目录

### 2. 普通用户 (normal)
- **账号**: user / user123
- **权限**:
  - 浏览、下载文件
  - 上传文件（最大10GB）
  - 上传的文件需要管理员审核
  - 查看自己的上传审核进度

### 3. 测试用户 (test)
- **账号**: test / test123
- **权限**:
  - 浏览、下载文件
  - 上传文件（最大1GB）
  - 上传的文件需要管理员审核
  - 查看自己的上传审核进度

## 使用说明

### 文件上传
1. 登录系统
2. 点击"上传文件"按钮
3. 选择要上传的文件
4. **选择目标目录**（可选择根目录或子目录）
5. 点击"开始上传"按钮
6. 等待上传完成

### 文件审核
1. 以管理员身份登录
2. 点击"文件审核"按钮
3. 浏览待审核的文件
4. **选择目标目录**（可选择根目录或子目录）
5. 点击"通过"按钮审核通过，或点击"拒绝"按钮拒绝
6. 审核通过的文件将被移动到指定的下载目录

### 文件下载
1. 浏览文件列表
2. 点击文件名直接下载
3. 支持按文件名搜索和过滤

### 目录管理
1. 以管理员身份登录
2. 在文件列表页面点击"创建目录"按钮
3. 输入目录名称
4. 选择父目录
5. 点击"创建"按钮

### 服务器日志和信息
1. 以管理员身份登录
2. 点击"查看服务器日志"按钮查看运行日志
3. 点击"查看服务器详细信息"按钮查看系统状态

### 用户管理
1. 以管理员身份登录
2. 点击"用户管理"按钮进入用户管理页面
3. **用户列表**: 以表格形式展示所有用户信息，包括用户名、角色、最大文件大小和操作
4. **添加用户**:
   - 填写用户名、密码
   - 选择角色（普通用户/测试用户）
   - 设置最大文件大小
   - 点击"添加用户"按钮
5. **修改密码**:
   - 在用户列表中找到要修改的用户
   - 在"新密码"和"确认密码"输入框中填写新密码
   - 点击"修改"按钮
6. **删除用户**:
   - 在用户列表中找到要删除的用户
   - 点击"删除"按钮
   - 确认删除操作
   - **注意**: 管理员账户（admin）默认不可删除

### 用户角色和权限配置
1. 管理员可以在添加或编辑用户时配置角色
2. **角色说明**:
   - **管理员(admin)**: 具有所有权限，上传文件无需审核
   - **普通用户(normal)**: 可以上传和下载文件，上传文件需要审核
   - **测试用户(test)**: 权限与普通用户类似，但上传文件大小限制较小
3. 每个用户可以设置不同的最大文件上传大小
4. 用户配置会自动保存到`config.json`文件中

## 技术实现

- **HTTP服务**: Go标准库 net/http
- **路由处理**: http.ServeMux (Go标准库)
- **文件处理**: io, os, path/filepath
- **前端界面**: 响应式HTML/CSS/JavaScript
- **配置管理**: JSON格式配置文件
- **用户认证**: 基于配置文件的认证系统
- **权限控制**: 基于角色的访问控制（RBAC）
- **文件大小**: 智能格式转换 (B, KB, MB, GB)
- **零外部依赖**: 仅使用Go标准库，无需额外安装包

## 安全特性

- 文件名清理和验证
- 路径遍历攻击防护（使用filepath.Clean）
- 基于角色的权限控制
- 最大文件大小限制
- 安全的文件删除机制
- 目录遍历防护

## 操作界面演示

以下是系统主要功能的操作界面演示：

### 1. 主界面
![主界面](./图片说明/1主界面.png)

### 2. 管理员界面
![管理员界面](./图片说明/2管理员界面.png)

### 3. 创建目录
![创建目录](./图片说明/3创建目录.png)

### 4. 创建成功主界面
![创建成功主界面](./图片说明/4创建成功主界面.png)

### 5. 用户管理界面
![用户管理界面](./图片说明/5用户管理界面.png)

### 6. 服务器日志
![服务器日志](./图片说明/6服务器日志.png)

### 7. 服务器信息
![服务器信息](./图片说明/7服务器信息.png)

### 8. 用户上传文件
![用户上传文件](./图片说明/8用户上传文件.png)

### 9. 管理员审核界面
![管理员审核界面](./图片说明/9管理员审核界面.png)

### 10. 审核通过发布界面
![审核通过发布界面](./图片说明/10审核通过发布界面.png)

## 开发说明

此项目使用纯Go标准库开发，无需额外的数据库依赖。所有文件操作都是直接文件系统操作，适合中小型文件分享场景。

## 许可证

MIT License