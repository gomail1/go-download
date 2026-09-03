# Go HTTP服务器下载站 - V1.2.0

使用Go语言开发的高性能文件下载站，提供文件上传、下载、浏览、审核和管理功能，支持基于角色的用户权限控制、全平台响应式设计、多协议下载功能，以及完善的安全防护和监控告警系统。

**GitHub仓库链接**: [https://github.com/gomail1/go-download](https://github.com/gomail1/go-download)
**Docker仓库链接**: [https://hub.docker.com/r/gomail1/go_downloader](https://hub.docker.com/r/gomail1/go_downloader)
**演示链接**: [https://go.dansg.xyz/](https://go.dansg.xyz/)

## 📢 最新更新

### V1.2.0 版本内容 (2026-08-30 ~ 2026-08-31)

#### 🌐 IP下载管理系统（新增）
1. **IP下载统计**
   - 自动记录每个IP的下载次数、总流量、首次出现时间、最后下载时间
   - 数据持久化存储到 `logs/ip_stats.json`
   - 支持按下载次数排序展示

2. **IP封禁/解封管理**
   - 支持手动添加封禁IP（输入IP地址和封禁原因）
   - 支持对已有IP进行封禁/解封操作
   - 被封禁IP无法下载文件，返回403 Forbidden
   - 封禁原因记录到操作日志

3. **IP流量限额配置**
   - 支持配置每日下载次数限制、每日下载流量限制
   - 支持配置每小时下载次数限制、每小时下载流量限制
   - 超过限额返回429 Too Many Requests
   - 支持自动封禁功能，超过限额时自动封禁IP

4. **历史日志自动导入**
   - 服务器启动时自动扫描 `logs/server_*.log` 历史日志
   - 解析下载记录，提取IP地址和时间，按IP聚合计数
   - 与现有IP统计合并去重，不覆盖已有的封禁状态
   - 只导入一次，通过 `logs/.ip_stats_imported` 标记文件记录
   - 历史日志中没有流量信息，导入的IP流量为0（后续下载正常累计）

5. **操作日志中文映射**
   - 所有操作类型显示中文名称，包括：
     - 认证相关：登录、退出登录、登录尝试、登录失败、登录调试、注册
     - 文件相关：上传文件、下载文件、删除文件、批量删除文件、重命名文件、创建目录、查看文件列表、审核文件、拒绝文件、API请求
     - 用户管理：添加用户、删除用户、修改密码、更新用户
     - IP管理：封禁IP、解封IP、更新IP限额配置
     - 分类管理：创建分类、更新分类、删除分类、设置文件分类
     - 系统相关：查看服务器信息、查看操作日志、查看热力图、查看IP管理、下载被阻止、下载被限制、IP自动封禁、告警触发
   - 操作日志页面清晰展示管理员操作记录

#### 🔒 全面安全加固
1. **XSS 跨站脚本防护**
   - 创建 `utils/xss.go` 工具，提供 `EscapeHTML`、`EscapeJavaScript`、`EscapeAttribute`、`SanitizeHTML`、`IsValidURL` 等函数
   - 将 XSS 防护应用到所有关键页面：文件列表、用户管理、日志页面、管理员界面
   - 对文件名、路径、分类、用户名、日志详情等用户输入进行严格编码
   - 清理危险HTML标签（script、style、iframe等）和事件处理属性
   - 验证和清理危险URL协议（javascript:、vbscript:等）

2. **CSRF 跨站请求伪造防护**
   - 创建 `utils/csrf.go` 工具，基于会话ID生成和验证CSRF令牌
   - 使用 `crypto/rand` 生成32字节安全令牌，常量时间比较防止时序攻击
   - 支持表单隐藏字段（`csrf_token`）和AJAX请求头（`X-CSRF-Token`）两种方式
   - 登录时自动生成CSRF令牌，页面注入meta标签和csrf.js，表单中添加隐藏字段
   - 将 CSRF 防护应用到所有关键操作：
     - 用户管理（添加用户、修改密码、删除用户）
     - 文件操作（上传、删除、批量删除、批量移动）
     - 分类管理（创建、编辑、删除分类）
     - 下载任务API（创建、暂停、恢复、删除任务）
   - 公开分享计数 `/api/increment-share` 保持匿名豁免

3. **路径遍历防护增强**
   - 创建 `utils/path_security.go` 工具，提供 `ValidateSafePath` 函数
   - 8步严格验证：清理路径、检查`..`、获取绝对路径、构建完整路径、验证是否在基础目录内等
   - 拒绝绝对路径、路径穿越（`..`）、越出下载目录的路径
   - 应用到文件下载、文件浏览等所有路径相关操作

4. **文件类型安全检查**
   - 上传文件时检查文件类型，阻止危险文件上传
   - 防止上传可执行文件、脚本文件等危险类型

5. **会话管理安全性增强**
   - 使用 `crypto/rand` 生成安全的会话ID
   - 添加 `HttpOnly`、`SameSite` 属性，`Secure` 按部署切换
   - 会话过期自动清理，密码修改时失效相关会话
   - 新增过期会话清理任务，定期回收失效session

6. **下载任务API鉴权补全**
   - `internal/api`（gin）增加会话鉴权中间件（匿名 → 401）
   - 增加CSRF中间件（缺令牌 → 403）
   - 所有任务端点都在需要登录+CSRF的路由组中
   - WebSocket端点也添加了登录鉴权

7. **分类管理鉴权补全**
   - 分类API（POST/PUT/DELETE）在CSRF校验之外增加登录鉴权
   - 防止普通用户改全站分类

#### ⚡ 性能优化
1. **Gzip 压缩**
   - 创建 `middleware.go`，启用 Gzip 压缩
   - 压缩 HTML、CSS、JavaScript 等文本资源，减少传输大小
   - 不对文件下载（`/download`、`/s/`）进行压缩，避免性能问题
   - 不对WebSocket（`/ws/`）进行压缩，避免Hijack问题

2. **静态资源缓存**
   - 为静态资源（CSS、JS、图片、字体、图标）设置缓存头
   - 根据文件扩展名设置不同缓存时间：
     - CSS/JS：1天
     - 图片（png/jpg/gif/svg/webp）：7天
     - 字体（woff/woff2/ttf/eot）：30天
     - 图标（ico）：30天
   - 减少重复请求，提高访问速度

#### 📊 功能优化
1. **监控告警系统**
   - 创建 `utils/monitor.go`，实现系统监控和告警功能
   - 监控 CPU、内存、磁盘使用率，请求量、错误率、响应时间
   - 支持告警规则配置，超过阈值自动触发告警
   - 将监控告警合并到服务器信息页面，统一展示

2. **跨平台磁盘使用率计算**
   - 创建 `utils/monitor_disk_windows.go`（Windows 平台）
   - 创建 `utils/monitor_disk_unix.go`（Linux/Mac 平台）
   - 使用条件编译实现跨平台支持
   - Windows 使用 `GetDiskFreeSpaceExW`，Linux/Mac 使用 `syscall.Statfs`
   - 在三个平台上都能准确计算磁盘使用率

3. **日志管理完善**
   - 创建 `utils/log_management.go`，完善日志管理功能
   - 支持日志级别分类、日志查询、日志清理
   - 记录用户操作、系统事件、安全告警

4. **热力图页面修复**
   - 修复热力图页面的 HTML 结构问题
   - 使用本地 Chart.js，避免依赖外部CDN
   - 添加降级方案，图表加载失败时显示友好提示

5. **文件分类管理**
   - 支持管理员自定义文件分类
   - 用户可对文件进行归类，前端按分类展示
   - 分类管理集成到文件列表页面，操作便捷

6. **前端分页设计**
   - 文件列表支持分页展示
   - 可配置每页显示数量，支持页码跳转
   - 提高大量文件时的加载速度

#### 🎨 界面全面重新设计
1. **前端用户界面**
   - 现代简洁风格（蓝白配色）
   - 优化 Hero 区域：背景、间距、字体大小、搜索框样式
   - 优化文件列表展示：卡片式布局、文件图标、下载按钮
   - 优化分类展示：分类标签、分类筛选
   - 移除不必要的顶部导航栏，简化界面布局
   - 支持响应式设计，适配桌面端和移动端

2. **后端管理界面**
   - 高级质感风格（Stripe 风格）
   - 优化导航栏：侧边栏导航、图标+文字
   - 优化数据表格：斑马纹、悬停效果、操作按钮
   - 优化表单设计：输入框、按钮、下拉框
   - 优化统计卡片：数据展示、图标、颜色
   - 所有管理操作在右侧内容区展开，不跳转到单独页面

#### 📝 开发优化
1. **测试覆盖率提升**
   - 新增 14+ 个测试用例
   - 覆盖安全工具、路径验证、CSRF防护、XSS防护、短链安全等
   - 所有测试用例全部通过

2. **代码风格指南**
   - 创建 `CODE_STYLE.md`
   - 包括命名规范、注释规范、错误处理规范等

3. **并发安全修复**
   - 修复用户管理自死锁问题（严重）
   - `config.UsersMu`（`sync.RWMutex`）保护并发读写
   - stats/files等共享数据访问加锁
   - 目录缓存返回副本防竞态

4. **下载引擎状态机修复**
   - 暂停任务被误标为「失败」：错误判定中排除 `context.Canceled`
   - `CancelTask` 现在真正调用 `cancelFunc()` 中断任务

5. **WebSocket 实时推送修复**
   - `statusResponseWriter` / `gzipResponseWriter` 实现 `http.Hijacker`
   - 启动时赋值 `GlobalWebSocketHub`，修复实时进度推送

6. **其他健壮性**
   - `GetClientIP` 仅在 `TrustProxy` 开启时信任 `X-Forwarded-For`
   - BT 种子文件解析限制读取大小，防大文件打爆内存
   - `SaveConfig` 改为原子写（临时文件 + rename）
   - `daily_upload` 统计数据落盘持久化
   - `CheckAPIAuthentication` 支持独立 API Key 配置

#### 🔒 短链接安全策略（按文件去重，防无限注入）
1. **漏洞根因**
   - 旧版本生成短链**不校验、不限制**：攻击者可直接模拟请求，反复为同一文件生成短链，向服务器无限注入垃圾记录。

2. **修复方案——按目标文件去重（核心防线）**
   - 分享是公开能力，**不要求登录鉴权**；防滥用不靠鉴权，而靠「同一个文件永远只对应一条短链」。
   - 生成短链前先按规范化后的目标路径查询：若文件已存在短链，**直接复用既有短码，不写入新记录、不走限流**。
   - 因此短链总数上限天然等于「下载目录里的文件总数」，外部无法通过反复请求无限注入。

3. **第二、三道防线（兜底，仅真正新增时触发）**
   - **单 IP 限流**：默认 1 分钟内同 IP 最多生成 120 条（`DefaultShortURLRateLimit`），超限返回 `ErrShortURLRateLimited`，需稍后重试。
   - **全局总量上限**：默认最多 5000 条（`DefaultShortURLMaxTotal`），超出返回 `ErrShortURLQuotaExceeded`，需先清理历史短链。
   - 两项阈值均可通过 `SetShortURLPolicy` 覆盖（0 值回退默认）。

4. **限流器内存安全修复**
   - 旧实现按 `limit` 值预分配切片（`make([]T, 0, limit)`），`limit` 取值很大时单次请求即分配数十 MB，高频调用可打爆内存、卡死进程。
   - 改为仅按「实际记录数 + 1」预分配并钉死单 key 上限 `rateLimiterMaxRecords=1024`；同时限制跟踪 key（IP）数量 `rateLimiterMaxKeys=10000`，超量自动清理过期条目。已加回归测试 `TestRateLimiterNoPrealloc` 守护。

5. **路径安全与清理**
   - 生成前 `ValidateShortURLPath` 拒绝绝对路径、路径穿越（`..`）、越出下载目录及目录对象，仅允许已存在的普通文件。
   - 解析短链时**二次校验**路径，历史库中残留的非法条目会自动失效。
   - 管理端支持 `ResetShortURLStore` 一键清空全部短链（内存 + 磁盘 `config/shorturls.json`）。

#### ✅ 多协议下载启用与优化
1. **BitTorrent（已启用，真实下载）**
   - `bt` / `magnet` 协议注册为真实下载器（基于 `github.com/anacrolix/torrent`）。
   - 支持磁力链接（`magnet:?xt=urn:btih:...`）与 `.torrent` 种子文件，支持做种、断点续传。

2. **FTP（已启用，真实下载）**
   - `ftp` 协议注册为真实下载器，采用 **PASV 被动模式 + RETR** 流式拉取文件，支持进度/暂停/恢复/取消。
   - 仅支持明文 `ftp://`；**未实现 FTPS/TLS**，请勿用于传输敏感文件。

3. **eDonkey2000 / ED2K（已启用，真实下载）**
   - `ed2k` 协议注册为真实 eDonkey2000 客户端（`ed2k://` 链接），实现：
     - 真实 **ed2k 哈希**：每 9728000 字节（9500×1024）分块做 MD4，多分块再对分块哈希拼接做 MD4，用于分片校验与完整性验证。
     - 服务器登录与源发现：登录 eD2K 服务器获取 peer 的 userhash / IP / 端口。
     - 对等端握手（Hello/HelloAnswer）、分片请求（RequestParts）/接收（PartData）、**zlib 解压**、分片位图管理、MD4 分片校验、断点续传（`.part` 文件 + 位图）。
   - 说明：Kad 网络、协议混淆、UDP 源交换、AICH 暂未实现；真实端到端下载需联网环境（活的 eD2K 服务器 + 在线 peer），本机未做联网验证。

## 🎯 功能特性

### 🔐 权限管理系统
- **多角色权限控制**：支持管理员、二级管理员、普通用户和访客四种角色
- **细粒度权限分配**：针对不同角色分配不同的操作权限
- **会话管理**：基于Cookie的安全认证机制，支持会话过期自动清理（24小时有效期）
- **默认子用户**：自动创建 `download-user` 子用户，用于接收下载任务文件，无登录权限

#### 用户角色权限对照表

| 权限/功能     | 管理员 | 二级管理员 | 普通用户 | 访客 |
|---------------|--------|------------|----------|------|
| 查看文件列表   | ✅     | ✅         | ✅       | ✅   |
| 文件下载       | ✅     | ✅         | ✅       | ✅   |
| 文件分享       | ✅     | ✅         | ✅       | ✅   |
| 文件上传       | ✅     | ✅         | ✅       | ❌   |
| 创建目录       | ✅     | ✅         | ✅       | ❌   |
| 删除文件       | ✅     | ✅         | ❌       | ❌   |
| 审核文件       | ✅     | ✅         | ❌       | ❌   |
| 用户管理       | ✅     | ✅         | ❌       | ❌   |
| 查看日志       | ✅     | ✅         | ❌       | ❌   |
| 查看统计信息   | ✅     | ✅         | ❌       | ❌   |
| 无限制上传     | ✅     | ✅         | ❌       | ❌   |
| 下载任务管理   | ✅     | ✅         | ❌       | ❌   |
| 分类管理       | ✅     | ✅         | ❌       | ❌   |

### 🌐 多协议下载功能
- **多协议支持**：当前版本支持 HTTP/HTTPS、BitTorrent（磁力链接 / `.torrent`）、FTP（明文 PASV）以及 eDonkey2000（ED2K）协议下载
  - `http` / `https`：文件直链（最常见），支持多线程加速与断点续传
  - `bt` / `magnet`：基于 `anacrolix/torrent` 的真实 BitTorrent 下载，支持做种与续传
  - `ftp`：PASV 被动模式 + RETR 真实流式拉取（仅明文，未实现 FTPS/TLS）
  - `ed2k`：真实 eDonkey2000 客户端，含 ed2k 哈希分片校验、服务器源发现、zlib 解压与断点续传
- **多线程加速**：支持多线程下载，提高下载速度
- **断点续传**：支持断点续传功能，保障下载可靠性
- **下载任务管理**：
  - 支持创建、查看、控制下载任务
  - 实时显示任务状态、进度、速度等信息
  - 支持暂停、恢复、删除下载任务
  - 提供任务详情页面，显示完整任务信息
  - WebSocket 实时推送下载进度
- **随机端口生成**：为 BitTorrent 添加随机端口生成功能，防止 ISP 封锁
- **自动审核机制**：所有下载文件默认归属于 `download-user` 子用户，自动进入待审核队列

### 📁 文件管理功能
- **文件上传**：支持拖拽上传、批量上传和目录选择，带有文件大小验证和每日上传限制
  - 普通用户：20GB/日上传限制
  - 管理员：无限制上传
  - 实时显示上传进度和今日剩余空间
- **文件下载**：支持单文件下载，精确记录下载次数和带宽消耗
- **文件浏览**：清晰的文件目录结构展示，支持按名称排序
- **文件删除**：支持单文件和批量删除，管理员和二级管理员都有权限
- **目录管理**：支持创建目录，方便文件分类管理
- **文件审核**：上传和下载文件自动进入待审核队列，需要管理员审核才能发布
  - 审核流程：用户上传/系统下载 → 存储到pending/[username]/目录 → 管理员审核 → 移动到downloads/目录对外发布
  - 审核提醒：实时显示待审核文件数量，管理员可快速查看待审核文件
  - 拒绝机制：支持拒绝文件上传/下载请求，自动清理待审核文件
  - 批量审核：支持批量处理待审核文件

### 📂 文件分类管理
- **自定义分类**：管理员可创建、编辑、删除文件分类
- **文件归类**：用户可对文件进行归类，设置文件所属分类
- **前端展示**：前端首页按分类展示文件，支持分类筛选
- **分类图标**：每个分类可设置自定义图标
- **权限控制**：只有管理员和二级管理员可以管理分类

### 🔗 短链接功能
- **短链生成**：为文件生成短链接，方便分享
- **按文件去重**：同一个文件永远只对应一条短链，防止无限注入
- **单IP限流**：默认1分钟内同IP最多生成120条短链
- **全局总量上限**：默认最多5000条短链
- **路径安全**：生成前验证路径安全，防止路径遍历
- **短链管理**：管理端支持查看和删除短链
- **友好提示**：短链不存在或已过期时，显示美观的提示页面，3秒后自动跳转到首页

### 📄 免责协议功能
- **首次登录确认**：用户首次登录时必须同意免责协议
- **同意状态持久化**：同意状态保存到配置文件，后续登录无需再次确认
- **访问控制**：未同意协议的用户无法使用系统功能
- **版本管理**：支持免责协议版本更新，版本变更时重新要求用户确认
- **清晰的条款展示**：使用易于阅读的格式展示完整的免责协议
- **滚动强制阅读**：要求用户滚动到页面底部才能点击同意按钮
- **明确的操作按钮**："同意"和"取消"按钮，样式鲜明

### 👥 用户管理系统
- **用户认证**：基于Cookie的安全认证机制，支持密码验证
- **用户列表**：管理员可查看系统所有用户信息，二级管理员只能查看普通用户
- **权限验证**：实时验证用户权限，确保操作安全性
- **管理员保护**：超级管理员账号默认不可删除，对二级管理员隐藏
- **密码管理**：支持修改用户密码，二级管理员只能修改普通用户密码
- **角色管理**：管理员可创建、修改和删除所有角色用户，二级管理员只能创建和管理普通用户
- **并发安全**：使用 `sync.RWMutex` 保护用户配置的并发读写

### 🌐 IP下载管理功能
- **IP下载统计**：自动记录每个IP的下载次数、总流量、首次出现时间、最后下载时间
- **IP封禁管理**：支持手动添加封禁IP，输入IP地址和封禁原因，被封禁IP无法下载文件
- **IP解封功能**：支持对已封禁IP进行解封操作，封禁后按钮自动切换为解封
- **IP流量限额**：支持配置每日/每小时下载次数和流量限制，超过限额返回429
- **自动封禁**：启用后，超过流量限额的IP会被自动封禁
- **历史日志导入**：启动时自动从历史操作日志导入IP统计数据（只导入一次），支持从最新日志格式中提取实际传输字节数
- **统计展示**：IP管理页面展示总IP数、已封禁IP、总下载次数、总下载流量、今日下载、今日流量
- **IP筛选功能**：支持按状态筛选IP列表（全部/正常/已封禁），每个选项显示对应数量
- **IP分页功能**：IP列表支持分页展示，默认每页20条，支持上一页/下一页导航，可通过URL参数自定义每页数量
- **操作日志**：所有IP管理操作（封禁、解封、更新配置）都记录到操作日志

### 📊 统计与监控功能
- **下载统计**：精确记录每个文件的下载次数和最后下载时间
- **分享统计**：跟踪文件分享活动，记录分享次数和最后分享时间
- **带宽监控**：精确记录文件下载流量，帮助管理员了解带宽使用情况
- **热力图可视化**：图形化展示文件活动趋势，支持按时间维度查看

#### 热力图统计面板
为管理员提供直观的数据可视化功能，帮助了解文件分享和下载活动趋势：
1. **活动趋势图**：使用Chart.js创建的柱状图，展示最近7天的文件分享和下载活动趋势
2. **文件统计表**：详细的文件统计数据表格，包括文件路径、分享次数、下载次数、最后分享时间和最后下载时间
3. **实时数据**：数据实时更新，反映最新的文件使用情况
4. **管理员与二级管理员专享**：只有管理员和二级管理员角色可以访问，确保数据安全性

#### 监控告警系统
- **系统监控**：实时监控 CPU、内存、磁盘使用率
- **应用监控**：监控请求量、错误率、响应时间
- **告警规则**：支持配置告警阈值，超过阈值自动触发告警
- **告警级别**：支持 info、warning、critical 三种告警级别
- **统一展示**：监控告警数据合并到服务器信息页面，统一展示

### 📱 全平台响应式设计
- **6级媒体查询断点**：覆盖从超小手机(360px)到桌面端(1025px+)的所有设备
- **触摸友好交互**：所有交互元素符合44x44px触摸目标尺寸，优化移动操作体验
- **响应式布局**：根据屏幕尺寸自动调整界面元素，确保在各种设备上都有良好的显示效果
- **横屏适配**：优化手机横屏模式的布局和可用性，提高大屏幕利用率
- **可访问性支持**：支持用户缩放，确保不同用户的使用需求

### 🎨 界面设计

#### 前端用户界面
- **现代简洁风格**：蓝白配色，清新美观
- **Hero 区域**：渐变背景、大标题、搜索框、统计数据展示
- **文件列表**：卡片式布局、文件图标、文件名、文件大小、下载按钮
- **分类展示**：分类标签、分类筛选、分类图标
- **分页设计**：页码导航、每页数量配置
- **响应式设计**：适配桌面端和移动端

#### 后端管理界面
- **高级质感风格**：Stripe 风格，专业大气
- **侧边栏导航**：图标+文字，清晰明了
- **数据表格**：斑马纹、悬停效果、操作按钮
- **表单设计**：统一的输入框、按钮、下拉框样式
- **统计卡片**：数据展示、图标、颜色区分
- **单页应用**：所有管理操作在右侧内容区展开，不跳转到单独页面

### 🛡️ 安全防护措施

#### 1. XSS 跨站脚本防护
- **HTML编码**：对所有用户输入进行HTML实体编码
- **HTML清理**：移除危险标签（script、style、iframe等）和事件处理属性
- **URL验证**：验证和清理危险URL协议（javascript:、vbscript:等）
- **属性安全**：对HTML属性值进行安全处理，防止属性注入
- **全页面覆盖**：应用到文件列表、用户管理、日志页面、管理员界面等所有关键页面

#### 2. CSRF 跨站请求伪造防护
- **安全令牌**：使用 `crypto/rand` 生成32字节安全令牌
- **常量时间比较**：防止时序攻击
- **双渠道支持**：支持表单隐藏字段和AJAX请求头两种方式
- **全操作覆盖**：应用到用户管理、文件操作、分类管理、下载任务API等所有关键操作
- **公开接口豁免**：公开分享计数接口保持匿名豁免

#### 3. 路径遍历防护
- **8步严格验证**：清理路径、检查`..`、获取绝对路径、验证是否在基础目录内等
- **全路径覆盖**：应用到文件下载、文件浏览等所有路径相关操作
- **符号链接检查**：可选的符号链接目标验证

#### 4. 访问控制
- **严格的权限验证**：实时验证用户权限，防止未授权访问
- **多角色支持**：管理员、二级管理员、普通用户、访客四种角色
- **细粒度权限**：针对不同角色分配不同的操作权限

#### 5. 会话管理
- **HttpOnly Cookie**：防止XSS攻击窃取会话
- **SameSite 属性**：防止CSRF攻击
- **Secure 属性**：HTTPS时启用，保护数据安全
- **24小时自动过期**：定期清理过期会话
- **密码修改失效**：密码修改时自动失效相关会话
- **安全会话ID**：使用 `crypto/rand` 生成安全的会话ID

#### 6. 密码安全
- **哈希存储**：密码哈希存储，防止密码泄露
- **独立API Key自动生成**：首次启动服务器时自动生成随机API Key（64字符十六进制），不再直接比对管理员明文密码；已存在配置文件时不覆盖，仅在api_key为空时自动生成并备份原配置

#### 7. 混合内容修复（HTTPS安全）
- **协议自动识别**：新增 `GetRequestScheme()` 函数，优先检查 `X-Forwarded-Proto` 头，其次检查 `r.TLS`，自动识别HTTP/HTTPS
- **图标URL修复**：文件列表图标URL、图标处理器URL统一使用当前请求协议，避免HTTPS页面中加载HTTP资源
- **短链URL修复**：短链生成API的FullURL从硬编码 `http://` 改为动态获取当前协议，确保反向代理HTTPS环境下生成正确的HTTPS短链
- **适用场景**：Nginx/Caddy/Traefik等反向代理HTTPS部署时，所有资源链接自动适配HTTPS，消除浏览器混合内容警告

#### 8. 反向代理真实IP获取
- **直接信任代理头**：`GetClientIP()` 函数直接检查 `X-Forwarded-For` 和 `X-Real-IP` 请求头，获取反向代理后的真实客户端IP
- **多代理链支持**：正确解析 `X-Forwarded-For` 中的多个IP，取第一个为真实客户端IP
- **日志记录准确**：用户登录、文件访问、下载等操作日志中记录真实IP，而非Docker网关或代理服务器IP
- **适用场景**：Docker部署、Nginx反向代理、CDN等环境下，日志和统计中的IP地址准确反映真实访问者

#### 9. 文件安全
- **文件类型检查**：阻止危险文件上传
- **文件大小限制**：根据用户角色设置不同的文件大小限制
- **每日上传限制**：普通用户20GB/日上传限制

#### 10. SSL支持
- **HTTPS加密传输**：支持HTTPS加密传输，保护数据安全
- **证书配置**：支持配置证书文件和私钥文件

### 📝 日志系统
- **多级日志记录**：支持success、error、warning、debug和info五种日志级别
- **详细日志内容**：记录用户操作、系统事件和错误信息
- **日志筛选功能**：支持按日期和日志级别筛选日志
- **日志持久化**：日志信息持久化存储，便于后续查询和分析
- **结构化日志格式**：包含时间戳、级别、用户名、角色、操作和详情
- **IP记录**：记录用户IP地址，支持IPv4和IPv6
- **日志管理**：支持日志查询、日志清理、日志级别配置

## 🚀 快速开始

### 环境要求
- Go 1.21+
- 支持的操作系统：Windows、Linux、macOS

### 安装

#### 方式一：从源码编译
```bash
# 克隆仓库
git clone https://github.com/gomail1/go-download.git
cd go-download

# 编译
go build -o go-download .

# 运行
./go-download
```

#### 方式二：使用 Docker
```bash
# 拉取镜像
docker pull gomail1/go_downloader:latest

# 运行容器
docker run -d \
  -p 9980:9980 \
  -v /path/to/downloads:/app/downloads \
  -v /path/to/config:/app/config \
  --name go-download \
  gomail1/go_downloader:latest
```

### 配置
首次运行会自动生成默认配置文件 `config/config.json`，可以根据需要修改配置。

#### 配置文件结构
```json
{
  "users": [
    {
      "username": "admin",
      "password": "admin123",
      "role": "admin",
      "max_file_size": 0
    }
  ],
  "server": {
    "port": 9980,
    "https_port": 1443,
    "cert_file": "",
    "key_file": "",
    "download_dir": "downloads",
    "pending_dir": "pending",
    "log_dir": "logs",
    "trust_proxy": false,
    "api_key": "",
    "server_name": "Go下载站"
  },
  "legal": {
    "terms_enabled": true,
    "terms_version": "1.0",
    "terms_content": "免责协议内容...",
    "footer_text": "© 2026 Go下载站",
    "browser_tips": "推荐使用Chrome、Firefox、Edge等现代浏览器"
  }
}
```

#### 配置项说明

##### 用户配置 (users)
| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| username | 用户名 | - |
| password | 密码（哈希存储） | - |
| role | 角色（admin/subadmin/normal） | - |
| max_file_size | 最大文件大小（字节，0表示无限制） | 0 |

##### 服务器配置 (server)
| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| port | HTTP端口 | 9980 |
| https_port | HTTPS端口 | 1443 |
| cert_file | SSL证书文件路径 | - |
| key_file | SSL私钥文件路径 | - |
| download_dir | 下载目录 | downloads |
| pending_dir | 待审核目录 | pending |
| log_dir | 日志目录 | logs |
| trust_proxy | 是否信任反向代理（X-Forwarded-For） | false |
| api_key | API密钥（可选） | - |
| server_name | 服务器名称 | Go下载站 |

##### 法律配置 (legal)
| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| terms_enabled | 是否启用免责协议 | true |
| terms_version | 免责协议版本 | 1.0 |
| terms_content | 免责协议内容 | - |
| footer_text | 页脚文字 | © 2026 Go下载站 |
| browser_tips | 浏览器提示 | - |

### 默认账号
- 管理员：admin / admin123（首次登录后请修改密码）

### 访问
- 前端首页：http://localhost:9980
- 登录页面：http://localhost:9980/login
- 管理后台：http://localhost:9980/admin

## 📡 API 文档

### 公开接口

#### 获取统计信息
```
GET /api/stats
```
返回文件下载和分享统计信息。

#### 增加分享计数
```
POST /api/increment-share?path=文件路径
```
匿名可用，增加文件的分享计数。

#### 获取分类列表
```
GET /api/categories
```
返回所有文件分类。

#### 获取自定义排序
```
GET /api/get-custom-sort?path=目录路径
```
返回指定目录的文件自定义排序。

### 需要鉴权的接口

所有需要鉴权的接口都需要：
1. 登录会话（Cookie）
2. CSRF令牌（请求头 `X-CSRF-Token` 或表单字段 `csrf_token`）

#### 文件操作
```
POST /delete          # 删除文件
POST /batch-delete    # 批量删除文件
POST /batch-move      # 批量移动文件
POST /batch-copy      # 批量复制文件
POST /mkdir           # 创建目录
POST /upload          # 上传文件
```

#### 用户管理
```
POST /add-user        # 添加用户
POST /change-password # 修改密码
POST /delete-user     # 删除用户
```

#### 分类管理
```
POST   /api/categories          # 创建分类
PUT    /api/categories/:id      # 更新分类
DELETE /api/categories/:id      # 删除分类
POST   /api/file-category        # 设置文件分类
```

#### 下载任务管理（Gin路由）
```
GET    /api/tasks              # 获取任务列表
POST   /api/tasks              # 创建任务
POST   /api/tasks/upload       # 上传种子文件
GET    /api/tasks/:id          # 获取任务详情
PUT    /api/tasks/:id/pause    # 暂停任务
PUT    /api/tasks/:id/resume   # 恢复任务
DELETE /api/tasks/:id          # 删除任务
GET    /api/stats              # 获取统计信息
GET    /ws/events              # WebSocket实时推送
```

#### 审核管理
```
POST /approve  # 审核通过
POST /reject   # 审核拒绝
```

#### 保存自定义排序
```
POST /api/save-custom-sort
```

## 📁 项目结构

```
go-download/
├── main.go                    # 主程序入口
├── middleware.go              # 中间件（Gzip、缓存、安全头）
├── config/                    # 配置管理
│   └── config.go             # 配置结构体和加载逻辑
├── constants/                 # 常量定义
│   └── constants.go          # 角色、权限等常量
├── session/                   # 会话管理
│   └── session.go            # 会话创建、验证、清理
├── handlers/                  # HTTP处理器
│   ├── files.go              # 文件列表、浏览、搜索
│   ├── download.go           # 文件下载
│   ├── upload.go             # 文件上传
│   ├── delete.go             # 文件删除
│   ├── mkdir.go              # 创建目录
│   ├── login.go              # 登录登出
│   ├── admin.go              # 管理后台
│   ├── user.go               # 用户管理
│   ├── logs.go               # 日志查看
│   ├── info.go               # 服务器信息
│   ├── heatmap.go            # 热力图统计
│   ├── review.go             # 审核管理
│   ├── approve.go            # 审核通过
│   ├── reject.go             # 审核拒绝
│   ├── terms.go              # 免责协议
│   ├── stats.go              # 统计API
│   ├── category_handler.go   # 分类管理API
│   ├── download_management.go # 下载管理页面
│   └── daily_upload.go       # 每日上传统计
├── internal/                  # 内部模块
│   ├── api/                   # 下载任务API（Gin）
│   │   ├── server.go         # API服务器
│   │   ├── handlers.go       # API处理器
│   │   ├── middleware.go     # API中间件（鉴权、CSRF）
│   │   └── websocket.go      # WebSocket推送
│   ├── core/                  # 下载引擎核心
│   │   ├── engine.go         # 下载引擎
│   │   ├── models.go         # 数据模型
│   │   ├── protocol.go       # 协议接口
│   │   ├── protocol_manager.go # 协议管理器
│   │   ├── persistence.go    # 持久化
│   │   ├── chunk_strategy.go # 分块策略
│   │   └── resource_controller.go # 资源控制器
│   ├── config/                # 下载引擎配置
│   ├── event/                 # 事件系统
│   ├── logger/                # 日志系统
│   └── tui/                   # 终端UI
├── protocols/                 # 下载协议实现
│   ├── httpx/                 # HTTP/HTTPS协议
│   ├── bt/                    # BitTorrent协议
│   ├── ftp/                   # FTP协议
│   └── ed2k/                  # eDonkey2000协议
├── utils/                     # 工具函数
│   ├── utils.go              # 通用工具函数
│   ├── xss.go                # XSS防护工具
│   ├── csrf.go               # CSRF防护工具
│   ├── path_security.go      # 路径安全验证
│   ├── monitor.go            # 监控告警
│   ├── monitor_disk_windows.go # Windows磁盘监控
│   ├── monitor_disk_unix.go  # Linux/Mac磁盘监控
│   ├── log_management.go     # 日志管理
│   ├── shorturl.go           # 短链接管理
│   ├── category.go           # 分类管理
│   └── security_test.go      # 安全工具测试
├── static/                    # 静态资源
│   ├── styles.css            # 样式文件
│   └── js/                   # JavaScript文件
│       └── csrf.js           # CSRF令牌自动携带
├── config/                    # 配置文件目录
│   ├── config.json           # 主配置文件
│   ├── shorturls.json        # 短链接数据
│   ├── categories.json       # 分类数据
│   └── sort.json             # 自定义排序数据
├── downloads/                 # 下载文件目录
├── pending/                   # 待审核文件目录
├── logs/                      # 日志目录
├── CODE_STYLE.md             # 代码风格指南
├── README.md                  # 项目说明文档
└── go.mod                     # Go模块定义
```

## 🛠️ 技术栈

### 后端技术
- **后端语言**：Go 1.25+
- **Web框架**：标准库 `net/http`（主服务）+ Gin（下载任务API）
- **数据库**：JSON文件存储（无需外部数据库，轻量级部署）
- **WebSocket**：`github.com/gorilla/websocket`（实时推送下载进度）
- **日志库**：`github.com/sirupsen/logrus`（结构化日志）
- **配置管理**：`github.com/spf13/viper`（灵活的配置管理）
- **文件系统监控**：`github.com/fsnotify/fsnotify`（文件变更监控）
- **加密库**：`golang.org/x/crypto`（密码哈希、安全加密）
- **TUI框架**：`github.com/charmbracelet/bubbletea` + `bubbles`（终端用户界面）

### 前端技术
- **前端**：原生 HTML + CSS + JavaScript（无框架依赖，轻量级）
- **图表库**：Chart.js（本地部署，无外部CDN依赖）
- **响应式设计**：6级媒体查询断点，适配桌面端和移动端
- **全平台支持**：Windows、Linux、macOS、Android、iOS

### 下载协议
- **HTTP/HTTPS**：标准库实现，支持多线程加速与断点续传
- **BitTorrent**：`github.com/anacrolix/torrent`，支持磁力链接与种子文件
- **FTP**：标准库实现，PASV被动模式 + RETR流式拉取
- **eDonkey2000 (ED2K)**：原生实现，含ed2k哈希分片校验、服务器源发现、zlib解压

### 安全防护
- **XSS跨站脚本防护**：自定义实现，HTML编码、危险标签清理、URL验证
- **CSRF跨站请求伪造防护**：自定义实现，基于会话ID的令牌机制
- **路径遍历防护**：自定义实现，8步严格验证
- **会话管理**：HttpOnly、SameSite、Secure属性，24小时自动过期
- **密码安全**：哈希存储，独立API Key支持

### 监控与运维
- **系统监控**：自定义实现，CPU、内存、磁盘使用率实时监控
- **跨平台磁盘监控**：条件编译，Windows使用GetDiskFreeSpaceExW，Linux/Mac使用syscall.Statfs
- **告警系统**：支持配置告警阈值，超过阈值自动触发告警
- **Gzip压缩**：中间件实现，压缩HTML、CSS、JavaScript等文本资源
- **静态资源缓存**：根据文件扩展名设置不同缓存时间
- **容器化**：Docker支持，一键部署

### 测试与质量
- **测试框架**：标准库 `testing`
- **测试覆盖**：安全工具、路径验证、CSRF防护、XSS防护、短链安全、ED2K协议等
- **代码风格**：`CODE_STYLE.md` 规范，统一代码风格
- **并发安全**：`sync.RWMutex` 保护共享数据，原子写配置文件

## 🧪 测试

运行所有测试：
```bash
go test ./...
```

运行指定包的测试：
```bash
go test ./utils/...
go test ./handlers/...
go test ./protocols/ed2k/...
```

## 🤝 贡献指南

欢迎提交 Issue 和 Pull Request！


### 提交规范
- feat: 新功能
- fix: 修复bug
- docs: 文档更新
- style: 代码格式调整
- refactor: 代码重构
- test: 测试相关
- chore: 构建/工具/依赖相关

## 📄 许可证

本项目采用 MIT 许可证，详见 LICENSE 文件。

## 📞 联系方式

- GitHub Issues：[https://github.com/gomail1/go-download/issues](https://github.com/gomail1/go-download/issues)
- Docker Hub：[https://hub.docker.com/r/gomail1/go_downloader](https://hub.docker.com/r/gomail1/go_downloader)

---

**注意**：本项目仅供学习和研究使用，请遵守当地法律法规，不要用于非法用途。
