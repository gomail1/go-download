package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 文件缓存结构
// 用于缓存文件和目录的元信息，减少频繁的文件系统调用

type fileCacheItem struct {
	info     os.FileInfo
	modTime  time.Time
	children []os.DirEntry
}

var (
	fileCache            = make(map[string]*fileCacheItem)
	fileCacheMutex       sync.RWMutex
	cacheExpiration      = 5 * time.Minute  // 缓存过期时间
	cacheCleanupInterval = 30 * time.Minute // 缓存清理间隔
)

// getCachedFileInfo 从缓存中获取文件信息
func getCachedFileInfo(path string) (os.FileInfo, bool) {
	fileCacheMutex.RLock()
	defer fileCacheMutex.RUnlock()

	item, found := fileCache[path]
	if !found || time.Since(item.modTime) > cacheExpiration {
		return nil, false
	}

	return item.info, true
}

// setCachedFileInfo 将文件信息存入缓存
func setCachedFileInfo(path string, info os.FileInfo) {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	fileCache[path] = &fileCacheItem{
		info:    info,
		modTime: time.Now(),
	}
}

// getCachedDirChildren 从缓存中获取目录子项
// 注意：返回的是拷贝后的 slice，调用方可以安全地原地排序，
// 不会污染缓存中共享的 children（并发浏览/不同排序参数互相覆盖的问题）
func getCachedDirChildren(path string) ([]os.DirEntry, bool) {
	fileCacheMutex.RLock()
	defer fileCacheMutex.RUnlock()

	item, found := fileCache[path]
	if !found || time.Since(item.modTime) > cacheExpiration || item.children == nil {
		return nil, false
	}

	copied := make([]os.DirEntry, len(item.children))
	copy(copied, item.children)
	return copied, true
}

// setCachedDirChildren 将目录子项存入缓存
func setCachedDirChildren(path string, children []os.DirEntry) {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	item, found := fileCache[path]
	if !found {
		item = &fileCacheItem{
			modTime: time.Now(),
		}
		fileCache[path] = item
	}

	item.children = children
	item.modTime = time.Now()
}

// invalidateCache 使指定路径的缓存失效
func invalidateCache(path string) {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	delete(fileCache, path)
}

// invalidateCacheRecursive 使指定路径及其所有子路径的缓存失效
func invalidateCacheRecursive(path string) {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	// 删除指定路径的缓存
	delete(fileCache, path)

	// 删除所有以指定路径为前缀的子路径缓存
	for key := range fileCache {
		if strings.HasPrefix(key, path+string(os.PathSeparator)) {
			delete(fileCache, key)
		}
	}
}

// cleanupExpiredCache 清理过期的缓存项
func cleanupExpiredCache() {
	fileCacheMutex.Lock()
	defer fileCacheMutex.Unlock()

	var expiredKeys []string
	now := time.Now()

	// 找出所有过期的缓存项
	for key, item := range fileCache {
		if now.Sub(item.modTime) > cacheExpiration {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// 删除过期的缓存项
	for _, key := range expiredKeys {
		delete(fileCache, key)
	}

	if len(expiredKeys) > 0 {
		log.Printf("清理了 %d 个过期缓存项", len(expiredKeys))
	}
}

// StartCacheCleanupTask 启动定期缓存清理任务（导出函数，供外部调用）
func StartCacheCleanupTask() {
	go func() {
		ticker := time.NewTicker(cacheCleanupInterval)
		defer ticker.Stop()

		for {
			<-ticker.C
			cleanupExpiredCache()
		}
	}()
}

// 首页文件项结构
type HomeFileItem struct {
	Name             string
	Path             string
	Size             int64
	SizeStr          string
	Downloads        int64
	Icon             string
	IconURL          string // 真实图标URL（从可执行文件提取）
	Ext              string
	ModTime          string
	ModTimeTime      time.Time // 修改时间（time.Time格式，用于排序和筛选）
	LastDownloadTime time.Time // 最后下载时间（用于筛选近7天热门）
}

// 递归读取目录下所有文件
func getAllFilesInDir(dirPath string) []HomeFileItem {
	var items []HomeFileItem

	files, err := os.ReadDir(dirPath)
	if err != nil {
		return items
	}

	for _, file := range files {
		fullPath := filepath.Join(dirPath, file.Name())

		if file.IsDir() {
			// 递归读取子目录
			subItems := getAllFilesInDir(fullPath)
			items = append(items, subItems...)
		} else {
			info, err := file.Info()
			if err != nil {
				continue
			}

			// 计算相对路径（相对于 downloads 目录）
			relPath, err := filepath.Rel(config.AppConfig.Server.DownloadDir, fullPath)
			if err != nil {
				relPath = file.Name()
			}
			relPath = utils.NormalizePath(relPath)

			// 获取下载次数
			downloads := GetFileDownloadCount(relPath)
			// 获取最后下载时间
			lastDownloadTime := GetFileLastDownloadTime(relPath)

			// 获取文件扩展名
			ext := strings.ToLower(filepath.Ext(file.Name()))

			items = append(items, HomeFileItem{
				Name:             file.Name(),
				Path:             relPath,
				Size:             info.Size(),
				SizeStr:          utils.FormatFileSize(info.Size()),
				Downloads:        downloads,
				Icon:             getFileIcon(file.Name()),
				Ext:              ext,
				ModTime:          info.ModTime().Format("2006-01-02 15:04:05"),
				ModTimeTime:      info.ModTime(),
				LastDownloadTime: lastDownloadTime,
			})
		}
	}

	return items
}

// 获取文件分类
func getFileCategory(ext string) string {
	switch ext {
	case ".iso", ".img", ".dmg":
		return "系统镜像"
	case ".exe", ".msi", ".app", ".deb", ".rpm":
		return "常用软件"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2":
		return "压缩包"
	case ".pdf", ".doc", ".docx", ".txt", ".md", ".xls", ".xlsx", ".ppt", ".pptx":
		return "办公文档"
	case ".mp4", ".avi", ".mkv", ".mov", ".wmv":
		return "视频"
	case ".mp3", ".wav", ".flac", ".aac":
		return "音频"
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".svg":
		return "图片"
	case ".py", ".go", ".js", ".ts", ".java", ".c", ".cpp", ".php", ".rb":
		return "开发工具"
	default:
		return "其他"
	}
}

// 根据文件扩展名获取图标背景色class
func getFileIconClass(ext string) string {
	switch ext {
	case ".iso", ".img", ".dmg":
		return "icon-iso"
	case ".exe", ".msi", ".app", ".deb", ".rpm":
		return "icon-exe"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2":
		return "icon-zip"
	case ".pdf", ".doc", ".docx", ".txt", ".md", ".xls", ".xlsx", ".ppt", ".pptx":
		return "icon-pdf"
	default:
		return "icon-doc"
	}
}

// 根据文件是否有真实图标URL生成图标HTML
func getFileIconHTML(f HomeFileItem, iconClass string) string {
	if f.IconURL != "" {
		// 有真实图标，使用img标签显示
		return fmt.Sprintf(`<img src="%s" alt="%s" class="file-icon-real" style="width:100%%;height:100%%;object-fit:contain;">`, 
			utils.EscapeHTML(f.IconURL), utils.EscapeHTML(f.Name))
	}
	// 没有真实图标，使用默认图标
	return f.Icon
}

// 根据文件是否有真实图标URL生成浮动卡片图标HTML
func getFloatCardIconHTML(f HomeFileItem, iconColor string) string {
	if f.IconURL != "" {
		// 有真实图标，使用img标签显示，白色背景
		return fmt.Sprintf(`<img src="%s" alt="%s" style="width:100%%;height:100%%;object-fit:contain;background:white;border-radius:8px;">`, 
			utils.EscapeHTML(f.IconURL), utils.EscapeHTML(f.Name))
	}
	// 没有真实图标，使用默认图标和背景色
	return f.Icon
}

// 主页处理函数 - 展示型首页
func IndexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 取会话ID用于生成CSRF令牌（未登录时为空，页面不输出令牌）
	sessionID := utils.GetSessionIDFromRequest(r)

	// 获取搜索、分类、排序和分页参数
	searchQuery := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	sortQuery := r.URL.Query().Get("sort")
	page := 1
	pageSize := 12
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	// 读取所有文件
	allFiles := getAllFilesInDir(config.AppConfig.Server.DownloadDir)

	// 为可执行文件生成真实图标URL
	baseURL := fmt.Sprintf("%s://%s", utils.GetRequestScheme(r), r.Host)
	for i := range allFiles {
		if utils.IsExecutableFile(allFiles[i].Ext) {
			// 使用EncodePath对路径进行URL编码，确保包含空格、中文、特殊字符的路径能正常工作
			allFiles[i].IconURL = fmt.Sprintf("%s/icon?path=%s", baseURL, utils.EncodePath(allFiles[i].Path))
		}
	}

	// 筛选文件
	cm := utils.GetCategoryManager()
	var filteredFiles []HomeFileItem
	for _, f := range allFiles {
		// 搜索筛选
		if searchQuery != "" && !strings.Contains(strings.ToLower(f.Name), strings.ToLower(searchQuery)) {
			continue
		}
		// 分类筛选（根据分类ID）
		if category != "" {
			fileCategoryID := cm.GetFileCategory(f.Path)
			if fileCategoryID != category {
				continue
			}
		}
		filteredFiles = append(filteredFiles, f)
	}

	// 统计数据（基于全部文件）
	totalFiles := len(allFiles)
	totalDownloads := int64(0)
	totalSize := int64(0)
	for _, f := range allFiles {
		totalDownloads += f.Downloads
		totalSize += f.Size
	}

	// 生成热门下载文件（优先展示近7天有下载的，不足则补充历史最高的）
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	var recentHotFiles []HomeFileItem
	var historicalHotFiles []HomeFileItem
	for _, f := range allFiles {
		if f.Downloads > 0 && !f.LastDownloadTime.IsZero() && f.LastDownloadTime.After(sevenDaysAgo) {
			recentHotFiles = append(recentHotFiles, f)
		} else if f.Downloads > 0 {
			historicalHotFiles = append(historicalHotFiles, f)
		}
	}
	// 近7天热门按下载量排序
	sort.Slice(recentHotFiles, func(i, j int) bool {
		return recentHotFiles[i].Downloads > recentHotFiles[j].Downloads
	})
	// 历史热门按下载量排序
	sort.Slice(historicalHotFiles, func(i, j int) bool {
		return historicalHotFiles[i].Downloads > historicalHotFiles[j].Downloads
	})
	// 合并：近7天热门优先，不足6个则补充历史热门
	hotFiles := recentHotFiles
	if len(hotFiles) < 6 {
		existingPaths := make(map[string]bool)
		for _, f := range hotFiles {
			existingPaths[f.Path] = true
		}
		for _, f := range historicalHotFiles {
			if len(hotFiles) >= 6 {
				break
			}
			if !existingPaths[f.Path] {
				hotFiles = append(hotFiles, f)
				existingPaths[f.Path] = true
			}
		}
	}
	if len(hotFiles) > 6 {
		hotFiles = hotFiles[:6]
	}

	// 生成Hero区域浮动文件卡片HTML（使用实际的热门文件）
	heroFloatCardsHTML := ""
	if len(hotFiles) > 0 {
		floatCardCount := 3
		if len(hotFiles) < floatCardCount {
			floatCardCount = len(hotFiles)
		}
		floatCardColors := []string{
			"background: #D1E9FF; color: #155EEF;",
			"background: #FCE7F6; color: #C11574;",
			"background: #D1FADF; color: #027A48;",
		}
		floatCardPositions := []string{"float-card-1", "float-card-2", "float-card-3"}
		for i := 0; i < floatCardCount; i++ {
			f := hotFiles[i]
			iconColor := floatCardColors[i%len(floatCardColors)]
			cardClass := floatCardPositions[i%len(floatCardPositions)]
			// 真实图标时添加has-real-icon类
			floatRealIconClass := ""
			floatIconStyle := iconColor
			if f.IconURL != "" {
				floatRealIconClass = " has-real-icon"
				floatIconStyle = ""
			}
			heroFloatCardsHTML += fmt.Sprintf(`<div class="float-card %s">
					<div class="float-card-icon%s" style="%s">%s</div>
					<div class="float-card-info">
						<div class="float-card-name">%s</div>
						<div class="float-card-meta">%s · %d 下载</div>
					</div>
				</div>`, cardClass, floatRealIconClass, floatIconStyle, getFloatCardIconHTML(f, iconColor), utils.EscapeHTML(f.Name), f.SizeStr, f.Downloads)
		}
	}

	// 生成最新上传文件（优先展示近7天上传的，不足则补充历史最新的）
	var recentLatestFiles []HomeFileItem
	var historicalLatestFiles []HomeFileItem
	for _, f := range allFiles {
		if f.ModTimeTime.After(sevenDaysAgo) {
			recentLatestFiles = append(recentLatestFiles, f)
		} else {
			historicalLatestFiles = append(historicalLatestFiles, f)
		}
	}
	// 近7天最新上传按时间倒序
	sort.Slice(recentLatestFiles, func(i, j int) bool {
		return recentLatestFiles[i].ModTimeTime.After(recentLatestFiles[j].ModTimeTime)
	})
	// 历史最新上传按时间倒序
	sort.Slice(historicalLatestFiles, func(i, j int) bool {
		return historicalLatestFiles[i].ModTimeTime.After(historicalLatestFiles[j].ModTimeTime)
	})
	// 合并：近7天最新优先，不足6个则补充历史最新
	latestFiles := recentLatestFiles
	if len(latestFiles) < 6 {
		existingPaths := make(map[string]bool)
		for _, f := range latestFiles {
			existingPaths[f.Path] = true
		}
		for _, f := range historicalLatestFiles {
			if len(latestFiles) >= 6 {
				break
			}
			if !existingPaths[f.Path] {
				latestFiles = append(latestFiles, f)
				existingPaths[f.Path] = true
			}
		}
	}
	if len(latestFiles) > 6 {
		latestFiles = latestFiles[:6]
	}

	// 生成热门下载HTML（搜索和排序查看时不显示）
	hotFilesHTML := ""
	if len(hotFiles) > 0 && searchQuery == "" && sortQuery == "" {
		hotFilesHTML = `<div class="section-header">
				<h2 class="section-title">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
					热门下载
				</h2>
				<a href="/?sort=downloads" class="section-more">查看全部
					<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
				</a>
			</div>
			<div class="file-grid file-grid-compact">`
		for i, f := range hotFiles {
			downloadURL := "/download?path=" + utils.EncodePath(f.Path)
			iconClass := getFileIconClass(f.Ext)
			rankBadge := ""
			if i < 3 {
				colors := []string{"#ff6b6b", "#ffa94d", "#ffd43b"}
				rankBadge = fmt.Sprintf(`<span class="hot-rank-badge" style="background: %s;">%d</span>`, colors[i], i+1)
			}
			// 真实图标时添加has-real-icon类
			hotRealIconClass := ""
			if f.IconURL != "" {
				hotRealIconClass = " has-real-icon"
			}
			hotFilesHTML += fmt.Sprintf(`<div class="file-card file-card-horizontal">
				<div class="file-card-icon %s%s">%s</div>
				<div class="file-card-info">
					<div class="file-card-name">%s%s</div>
					<div class="file-card-meta">
						<span>%s</span>
						<span class="file-card-meta-dot"></span>
						<span>%d 次下载</span>
					</div>
				</div>
				<a href="%s" class="file-card-btn">下载</a>
			</div>`, iconClass, hotRealIconClass, getFileIconHTML(f, iconClass), rankBadge, utils.EscapeHTML(f.Name), f.SizeStr, f.Downloads, downloadURL)
		}
		hotFilesHTML += `</div>`
	}

	// 生成最新上传HTML（搜索和排序查看时不显示）
	latestFilesHTML := ""
	if len(latestFiles) > 0 && searchQuery == "" && sortQuery == "" {
		latestFilesHTML = `<div class="section-header" style="margin-top: 40px;">
				<h2 class="section-title">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
					最新上传
				</h2>
				<a href="/?sort=latest" class="section-more">查看全部
					<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
				</a>
			</div>
			<div class="file-grid file-grid-compact">`
		for _, f := range latestFiles {
			downloadURL := "/download?path=" + utils.EncodePath(f.Path)
			iconClass := getFileIconClass(f.Ext)
			// 真实图标时添加has-real-icon类
			latestRealIconClass := ""
			if f.IconURL != "" {
				latestRealIconClass = " has-real-icon"
			}
			latestFilesHTML += fmt.Sprintf(`<div class="file-card file-card-horizontal">
				<div class="file-card-icon %s%s">%s</div>
				<div class="file-card-info">
					<div class="file-card-name">%s</div>
					<div class="file-card-meta">
						<span>%s</span>
						<span class="file-card-meta-dot"></span>
						<span>%s</span>
					</div>
				</div>
				<a href="%s" class="file-card-btn">下载</a>
			</div>`, iconClass, latestRealIconClass, getFileIconHTML(f, iconClass), utils.EscapeHTML(f.Name), f.SizeStr, f.ModTime, downloadURL)
		}
		latestFilesHTML += `</div>`
	}

	// 根据排序参数排序
	switch sortQuery {
	case "latest":
		// 按上传时间（修改时间）排序，最新的在前
		sort.Slice(filteredFiles, func(i, j int) bool {
			return filteredFiles[i].ModTime > filteredFiles[j].ModTime
		})
	case "downloads":
		fallthrough
	default:
		// 按下载次数排序（默认），下载最多的在前
		sort.Slice(filteredFiles, func(i, j int) bool {
			return filteredFiles[i].Downloads > filteredFiles[j].Downloads
		})
	}

	// 分页计算
	totalFiltered := len(filteredFiles)
	totalPages := (totalFiltered + pageSize - 1) / pageSize
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}
	startIndex := (page - 1) * pageSize
	endIndex := startIndex + pageSize
	if endIndex > totalFiltered {
		endIndex = totalFiltered
	}

	// 获取当前页的文件
	pagedFiles := filteredFiles
	if startIndex < totalFiltered {
		pagedFiles = filteredFiles[startIndex:endIndex]
	}

	// 生成排序状态栏HTML
	sortBarHTML := ""
	if len(filteredFiles) > 0 {
		// 构建基础URL（保留搜索和分类参数）
		sortBaseURL := "/?"
		if searchQuery != "" {
			sortBaseURL += "search=" + url.QueryEscape(searchQuery) + "&"
		}
		if category != "" {
			sortBaseURL += "category=" + url.QueryEscape(category) + "&"
		}
		
		// 当前排序方式
		currentSort := "downloads"
		if sortQuery == "latest" {
			currentSort = "latest"
		}
		
		// 排序按钮
		downloadsActive := ""
		latestActive := ""
		if currentSort == "downloads" {
			downloadsActive = "sort-btn-active"
		} else {
			latestActive = "sort-btn-active"
		}
		
		sortBarHTML = fmt.Sprintf(`<div class="sort-bar">
			<span class="sort-bar-label">排序：</span>
			<a href="%ssort=downloads" class="sort-btn %s">🔥 热门下载</a>
			<a href="%ssort=latest" class="sort-btn %s">🕐 最新上传</a>
			<span class="sort-bar-count">共 %d 个文件</span>
		</div>`, sortBaseURL, downloadsActive, sortBaseURL, latestActive, totalFiltered)
	}

	// 生成文件卡片HTML
	var filesHTML string
	if len(pagedFiles) == 0 {
		filesHTML = `<div class="empty-state-v2" style="grid-column: 1/-1;">
			<div class="empty-state-icon-v2">📭</div>
			<div class="empty-state-title-v2">暂无文件</div>
			<div class="empty-state-desc-v2">没有找到匹配的文件</div>
		</div>`
	} else {
		for _, f := range pagedFiles {
			downloadURL := "/download?path=" + utils.EncodePath(f.Path)
			iconClass := getFileIconClass(f.Ext)
			// 从分类管理器获取文件的实际分类
			fileCategoryID := cm.GetFileCategory(f.Path)
			fileCategoryName := "未分类"
			if cat, err := cm.GetCategoryByID(fileCategoryID); err == nil && fileCategoryID != "default" {
				fileCategoryName = cat.Name
			}
			// XSS防护：对用户可控的输出进行HTML编码
			safeFileName := utils.EscapeHTML(f.Name)
			safeFilePath := utils.EscapeHTML(f.Path)
			safeCategoryName := utils.EscapeHTML(fileCategoryName)
			// 真实图标时添加has-real-icon类
			realIconClass := ""
			if f.IconURL != "" {
				realIconClass = " has-real-icon"
			}
			filesHTML += fmt.Sprintf(`<div class="file-card">
				<div class="file-card-icon %s%s">%s</div>
				<div class="file-card-name">%s</div>
				<div class="file-card-meta">
					<span>%s</span>
					<span class="file-card-meta-dot"></span>
					<span>%s</span>
				</div>
				<div class="file-card-footer">
					<div class="file-card-downloads">
						<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
						%d
					</div>
					<div class="file-card-actions">
						<button class="file-card-btn file-card-share-btn" data-path="%s" data-filename="%s">
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>
							分享
						</button>
						<a href="%s" class="file-card-btn">下载</a>
					</div>
				</div>
			</div>`, iconClass, realIconClass, getFileIconHTML(f, iconClass), safeFileName, f.SizeStr, safeCategoryName, f.Downloads, safeFilePath, safeFileName, downloadURL)
		}
	}

	// 生成分页控件HTML
	paginationHTML := ""
	if totalPages > 1 {
		// 构建基础URL（保留搜索、分类和每页数量参数）
		baseURL := "/?"
		if searchQuery != "" {
			baseURL += "search=" + url.QueryEscape(searchQuery) + "&"
		}
		if category != "" {
			baseURL += "category=" + url.QueryEscape(category) + "&"
		}
		if sortQuery != "" {
			baseURL += "sort=" + url.QueryEscape(sortQuery) + "&"
		}
		if pageSize != 12 {
			baseURL += "page_size=" + strconv.Itoa(pageSize) + "&"
		}

		paginationHTML = `<div class="pagination-v2">`

		// 上一页按钮
		if page > 1 {
			paginationHTML += fmt.Sprintf(`<a href="%spage=%d" class="pagination-btn-v2 pagination-prev-v2">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg>
				上一页
			</a>`, baseURL, page-1)
		} else {
			paginationHTML += `<span class="pagination-btn-v2 pagination-disabled-v2">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg>
				上一页
			</span>`
		}

		// 页码按钮（显示当前页附近的页码）
		paginationHTML += `<div class="pagination-pages-v2">`
		maxVisible := 5
		startPage := page - maxVisible/2
		if startPage < 1 {
			startPage = 1
		}
		endPage := startPage + maxVisible - 1
		if endPage > totalPages {
			endPage = totalPages
			startPage = endPage - maxVisible + 1
			if startPage < 1 {
				startPage = 1
			}
		}

		// 第一页和省略号
		if startPage > 1 {
			paginationHTML += fmt.Sprintf(`<a href="%spage=1" class="pagination-page-v2">1</a>`, baseURL)
			if startPage > 2 {
				paginationHTML += `<span class="pagination-ellipsis-v2">...</span>`
			}
		}

		for i := startPage; i <= endPage; i++ {
			if i == page {
				paginationHTML += fmt.Sprintf(`<span class="pagination-page-v2 pagination-active-v2">%d</span>`, i)
			} else {
				paginationHTML += fmt.Sprintf(`<a href="%spage=%d" class="pagination-page-v2">%d</a>`, baseURL, i, i)
			}
		}

		// 最后一页和省略号
		if endPage < totalPages {
			if endPage < totalPages-1 {
				paginationHTML += `<span class="pagination-ellipsis-v2">...</span>`
			}
			paginationHTML += fmt.Sprintf(`<a href="%spage=%d" class="pagination-page-v2">%d</a>`, baseURL, totalPages, totalPages)
		}

		paginationHTML += `</div>`

		// 下一页按钮
		if page < totalPages {
			paginationHTML += fmt.Sprintf(`<a href="%spage=%d" class="pagination-btn-v2 pagination-next-v2">
				下一页
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
			</a>`, baseURL, page+1)
		} else {
			paginationHTML += `<span class="pagination-btn-v2 pagination-disabled-v2">
				下一页
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
			</span>`
		}

		// 分页信息
		paginationHTML += fmt.Sprintf(`<span class="pagination-info-v2">第 %d / %d 页，共 %d 个文件</span>`, page, totalPages, totalFiltered)

		paginationHTML += `</div>`
	}

	// 生成分类标签HTML（从后端动态获取）
	categories := cm.GetCategories()
	var categoryTabsHTML string
	for _, cat := range categories {
		active := ""
		if (category == "" && cat.ID == "default") || category == cat.ID {
			active = "active"
		}
		link := "/"
		if cat.ID != "default" {
			link = "/?category=" + url.QueryEscape(cat.ID)
			if searchQuery != "" {
				link += "&search=" + url.QueryEscape(searchQuery)
			}
		} else if searchQuery != "" {
			link = "/?search=" + url.QueryEscape(searchQuery)
		}
		categoryTabsHTML += fmt.Sprintf(`<a href="%s" class="category-tab %s">%s %s</a>`, link, active, utils.EscapeHTML(cat.Icon), utils.EscapeHTML(cat.Name))
	}

	// 搜索结果标题和分类标签显示控制
	var fileSectionTitleHTML string
	var categoryTabsSectionHTML string
	if searchQuery != "" {
		// 搜索模式：显示搜索结果标题和结果数量
		fileSectionTitleHTML = fmt.Sprintf(`<div class="section-header" style="margin-top: 40px;">
				<h2 class="section-title">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
					搜索结果：<span style="color: #155EEF;">"%s"</span>
					<span style="font-size: 14px; color: #667085; font-weight: normal; margin-left: 8px;">共 %d 个结果</span>
				</h2>
			</div>`, utils.EscapeHTML(searchQuery), totalFiltered)
		// 搜索时隐藏分类标签
		categoryTabsSectionHTML = ""
	} else {
		// 正常模式：显示全部文件标题和分类标签
		fileSectionTitleHTML = `<div class="section-header" style="margin-top: 40px;">
				<h2 class="section-title">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
					全部文件
				</h2>
			</div>`
		categoryTabsSectionHTML = `<div class="category-tabs">
				` + categoryTabsHTML + `
			</div>`
	}

	// 构建HTML
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	` + utils.GenerateCSRFTokenMeta(sessionID) + `
	<script src="/static/js/csrf.js"></script>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>` + config.AppConfig.Server.ServerName + ` - 优质资源下载</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">
</head>
<body class="v2">
	<!-- Hero 区域 -->
	<section class="hero-home">
		<div class="hero-home-content">
			<div class="hero-home-badge">
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
				高速稳定 · 免费下载
			</div>
			<h1 class="hero-home-title">优质资源，<span class="gradient-text">一键下载</span></h1>
			<p class="hero-home-desc">汇集系统镜像、开发工具、常用软件等优质资源，高速稳定，免费下载。</p>
			<div class="hero-home-search">
				<form action="/" method="GET">
					<span class="hero-search-icon">
						<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
					</span>
					<input type="text" name="search" placeholder="搜索你需要的文件，例如：debian、wps、git..." value="` + utils.EscapeHTML(searchQuery) + `">
					<button type="submit">搜索</button>
				</form>
			</div>
			<div class="hero-home-stats">
				<div class="hero-home-stat">
					<div class="hero-home-stat-num">` + fmt.Sprintf("%d", totalFiles) + `</div>
					<div class="hero-home-stat-label">优质资源</div>
				</div>
				<div class="hero-home-stat">
					<div class="hero-home-stat-num">` + fmt.Sprintf("%d", totalDownloads) + `</div>
					<div class="hero-home-stat-label">累计下载</div>
				</div>
				<div class="hero-home-stat">
					<div class="hero-home-stat-num">` + utils.FormatFileSize(totalSize) + `</div>
					<div class="hero-home-stat-label">总容量</div>
				</div>
			</div>
		</div>
	</section>

	<!-- 主内容区 -->
	<main class="page-main">
		<!-- 热门下载 -->
		` + hotFilesHTML + `

		<!-- 最新上传 -->
		` + latestFilesHTML + `

		<!-- 全部文件 / 搜索结果 -->
		` + fileSectionTitleHTML + `

		<!-- 分类标签（搜索时隐藏） -->
		` + categoryTabsSectionHTML + `

		<!-- 排序状态栏 -->
		` + sortBarHTML + `

		<div class="file-grid">
			` + filesHTML + `
		</div>

		` + paginationHTML + `
	</main>

	<!-- 页脚 -->
	<footer class="footer-v2">
		<div class="footer-content-v2">
			` + func() string {
		if session.GetCurrentUser(r) != nil {
			return `<div>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + ` | <a href="` + constants.RepoURL + `" target="_blank">GitHub</a></div>
<div class="footer-links-v2"><a href="/terms">使用条款</a><a href="#">隐私政策</a></div>`
		}
		return `<div>` + config.AppConfig.Legal.FooterText + `</div><div>` + config.AppConfig.Legal.BrowserTips + `</div>`
	}() + `
		</div>
	</footer>
	<script>
	document.addEventListener('DOMContentLoaded', function() {
		// 滚动位置记忆功能
		var scrollPos = sessionStorage.getItem('homeScrollPos');
		if (scrollPos !== null) {
			window.scrollTo(0, parseInt(scrollPos));
			sessionStorage.removeItem('homeScrollPos');
		}

		// 分类标签点击时保存滚动位置
		var categoryTabs = document.querySelectorAll('.category-tab');
		categoryTabs.forEach(function(tab) {
			tab.addEventListener('click', function() {
				sessionStorage.setItem('homeScrollPos', window.scrollY.toString());
			});
		});

		// 分页链接点击时保存滚动位置
		var paginationLinks = document.querySelectorAll('.pagination-page-v2, .pagination-prev-v2, .pagination-next-v2');
		paginationLinks.forEach(function(link) {
			link.addEventListener('click', function() {
				sessionStorage.setItem('homeScrollPos', window.scrollY.toString());
			});
		});

		var shareButtons = document.querySelectorAll('.file-card-share-btn');
		shareButtons.forEach(function(btn) {
			btn.addEventListener('click', function() {
				var path = this.getAttribute('data-path');
				var filename = this.getAttribute('data-filename');
				var originalHTML = this.innerHTML;
				this.innerHTML = '生成中...';
				this.disabled = true;
				fetch('/api/generate-short-url?path=' + encodeURIComponent(path) + '&filename=' + encodeURIComponent(filename), {method: 'POST'})
				.then(function(res) { return res.json(); })
				.then(function(data) {
					if (data.success) {
						navigator.clipboard.writeText(data.full_url).then(function() {
							btn.innerHTML = '已复制';
							btn.style.background = '#12B76A';
							setTimeout(function() { btn.innerHTML = originalHTML; btn.style.background = ''; btn.disabled = false; }, 2000);
						}).catch(function() { alert('短链：' + data.full_url); btn.innerHTML = originalHTML; btn.disabled = false; });
					} else {
						alert('失败：' + (data.error || '未知错误'));
						btn.innerHTML = originalHTML;
						btn.disabled = false;
					}
				})
				.catch(function(err) { alert('失败：' + err.message); btn.innerHTML = originalHTML; btn.disabled = false; });
			});
		});
	});
	</script>
</body>
</html>`

	w.Write([]byte(html))
}

// 获取上传统计信息的HTML
func getUploadStatsHTML(sess *session.Session) string {
	if sess == nil {
		return ""
	}

	// 获取今日已上传大小
	uploaded := GetDailyUpload(sess.Username)

	// 构建统计文本
	if sess.Username == "admin" {
		return "今日上传: 无限制"
	} else {
		return fmt.Sprintf("今日上传: %s / %s", utils.FormatFileSize(uploaded), utils.FormatFileSize(constants.DailyUploadLimit))
	}
}

// 递归搜索文件函数
type SearchResult struct {
	Name         string
	Path         string
	IsDir        bool
	Size         int64
	ModTime      string
	RelativePath string
}

func recursiveSearch(directory string, searchQuery string) []SearchResult {
	var results []SearchResult

	// 遍历目录
	files, err := os.ReadDir(directory)
	if err != nil {
		return results
	}

	for _, file := range files {
		filePath := filepath.Join(directory, file.Name())

		// 检查文件名是否匹配搜索关键词
		if strings.Contains(strings.ToLower(file.Name()), strings.ToLower(searchQuery)) {
			info, err := file.Info()
			if err == nil {
				results = append(results, SearchResult{
					Name:         file.Name(),
					Path:         filePath,
					IsDir:        file.IsDir(),
					Size:         info.Size(),
					ModTime:      info.ModTime().Format("2006-01-02 15:04:05"),
					RelativePath: filePath,
				})
			}
		}

		// 如果是目录，递归搜索
		if file.IsDir() {
			subResults := recursiveSearch(filePath, searchQuery)
			results = append(results, subResults...)
		}
	}

	return results
}

// 生成搜索结果的HTML
type ResultItem struct {
	Name         string
	Path         string
	IsDir        bool
	Size         int64
	ModTime      string
	RelativePath string
}

func generateSearchResults(r *http.Request, results []SearchResult, basePath string) string {
	var resultHTML string

	// 获取当前用户
	sess := session.GetCurrentUser(r)

	// 为管理员和二级管理员添加批量操作按钮
	var batchActions string
	if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
		// 批量操作按钮（搜索结果暂时不支持批量操作）
		batchActions = "<div class='batch-actions' style='margin-bottom: 20px; color: #666;'>搜索结果不支持批量操作</div>"
	}

	// 构建结果列表
	resultHTML += batchActions
	resultHTML += "<div class='search-results'>"

	if len(results) == 0 {
		resultHTML += "<div style='text-align: center; padding: 40px; color: #666;'>未找到匹配的文件或目录</div>"
	} else {
		for _, result := range results {
			// 生成相对路径
			relPath, err := filepath.Rel(basePath, result.Path)
			if err != nil {
				relPath = result.Path
			}

			// 生成文件图标
			var icon string
			if result.IsDir {
				icon = "📁"
			} else {
				icon = getFileIcon(result.Name)
			}

			// 生成文件元信息
			var meta string
			if result.IsDir {
				meta = "目录 • " + result.ModTime
			} else {
				meta = fmt.Sprintf("文件 • %s • %s", utils.FormatFileSize(result.Size), result.ModTime)
			}

			// 生成文件链接
			fileURL := utils.EncodePath(relPath)
			var fileLink string
			if result.IsDir {
				fileLink = fmt.Sprintf("<a href='/files?path=%s'>%s</a>", fileURL, utils.EscapeHTML(result.Name))
			} else {
				// 将文件名显示为普通文本，而不是链接
				fileLink = utils.EscapeHTML(result.Name)
			}

			// 生成相对路径显示
			var pathDisplay string
			if relPath == result.Name {
				pathDisplay = "根目录"
			} else {
				pathDisplay = filepath.Dir(relPath)
				if pathDisplay == "." {
					pathDisplay = "根目录"
				}
			}

			// 生成结果项
			var actionsHTML string
			if !result.IsDir {
				// 为文件添加下载和分享按钮
				actionsHTML = fmt.Sprintf(`<a href='/download?path=%s' class='btn btn-secondary'>下载</a>
                <button onclick="shareFile('%s')" class='btn btn-primary'>分享</button>`, fileURL, fileURL)
			}

			item := fmt.Sprintf(`<div class='file-item'>
                <div class='file-item-content'>
                    <div class='file-icon'>%s</div>
                    <div class='file-info'>
                        <div class='file-name'>%s</div>
                        <div class='file-meta'>%s • 位置: %s</div>
                    </div>
                </div>
                <div class='file-actions'>
                    %s
                </div>
            </div>`, icon, fileLink, meta, pathDisplay, actionsHTML)

			resultHTML += item
		}
	}

	// 添加分享功能脚本
	shareScript := `<script>
		// 分享文件功能
		function shareFile(filePath) {
			if (filePath) {
				// 直接使用已经编码过的filePath，避免双重编码问题
				const decodedFilePath = decodeURIComponent(filePath);
				const baseUrl = window.location.origin;
				const shareUrl = baseUrl + '/download?path=' + filePath;
				
				// 创建显示通知的辅助函数
				function showNotification(message, isSuccess) {
					// 移除任何现有的通知
					const existingNotification = document.querySelector('.share-notification');
					if (existingNotification) {
						existingNotification.remove();
					}
					
					// 创建新的通知元素
					const notification = document.createElement('div');
					notification.className = 'share-notification';
					notification.textContent = message;
					notification.style.cssText = "position:fixed; top:50%; left:50%; transform:translate(-50%, -50%); background:" + (isSuccess ? "#4CAF50" : "#f44336") + "; color:white; padding:20px; border-radius:5px; z-index:9999; box-shadow:0 2px 20px rgba(0,0,0,0.3); font-size:16px; text-align:center;";
					document.body.appendChild(notification);
					
					// 3秒后移除提示
					setTimeout(() => {
						if (notification.parentNode) {
							notification.remove();
						}
					}, 3000);
				}
				
				// 生成短链
				fetch('/api/generate-short-url?path=' + filePath + '&filename=' + encodeURIComponent(decodedFilePath.split('/').pop()), {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json'
					}
				}).then(response => response.json()).then(data => {
					if (data.success) {
						// 构建带有文件名的短链
						const fullShortURL = baseUrl + data.short_url;
						const filename = data.filename;
						const finalShareUrl = filename + ': ' + fullShortURL;
						
						// 复制到剪贴板
						if (navigator.clipboard) {
							navigator.clipboard.writeText(finalShareUrl).then(function() {
								// 显示成功通知
								showNotification('分享短链已复制到剪贴板！', true);
								
								// 直接调用后端API更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (!response.ok) {
										console.error('更新分享计数失败：', response.status);
										// 显示错误提示给用户
										showNotification('分享计数更新失败，请稍后重试', false);
									} else {
										console.log('分享计数更新成功');
									}
								}).catch(function(err) {
									console.error('更新分享计数失败：', err);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								});
							}).catch(function(err) {
								console.error('复制失败：', err);
								alert('分享链接复制失败，请手动复制：' + finalShareUrl);
								// 即使复制失败，仍然更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (response.ok) {
										console.log('分享计数更新成功（复制失败后）');
									} else {
										console.error('更新分享计数失败（复制失败后）：', response.status);
									}
								}).catch(function(err) {
									console.error('更新分享计数失败（复制失败后）：', err);
								});
							});
						} else {
							// 不支持剪贴板API的浏览器备用方案
							alert('您的浏览器不支持自动复制，请手动复制链接：' + finalShareUrl);
							// 仍然尝试更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（不支持剪贴板）');
								} else {
									console.error('更新分享计数失败（不支持剪贴板）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（不支持剪贴板）：', err);
							});
						}
					} else {
						// 生成短链失败：提示具体原因（未登录 / 路径非法 / 限流 / 配额），再回退为原始链接
						showNotification(data.error || '生成短链失败，已改为分享原始链接', false);
						if (navigator.clipboard) {
							navigator.clipboard.writeText(shareUrl).then(function() {
								// 显示成功通知
								showNotification('分享链接已复制到剪贴板！', true);
								
								// 直接调用后端API更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (!response.ok) {
										console.error('更新分享计数失败：', response.status);
										// 显示错误提示给用户
										showNotification('分享计数更新失败，请稍后重试', false);
									} else {
										console.log('分享计数更新成功');
									}
								}).catch(function(err) {
									console.error('更新分享计数失败：', err);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								});
							}).catch(function(err) {
								console.error('复制失败：', err);
								alert('分享链接复制失败，请手动复制：' + shareUrl);
								// 即使复制失败，仍然更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (response.ok) {
										console.log('分享计数更新成功（复制失败后）');
									} else {
										console.error('更新分享计数失败（复制失败后）：', response.status);
									}
								}).catch(function(err) {
									console.error('更新分享计数失败（复制失败后）：', err);
								});
							});
						} else {
							// 不支持剪贴板API的浏览器备用方案
							alert('您的浏览器不支持自动复制，请手动复制链接：' + shareUrl);
							// 仍然尝试更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（不支持剪贴板）');
								} else {
									console.error('更新分享计数失败（不支持剪贴板）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（不支持剪贴板）：', err);
							});
						}
					}
				}).catch(function(err) {
					console.error('生成短链失败：', err);
					// 生成短链失败，使用原始链接
					if (navigator.clipboard) {
						navigator.clipboard.writeText(shareUrl).then(function() {
							// 显示成功通知
							showNotification('分享链接已复制到剪贴板！', true);
							
							// 直接调用后端API更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (!response.ok) {
									console.error('更新分享计数失败：', response.status);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								} else {
									console.log('分享计数更新成功');
								}
							}).catch(function(err) {
								console.error('更新分享计数失败：', err);
								// 显示错误提示给用户
								showNotification('分享计数更新失败，请稍后重试', false);
							});
						}).catch(function(err) {
							console.error('复制失败：', err);
							alert('分享链接复制失败，请手动复制：' + shareUrl);
							// 即使复制失败，仍然更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（复制失败后）');
								} else {
									console.error('更新分享计数失败（复制失败后）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（复制失败后）：', err);
							});
						});
					} else {
						// 不支持剪贴板API的浏览器备用方案
						alert('您的浏览器不支持自动复制，请手动复制链接：' + shareUrl);
						// 仍然尝试更新分享计数
						fetch('/api/increment-share?path=' + filePath, {
							method: 'POST',
							headers: {
								'Content-Type': 'application/json'
							}
						}).then(response => {
							if (response.ok) {
								console.log('分享计数更新成功（不支持剪贴板）');
							} else {
								console.error('更新分享计数失败（不支持剪贴板）：', response.status);
							}
						}).catch(function(err) {
							console.error('更新分享计数失败（不支持剪贴板）：', err);
						});
					}
				});
			}
		}
	</script>`

	resultHTML += shareScript
	resultHTML += "</div>"

	return resultHTML
}

// 文件列表处理函数
func FilesHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户会话和协议同意状态
	sess := session.GetCurrentUser(r)
	if sess == nil {
		// 用户未登录，重定向到登录页面
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	if sess != nil && !sess.AgreedToTerms {
		// 用户已登录但未同意协议，重定向到协议页面
		http.Redirect(w, r, "/terms", http.StatusFound)
		return
	}

	// 取会话ID用于生成CSRF令牌（本页所有AJAX请求都会自动带上该令牌）
	sessionID := utils.GetSessionIDFromRequest(r)

	// 获取当前路径
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// 获取搜索关键词
	searchQuery := r.URL.Query().Get("search")
	searchQuery = strings.TrimSpace(searchQuery)

	// URL解码路径
	var err error
	path, err = utils.DecodePath(path)
	if err != nil {
		log.Printf("路径解码失败: %v, 使用默认路径", err)
		path = "."
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 记录日志
	if searchQuery != "" {
		utils.LogUserAction(r, "search_files", fmt.Sprintf("搜索文件，路径: %s, 关键词: %s", path, searchQuery))
	} else {
		utils.LogUserAction(r, "view_files", fmt.Sprintf("访问文件列表，路径: %s", path))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建完整路径
	fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, path)

	// 初始化变量
	var files []os.DirEntry
	var searchResults []SearchResult
	var isSearch bool = searchQuery != ""

	if isSearch {
		// 使用递归搜索整个目录树
		searchResults = recursiveSearch(fullPath, searchQuery)
		// 对搜索结果进行排序：目录在前，文件在后，按修改时间倒序
		sort.Slice(searchResults, func(i, j int) bool {
			// 目录在前，文件在后
			if searchResults[i].IsDir && !searchResults[j].IsDir {
				return true
			}
			if !searchResults[i].IsDir && searchResults[j].IsDir {
				return false
			}
			// 同类型按修改时间倒序排列（最新的在前）
			// 由于ModTime是字符串格式，需要先解析为time.Time类型
			timeI, _ := time.Parse("2006-01-02 15:04:05", searchResults[i].ModTime)
			timeJ, _ := time.Parse("2006-01-02 15:04:05", searchResults[j].ModTime)
			return timeI.After(timeJ)
		})
	} else {
		// 普通目录浏览 - 优先使用缓存
		var err error
		var found bool
		files, found = getCachedDirChildren(fullPath)
		if !found {
			// 缓存未命中，从磁盘读取
			files, err = os.ReadDir(fullPath)
			if err != nil {
				log.Printf("读取目录失败: %v", err)
				http.Error(w, fmt.Sprintf("无法读取目录: %v", err), http.StatusInternalServerError)
				return
			}
			// 将结果存入缓存
			setCachedDirChildren(fullPath, files)
		}
	}

	// 获取排序参数
	sortBy := r.URL.Query().Get("sort_by")
	sortOrder := r.URL.Query().Get("sort_order")

	// 默认排序：目录在前，文件在后
	sort.Slice(files, func(i, j int) bool {
		// 获取文件信息
		infoI, errI := files[i].Info()
		infoJ, errJ := files[j].Info()

		// 错误处理：出错的文件放在后面
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}

		// 目录在前，文件在后
		if files[i].IsDir() && !files[j].IsDir() {
			return true
		}
		if !files[i].IsDir() && files[j].IsDir() {
			return false
		}

		// 同类型根据排序参数进行排序
		// 只有管理员和二级管理员可以使用自定义排序
		isAdmin := sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin)

		if isAdmin {
			// 管理员自定义排序
			switch sortBy {
			case "name":
				// 按名称排序
				if sortOrder == "desc" {
					return files[i].Name() > files[j].Name()
				}
				return files[i].Name() < files[j].Name()
			case "size":
				// 按大小排序
				if sortOrder == "desc" {
					return infoI.Size() > infoJ.Size()
				}
				return infoI.Size() < infoJ.Size()
			case "type":
				// 按类型排序（目录优先，文件按扩展名）
				if files[i].IsDir() && files[j].IsDir() {
					// 两个都是目录，按名称
					if sortOrder == "desc" {
						return files[i].Name() > files[j].Name()
					}
					return files[i].Name() < files[j].Name()
				}
				if !files[i].IsDir() && !files[j].IsDir() {
					// 两个都是文件，按扩展名
					extI := filepath.Ext(files[i].Name())
					extJ := filepath.Ext(files[j].Name())
					if sortOrder == "desc" {
						return extI > extJ || (extI == extJ && files[i].Name() > files[j].Name())
					}
					return extI < extJ || (extI == extJ && files[i].Name() < files[j].Name())
				}
			}
		}

		// 默认按修改时间倒序排列（最新的在前）
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// 构建HTML页面（V2 新设计）
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>文件列表 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(sessionID) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">
	<script>
	// 全局Toast通知函数
	function showToast(message, type) {
		// 移除已有的Toast
		const existingToast = document.querySelector('.toast-notification');
		if (existingToast) {
			existingToast.remove();
		}
		
		// 创建Toast元素
		const toast = document.createElement('div');
		toast.className = 'toast-notification';
		toast.style.cssText = 'position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%) scale(0.8); padding: 16px 32px; border-radius: 12px; color: white; font-size: 16px; font-weight: 500; z-index: 99999; box-shadow: 0 8px 32px rgba(0,0,0,0.2); opacity: 0; transition: all 0.3s ease; text-align: center; min-width: 200px; max-width: 80%;';
		
		// 根据类型设置背景色
		if (type === 'success') {
			toast.style.background = 'linear-gradient(135deg, #12B76A, #039855)';
		} else if (type === 'error') {
			toast.style.background = 'linear-gradient(135deg, #F04438, #D92D20)';
		} else if (type === 'warning') {
			toast.style.background = 'linear-gradient(135deg, #F79009, #DC6803)';
		} else {
			toast.style.background = 'linear-gradient(135deg, #2E90FA, #1570EF)';
		}
		
		toast.textContent = message;
		document.body.appendChild(toast);
		
		// 显示动画
		setTimeout(() => {
			toast.style.opacity = '1';
			toast.style.transform = 'translate(-50%, -50%) scale(1)';
		}, 10);
		
		// 3秒后自动消失
		setTimeout(() => {
			toast.style.opacity = '0';
			toast.style.transform = 'translate(-50%, -50%) scale(0.8)';
			setTimeout(() => {
				if (toast.parentNode) {
					toast.parentNode.removeChild(toast);
				}
			}, 300);
		}, 3000);
	}
	
	// 重写全局alert函数，自动使用Toast通知
	window.alert = function(message) {
		// 根据消息内容判断类型
		let type = 'info';
		if (message.includes('成功') || message.includes('已')) {
			type = 'success';
		} else if (message.includes('失败') || message.includes('错误') || message.includes('不能') || message.includes('无法')) {
			type = 'error';
		} else if (message.includes('请') || message.includes('确认') || message.includes('警告')) {
			type = 'warning';
		}
		showToast(message, type);
	};
	</script>

</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">文件列表</h1>
					<p class="admin-page-desc">管理和浏览所有文件</p>
				</div>
			<!-- 显示消息 -->
			` + utils.GetMessage(r) + `

			<!-- 搜索框 -->
			<div class="search-bar-v2" style="display: flex; gap: 12px; align-items: center;">
				<form action="/" method="GET" style="flex: 1; display: flex;">
					<input type="hidden" name="path" value="` + url.QueryEscape(path) + `">
					<input type="text" name="search" placeholder="搜索文件或目录..." value="` + utils.EscapeHTML(searchQuery) + `" style="flex: 1;">
					<button type="submit">搜索</button>
				</form>
				<button onclick="openCategoryManager()" style="padding: 10px 20px; background: var(--v2-primary); color: white; border: none; border-radius: 10px; font-size: 14px; font-weight: 500; cursor: pointer; white-space: nowrap;">📁 分类管理</button>
			</div>

			<!-- 路径导航 -->
			<div class="path-nav-v2">
				<a href="/files?path=./">📁 根目录</a>
				` + utils.GeneratePathNavigation(path) + `
			</div>

				<!-- 文件列表 -->
			` + func() string {
		if isSearch {
			return generateSearchResults(r, searchResults, config.AppConfig.Server.DownloadDir)
		} else {
			return generateFileList(r, files, path)
		}
	}() + `
	</main>
		</div>

		<!-- 分类管理对话框 -->
		<div id="categoryModal" style="display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.5); z-index: 1000; justify-content: center; align-items: center;">
			<div style="background: white; border-radius: 12px; padding: 24px; width: 500px; max-height: 80vh; overflow-y: auto;">
				<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;">
					<h3 style="margin: 0; font-size: 18px; font-weight: 600;">分类管理</h3>
					<button onclick="closeCategoryManager()" style="background: none; border: none; font-size: 20px; cursor: pointer; color: #666;">&times;</button>
				</div>
				<div style="margin-bottom: 16px;">
					<input type="text" id="newCategoryName" placeholder="分类名称" style="width: 60%; padding: 8px 12px; border: 1px solid #e5e7eb; border-radius: 8px; font-size: 14px; margin-right: 8px;">
					<input type="text" id="newCategoryIcon" placeholder="图标" style="width: 20%; padding: 8px 12px; border: 1px solid #e5e7eb; border-radius: 8px; font-size: 14px; margin-right: 8px;">
					<button onclick="addCategory()" style="padding: 8px 16px; background: #4f46e5; color: white; border: none; border-radius: 8px; font-size: 14px; cursor: pointer;">添加</button>
				</div>
				<div id="categoryList" style="border-top: 1px solid #e5e7eb; padding-top: 16px;">
				</div>
			</div>
		</div>

		<script>
		function openCategoryManager() {
			document.getElementById('categoryModal').style.display = 'flex';
			loadCategories();
		}
		function closeCategoryManager() {
			document.getElementById('categoryModal').style.display = 'none';
		}
		function loadCategories() {
			fetch('/api/categories').then(res => res.json()).then(data => {
				const list = document.getElementById('categoryList');
				list.innerHTML = '';
				if (data.success && data.data) {
					data.data.forEach(cat => {
						const item = document.createElement('div');
						item.id = 'cat-item-' + cat.id;
						item.style.cssText = 'display: flex; align-items: center; padding: 10px 0; border-bottom: 1px solid #f3f4f6;';
						item.innerHTML = '<span id="cat-icon-' + cat.id + '" style="font-size: 18px; margin-right: 10px; width: 24px; text-align: center;">' + cat.icon + '</span><span id="cat-name-' + cat.id + '" style="flex: 1; font-size: 14px;">' + cat.name + '</span><div id="cat-actions-' + cat.id + '"><button onclick="startEditCategory(\'' + cat.id + '\', \'' + cat.name + '\', \'' + cat.icon + '\')" style="margin-right: 8px; padding: 4px 10px; background: #f3f4f6; border: none; border-radius: 6px; font-size: 12px; cursor: pointer;">编辑</button>' + (cat.id !== 'default' ? '<button onclick="deleteCategory(\'' + cat.id + '\')" style="padding: 4px 10px; background: #fee2e2; color: #dc2626; border: none; border-radius: 6px; font-size: 12px; cursor: pointer;">删除</button>' : '') + '</div>';
						list.appendChild(item);
					});
				}
			});
		}

		function startEditCategory(id, name, icon) {
			const nameEl = document.getElementById('cat-name-' + id);
			const iconEl = document.getElementById('cat-icon-' + id);
			const actionsEl = document.getElementById('cat-actions-' + id);
			
			nameEl.innerHTML = '<input type="text" id="edit-name-' + id + '" value="' + name + '" style="width: 120px; padding: 4px 8px; border: 1px solid #d1d5db; border-radius: 6px; font-size: 14px;">';
			iconEl.innerHTML = '<input type="text" id="edit-icon-' + id + '" value="' + icon + '" style="width: 40px; padding: 4px; border: 1px solid #d1d5db; border-radius: 6px; font-size: 14px; text-align: center;">';
			actionsEl.innerHTML = '<button onclick="saveEditCategory(\'' + id + '\')" style="margin-right: 8px; padding: 4px 10px; background: #4f46e5; color: white; border: none; border-radius: 6px; font-size: 12px; cursor: pointer;">保存</button><button onclick="loadCategories()" style="padding: 4px 10px; background: #f3f4f6; border: none; border-radius: 6px; font-size: 12px; cursor: pointer;">取消</button>';
		}

		function saveEditCategory(id) {
			const newName = document.getElementById('edit-name-' + id).value.trim();
			const newIcon = document.getElementById('edit-icon-' + id).value.trim() || '📁';
			if (!newName) { alert('分类名称不能为空'); return; }
			
			fetch('/api/categories/' + id, {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name: newName, icon: newIcon })
			}).then(res => res.json()).then(data => {
				if (data.success) {
					loadCategories();
				} else {
					alert(data.message || '修改失败');
				}
			});
		}
		function addCategory() {
			const name = document.getElementById('newCategoryName').value.trim();
			const icon = document.getElementById('newCategoryIcon').value.trim() || '📁';
			if (!name) { alert('请输入分类名称'); return; }
			fetch('/api/categories', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, icon }) }).then(res => res.json()).then(data => {
				if (data.success) { document.getElementById('newCategoryName').value = ''; document.getElementById('newCategoryIcon').value = ''; loadCategories(); }
				else { alert(data.message || '添加失败'); }
			});
		}

		function deleteCategory(id) {
			if (!confirm('确定要删除这个分类吗？该分类下的文件将移到全部分类。')) return;
			fetch('/api/categories/' + id, { method: 'DELETE' }).then(res => res.json()).then(data => {
				if (data.success) { loadCategories(); } else { alert(data.message || '删除失败'); }
			});
		}
		</script>
	</body>
</html>`

	w.Write([]byte(html))
}

// 根据文件扩展名获取图标
func getFileIcon(fileName string) string {
	// 定义文件类型图标映射
	// 使用简单、兼容性好的emoji
	iconMap := map[string]string{
		// 视频文件
		".mp4":  "🎬",
		".avi":  "🎬",
		".mkv":  "🎬",
		".mov":  "🎬",
		".wmv":  "🎬",
		".flv":  "🎬",
		".webm": "🎬",
		".mpeg": "🎬",
		".mpg":  "🎬",
		".rm":   "🎬",
		".rmvb": "🎬",
		// 音频文件
		".mp3":  "🎵",
		".wav":  "🎵",
		".flac": "🎵",
		".aac":  "🎵",
		".ogg":  "🎵",
		".wma":  "🎵",
		".m4a":  "🎵",
		// 图片文件
		".jpg":  "🖼️",
		".jpeg": "🖼️",
		".png":  "🖼️",
		".gif":  "🖼️",
		".bmp":  "🖼️",
		".svg":  "🖼️",
		".webp": "🖼️",
		".ico":  "🖼️",
		// 文档文件
		".pdf":  "📄",
		".doc":  "📄",
		".docx": "📄",
		".txt":  "📄",
		".rtf":  "📄",
		".md":   "📄",
		".html": "📄",
		".htm":  "📄",
		".csv":  "📄",
		".xls":  "📄",
		".xlsx": "📄",
		".ppt":  "📄",
		".pptx": "📄",
		// 压缩文件
		".zip": "📦",
		".rar": "📦",
		".7z":  "📦",
		".tar": "📦",
		".gz":  "📦",
		".bz2": "📦",
		".xz":  "📦",
		".tgz": "📦",
		// 种子文件
		".torrent": "🧲",
		// 可执行文件
		".exe": "⚙️",
		".app": "⚙️",
		".sh":  "⚙️",
		".bat": "⚙️",
		".cmd": "⚙️",
		".jar": "⚙️",
		".apk": "🤖",
		// 编程语言
		".py":    "❓",
		".go":    "❓",
		".js":    "❓",
		".ts":    "❓",
		".java":  "❓",
		".c":     "❓",
		".cpp":   "❓",
		".h":     "❓",
		".hpp":   "❓",
		".php":   "❓",
		".rb":    "❓",
		".swift": "❓",
		".kt":    "❓",
		// 磁盘镜像
		".iso": "💿",
		".img": "💿",
		".dmg": "💿",
		// 配置文件
		".json": "⚙️",
		".yaml": "⚙️",
		".yml":  "⚙️",
		".xml":  "⚙️",
		".cfg":  "⚙️",
		".conf": "⚙️",
	}

	// 获取文件扩展名并转换为小写
	ext := strings.ToLower(filepath.Ext(fileName))

	// 查找对应的图标，如果没有找到则返回默认图标
	if icon, ok := iconMap[ext]; ok {
		return icon
	}

	// 默认文件图标
	return "📄"
}

// 辅助函数：生成文件列表
func generateFileList(r *http.Request, files []os.DirEntry, currentPath string) string {
	var fileList string

	// 获取当前用户
	sess := session.GetCurrentUser(r)

	// 为管理员和二级管理员添加批量操作按钮
	var batchActions string
	if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
		// 获取所有目录列表，用于目标路径选择
		dirList := utils.GetDirectoryList(config.AppConfig.Server.DownloadDir)

		// 构建目录选择下拉框
		selectHTML := `<select id="target-path" style="padding: 8px; margin-right: 10px; border-radius: 3px; border: 1px solid #ddd;">`
		for _, dir := range dirList {
			displayName := dir
			if dir == "." {
				displayName = "根目录"
			}
			selectHTML += `<option value="` + dir + `">` + displayName + `</option>`
		}
		selectHTML += `</select>`

		batchActions = `<div class="batch-actions" style="margin-bottom: 20px;">
		<div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
			<button type="button" id="select-all" class="btn btn-info">全选</button>
			<button type="button" id="select-none" class="btn btn-info">取消全选</button>
			<button type="button" id="batch-delete" class="btn btn-danger">删除</button>
			<button type="button" id="batch-move" class="btn btn-primary">移动</button>
			<button type="button" id="batch-copy" class="btn btn-secondary">复制</button>
			<button type="button" id="create-dir" class="btn btn-primary">创建目录</button>
			<button type="button" id="custom-sort" class="btn btn-success">自定义排序</button>
			<button type="button" id="set-category" class="btn btn-warning" style="background: #F79009; color: white;">设置分类</button>
			<div style="display: none; margin-left: 10px;" id="move-copy-form">
				` + selectHTML + `
				<button type="button" id="confirm-action" class="btn btn-success">确认</button>
				<button type="button" id="cancel-action" class="btn btn-danger">取消</button>
			</div>
			<div style="display: none; margin-left: 10px;" id="create-dir-form">
				<input type="text" id="dir-name" placeholder="请输入目录名称" style="padding: 8px; margin-right: 10px; border-radius: 3px; border: 1px solid #ddd;">
				<button type="button" id="confirm-create-dir" class="btn btn-success">确认</button>
				<button type="button" id="cancel-create-dir" class="btn btn-danger">取消</button>
			</div>
			<div style="display: none; margin-left: 10px;" id="set-category-form">
				<select id="category-select" style="padding: 8px; margin-right: 10px; border-radius: 3px; border: 1px solid #ddd;">
					<option value="">请选择分类</option>
				</select>
				<button type="button" id="confirm-set-category" class="btn btn-success">确认</button>
				<button type="button" id="cancel-set-category" class="btn btn-danger">取消</button>
			</div>
		</div>
	</div>`
	}

	// 添加批量操作脚本
	var batchScript string
	// 分享功能脚本对所有用户都可用
	shareScript := `<script>
		// 分享文件功能
		function shareFile(filePath) {
			if (filePath) {
				// 直接使用已经编码过的filePath，避免双重编码问题
				const decodedFilePath = decodeURIComponent(filePath);
				const baseUrl = window.location.origin;
				const shareUrl = baseUrl + '/download?path=' + filePath;
				
				// 创建显示通知的辅助函数
				function showNotification(message, isSuccess) {
					// 移除任何现有的通知
					const existingNotification = document.querySelector('.share-notification');
					if (existingNotification) {
						existingNotification.remove();
					}
					
					// 创建新的通知元素
					const notification = document.createElement('div');
					notification.className = 'share-notification';
					notification.textContent = message;
					notification.style.cssText = "position:fixed; top:50%; left:50%; transform:translate(-50%, -50%); background:" + (isSuccess ? "#4CAF50" : "#f44336") + "; color:white; padding:20px; border-radius:5px; z-index:9999; box-shadow:0 2px 20px rgba(0,0,0,0.3); font-size:16px; text-align:center;";
					document.body.appendChild(notification);
					
					// 3秒后移除提示
					setTimeout(() => {
						if (notification.parentNode) {
							notification.remove();
						}
					}, 3000);
				}
				
				// 生成短链
				fetch('/api/generate-short-url?path=' + filePath + '&filename=' + encodeURIComponent(decodedFilePath.split('/').pop()), {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json'
					}
				}).then(response => response.json()).then(data => {
					if (data.success) {
						// 构建带有文件名的短链
						const fullShortURL = baseUrl + data.short_url;
						const filename = data.filename;
						const finalShareUrl = filename + ': ' + fullShortURL;
						
						// 复制到剪贴板
						if (navigator.clipboard) {
							navigator.clipboard.writeText(finalShareUrl).then(function() {
								// 显示成功通知
								showNotification('分享短链已复制到剪贴板！', true);
								
								// 直接调用后端API更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (!response.ok) {
										console.error('更新分享计数失败：', response.status);
										// 显示错误提示给用户
										showNotification('分享计数更新失败，请稍后重试', false);
									} else {
										console.log('分享计数更新成功');
									}
								}).catch(function(err) {
									console.error('更新分享计数失败：', err);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								});
							}).catch(function(err) {
								console.error('复制失败：', err);
								alert('分享链接复制失败，请手动复制：' + finalShareUrl);
								// 即使复制失败，仍然更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (response.ok) {
										console.log('分享计数更新成功（复制失败后）');
									} else {
										console.error('更新分享计数失败（复制失败后）：', response.status);
									}
								}).catch(function(err) {
									console.error('更新分享计数失败（复制失败后）：', err);
								});
							});
						} else {
							// 不支持剪贴板API的浏览器备用方案
							alert('您的浏览器不支持自动复制，请手动复制链接：' + finalShareUrl);
							// 仍然尝试更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（不支持剪贴板）');
								} else {
									console.error('更新分享计数失败（不支持剪贴板）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（不支持剪贴板）：', err);
							});
						}
					} else {
						// 生成短链失败：提示具体原因（未登录 / 路径非法 / 限流 / 配额），再回退为原始链接
						showNotification(data.error || '生成短链失败，已改为分享原始链接', false);
						if (navigator.clipboard) {
							navigator.clipboard.writeText(shareUrl).then(function() {
								// 显示成功通知
								showNotification('分享链接已复制到剪贴板！', true);
								
								// 直接调用后端API更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (!response.ok) {
										console.error('更新分享计数失败：', response.status);
										// 显示错误提示给用户
										showNotification('分享计数更新失败，请稍后重试', false);
									} else {
										console.log('分享计数更新成功');
									}
								}).catch(function(err) {
									console.error('更新分享计数失败：', err);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								});
							}).catch(function(err) {
								console.error('复制失败：', err);
								alert('分享链接复制失败，请手动复制：' + shareUrl);
								// 即使复制失败，仍然更新分享计数
								fetch('/api/increment-share?path=' + filePath, {
									method: 'POST',
									headers: {
										'Content-Type': 'application/json'
									}
								}).then(response => {
									if (response.ok) {
										console.log('分享计数更新成功（复制失败后）');
									} else {
										console.error('更新分享计数失败（复制失败后）：', response.status);
									}
								}).catch(function(err) {
									console.error('更新分享计数失败（复制失败后）：', err);
								});
							});
						} else {
							// 不支持剪贴板API的浏览器备用方案
							alert('您的浏览器不支持自动复制，请手动复制链接：' + shareUrl);
							// 仍然尝试更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（不支持剪贴板）');
								} else {
									console.error('更新分享计数失败（不支持剪贴板）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（不支持剪贴板）：', err);
							});
						}
					}
				}).catch(function(err) {
					console.error('生成短链失败：', err);
					// 生成短链失败，使用原始链接
					if (navigator.clipboard) {
						navigator.clipboard.writeText(shareUrl).then(function() {
							// 显示成功通知
							showNotification('分享链接已复制到剪贴板！', true);
							
							// 直接调用后端API更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (!response.ok) {
									console.error('更新分享计数失败：', response.status);
									// 显示错误提示给用户
									showNotification('分享计数更新失败，请稍后重试', false);
								} else {
									console.log('分享计数更新成功');
								}
							}).catch(function(err) {
								console.error('更新分享计数失败：', err);
								// 显示错误提示给用户
								showNotification('分享计数更新失败，请稍后重试', false);
							});
						}).catch(function(err) {
							console.error('复制失败：', err);
							alert('分享链接复制失败，请手动复制：' + shareUrl);
							// 即使复制失败，仍然更新分享计数
							fetch('/api/increment-share?path=' + filePath, {
								method: 'POST',
								headers: {
									'Content-Type': 'application/json'
								}
							}).then(response => {
								if (response.ok) {
									console.log('分享计数更新成功（复制失败后）');
								} else {
									console.error('更新分享计数失败（复制失败后）：', response.status);
								}
							}).catch(function(err) {
								console.error('更新分享计数失败（复制失败后）：', err);
							});
						});
					} else {
						// 不支持剪贴板API的浏览器备用方案
						alert('您的浏览器不支持自动复制，请手动复制链接：' + shareUrl);
						// 仍然尝试更新分享计数
						fetch('/api/increment-share?path=' + filePath, {
							method: 'POST',
							headers: {
								'Content-Type': 'application/json'
							}
						}).then(response => {
							if (response.ok) {
								console.log('分享计数更新成功（不支持剪贴板）');
							} else {
								console.error('更新分享计数失败（不支持剪贴板）：', response.status);
							}
						}).catch(function(err) {
							console.error('更新分享计数失败（不支持剪贴板）：', err);
						});
					}
				});
			}
		}
	</script>`

	if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
		batchScript = `<script>
			// 批量操作脚本
			document.addEventListener('DOMContentLoaded', function() {
				const batchDeleteBtn = document.getElementById('batch-delete');
				const batchMoveBtn = document.getElementById('batch-move');
				const batchCopyBtn = document.getElementById('batch-copy');
				const createDirBtn = document.getElementById('create-dir');
				const selectAllBtn = document.getElementById('select-all');
				const selectNoneBtn = document.getElementById('select-none');
				const moveCopyForm = document.getElementById('move-copy-form');
				const createDirForm = document.getElementById('create-dir-form');
				const confirmBtn = document.getElementById('confirm-action');
				const cancelBtn = document.getElementById('cancel-action');
				const confirmCreateDirBtn = document.getElementById('confirm-create-dir');
				const cancelCreateDirBtn = document.getElementById('cancel-create-dir');
				const setCategoryBtn = document.getElementById('set-category');
				const setCategoryForm = document.getElementById('set-category-form');
				const confirmSetCategoryBtn = document.getElementById('confirm-set-category');
				const cancelSetCategoryBtn = document.getElementById('cancel-set-category');
				const categorySelect = document.getElementById('category-select');
				let currentAction = '';

			// 全选功能
			selectAllBtn.addEventListener('click', function() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]');
				checkboxes.forEach(cb => {
					cb.checked = true;
				});
			});

			// 取消全选功能
			selectNoneBtn.addEventListener('click', function() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]');
				checkboxes.forEach(cb => {
					cb.checked = false;
				});
			});

			// 显示移动/复制表单
				function showMoveCopyForm(action) {
					currentAction = action;
					// 检查是否是移动设备
					if (window.innerWidth <= 768) {
						moveCopyForm.classList.add('active');
						// 隐藏创建目录表单
						createDirForm.classList.remove('active');
					} else {
						moveCopyForm.style.display = 'flex';
						// 隐藏创建目录表单
						createDirForm.style.display = 'none';
					}
				}

				// 隐藏移动/复制表单
				function hideMoveCopyForm() {
					// 检查是否是移动设备
					if (window.innerWidth <= 768) {
						moveCopyForm.classList.remove('active');
					} else {
						moveCopyForm.style.display = 'none';
					}
					currentAction = '';
					// 清空输入框
					document.getElementById('target-path').value = '';
				}

				// 显示创建目录表单
				function showCreateDirForm() {
					// 检查是否是移动设备
					if (window.innerWidth <= 768) {
						createDirForm.classList.add('active');
						// 隐藏移动/复制表单
						moveCopyForm.classList.remove('active');
					} else {
						createDirForm.style.display = 'flex';
						// 隐藏移动/复制表单
						moveCopyForm.style.display = 'none';
					}
					// 清空输入框
					document.getElementById('dir-name').value = '';
				}

				// 隐藏创建目录表单
				function hideCreateDirForm() {
					// 检查是否是移动设备
					if (window.innerWidth <= 768) {
						createDirForm.classList.remove('active');
					} else {
						createDirForm.style.display = 'none';
					}
					// 清空输入框
					document.getElementById('dir-name').value = '';
				}

				// 加载分类列表
				function loadCategories() {
					fetch('/api/categories')
						.then(response => response.json())
						.then(data => {
							if (data.success && data.data) {
								categorySelect.innerHTML = '<option value="">请选择分类</option>';
								data.data.forEach(cat => {
									if (cat.id !== 'default') {
										const option = document.createElement('option');
										option.value = cat.id;
										option.textContent = cat.icon + ' ' + cat.name;
										categorySelect.appendChild(option);
									}
								});
							}
						})
						.catch(err => {
							console.error('加载分类列表失败:', err);
						});
				}

				// 显示设置分类表单
				function showSetCategoryForm() {
					// 隐藏其他表单
					hideMoveCopyForm();
					hideCreateDirForm();
					// 加载分类列表
					loadCategories();
					// 显示设置分类表单
					if (window.innerWidth <= 768) {
						setCategoryForm.classList.add('active');
					} else {
						setCategoryForm.style.display = 'flex';
					}
				}

				// 隐藏设置分类表单
				function hideSetCategoryForm() {
					if (window.innerWidth <= 768) {
						setCategoryForm.classList.remove('active');
					} else {
						setCategoryForm.style.display = 'none';
					}
					// 清空选择
					categorySelect.value = '';
				}

			// 获取选中的文件
			function getSelectedFiles() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]:checked');
				const files = [];
				checkboxes.forEach(cb => {
					files.push(cb.value);
				});
				return files;
			}

			// 批量删除
			batchDeleteBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要删除的文件');
					return;
				}
				if (confirm('确定要删除选中的 ' + files.length + ' 个文件/目录吗？')) {
					const form = document.createElement('form');
					form.method = 'POST';
					form.action = '/batch-delete';
					files.forEach(file => {
						const input = document.createElement('input');
						input.type = 'hidden';
						input.name = 'files';
						input.value = file;
						form.appendChild(input);
					});
					document.body.appendChild(form);
					form.submit();
				}
			});

			// 批量移动
			batchMoveBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要移动的文件');
					return;
				}
				showMoveCopyForm('move');
			});

			// 批量复制
			batchCopyBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要复制的文件');
					return;
				}
				showMoveCopyForm('copy');
			});

			// 创建目录
			createDirBtn.addEventListener('click', function() {
				showCreateDirForm();
			});

			// 确认创建目录
			confirmCreateDirBtn.addEventListener('click', function() {
				const dirName = document.getElementById('dir-name').value;
				if (dirName === '') {
					alert('请输入目录名称');
					return;
				}

				// 获取当前路径
				const currentPath = new URL(window.location.href).searchParams.get('path') || '.';

				// 创建AJAX请求
				const xhr = new XMLHttpRequest();
				xhr.open('POST', '/mkdir', true);
				xhr.setRequestHeader('Content-Type', 'application/x-www-form-urlencoded');

				xhr.onload = function() {
					if (xhr.status === 200) {
						// 目录创建成功，刷新页面
						window.location.reload();
					} else {
						// 目录创建失败，显示错误信息
						alert('创建目录失败: ' + xhr.responseText);
					}
				};

				xhr.onerror = function() {
					alert('创建目录失败: 网络错误');
				};

				// 发送请求
				xhr.send('parent_dir=' + encodeURIComponent(currentPath) + '&dir_name=' + encodeURIComponent(dirName));
			});

			// 取消创建目录
			cancelCreateDirBtn.addEventListener('click', function() {
				hideCreateDirForm();
			});

			// 设置分类
			setCategoryBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					showToast('请选择要设置分类的文件', 'warning');
					return;
				}
				showSetCategoryForm();
			});

			// Toast通知函数
			function showToast(message, type) {
				// 移除已有的Toast
				const existingToast = document.querySelector('.toast-notification');
				if (existingToast) {
					existingToast.remove();
				}
				
				// 创建Toast元素
				const toast = document.createElement('div');
				toast.className = 'toast-notification';
				toast.style.cssText = 'position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%) scale(0.8); padding: 16px 32px; border-radius: 12px; color: white; font-size: 16px; font-weight: 500; z-index: 9999; box-shadow: 0 8px 32px rgba(0,0,0,0.2); opacity: 0; transition: all 0.3s ease; text-align: center; min-width: 200px;';
				
				// 根据类型设置背景色
				if (type === 'success') {
					toast.style.background = 'linear-gradient(135deg, #12B76A, #039855)';
				} else if (type === 'error') {
					toast.style.background = 'linear-gradient(135deg, #F04438, #D92D20)';
				} else if (type === 'warning') {
					toast.style.background = 'linear-gradient(135deg, #F79009, #DC6803)';
				} else {
					toast.style.background = 'linear-gradient(135deg, #2E90FA, #1570EF)';
				}
				
				toast.textContent = message;
				document.body.appendChild(toast);
				
				// 显示动画
				setTimeout(() => {
					toast.style.opacity = '1';
					toast.style.transform = 'translate(-50%, -50%) scale(1)';
				}, 10);
				
				// 3秒后自动消失
				setTimeout(() => {
					toast.style.opacity = '0';
					toast.style.transform = 'translate(-50%, -50%) scale(0.8)';
					setTimeout(() => {
						if (toast.parentNode) {
							toast.parentNode.removeChild(toast);
						}
					}, 300);
				}, 3000);
			}
			
			// 确认设置分类
			confirmSetCategoryBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				const categoryId = categorySelect.value;
				if (categoryId === '') {
					showToast('请选择分类', 'warning');
					return;
				}
				
				// 获取CSRF令牌
				const csrfMeta = document.querySelector('meta[name="csrf-token"]');
				const csrfToken = csrfMeta ? csrfMeta.content : '';

				// 逐个设置文件分类
				let successCount = 0;
				let failCount = 0;
				files.forEach((filePath, index) => {
					// 对URL编码的路径进行解码
					let decodedPath = filePath;
					try {
						decodedPath = decodeURIComponent(filePath);
					} catch (e) {
						// 解码失败，使用原始路径
					}
					
					fetch('/api/file-category', {
						method: 'POST',
						headers: {
							'Content-Type': 'application/json',
							'X-CSRF-Token': csrfToken
						},
						body: JSON.stringify({
							file_path: decodedPath,
							category_id: categoryId
						})
					})
					.then(response => response.json())
					.then(data => {
						if (data.success) {
							successCount++;
						} else {
							failCount++;
							console.error('设置分类失败:', data.message);
						}
						// 最后一个文件处理完成后，显示结果并刷新页面
						if (index === files.length - 1) {
							if (failCount === 0) {
								showToast('成功设置 ' + successCount + ' 个文件的分类', 'success');
							} else {
								showToast('成功设置 ' + successCount + ' 个文件的分类，失败 ' + failCount + ' 个', 'warning');
							}
							setTimeout(() => {
								window.location.reload();
							}, 1000);
						}
					})
					.catch(err => {
						failCount++;
						console.error('设置文件分类失败:', err);
						if (index === files.length - 1) {
							showToast('设置分类失败: ' + err.message, 'error');
						}
					});
				});
			});

			// 取消设置分类
			cancelSetCategoryBtn.addEventListener('click', function() {
				hideSetCategoryForm();
			});

			// 确认移动/复制
			confirmBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				const targetPath = document.getElementById('target-path').value;
				if (targetPath === '') {
					alert('请输入目标路径');
					return;
				}

				const form = document.createElement('form');
				form.method = 'POST';
				if (currentAction === 'move') {
					form.action = '/batch-move';
				} else {
					form.action = '/batch-copy';
				}

				// 添加选中的文件
				files.forEach(file => {
					const input = document.createElement('input');
					input.type = 'hidden';
					input.name = 'files';
					input.value = file;
					form.appendChild(input);
				});

				// 添加目标路径
				const targetInput = document.createElement('input');
				targetInput.type = 'hidden';
				targetInput.name = 'target_path';
				targetInput.value = targetPath;
				form.appendChild(targetInput);

				document.body.appendChild(form);
				form.submit();
			});

			// 取消移动/复制
			cancelBtn.addEventListener('click', function() {
				hideMoveCopyForm();
			});
		});
		</script>`
		// 将分享脚本和批量操作脚本合并
		batchScript += shareScript
	} else {
		// 对于非管理员用户，只提供分享功能脚本
		batchScript = shareScript
	}

	// 先添加返回上一级目录的选项（如果不是根目录）
	if currentPath != "." {
		parentPath := filepath.Dir(currentPath)
		if parentPath == "." {
			parentPath = ""
		}
		fileList += fmt.Sprintf(`<div class="file-item">
					<div class="file-item-content">
						<div class="file-icon">📁</div>
						<div class="file-info">
							<div class="file-name"><a href="/files?path=%s">..</a></div>
							<div class="file-meta">返回上一级</div>
						</div>
					</div>
				</div>`, url.QueryEscape(parentPath))
	}

	// 添加文件和目录
	for _, file := range files {
		name := file.Name()
		filePath := filepath.Join(currentPath, name)
		fileURL := utils.EncodePath(filePath)

		// 获取文件信息
		info, err := file.Info()
		if err != nil {
			continue
		}

		// 生成文件图标
		var icon string
		if file.IsDir() {
			icon = "📁"
		} else {
			// 对于可执行文件，使用/icon?path=方式获取真实图标
			// 注意：不能直接访问图标缓存文件，因为config/icons/cache目录没有被映射为静态资源
			ext := strings.ToLower(filepath.Ext(name))
			if utils.IsExecutableFile(ext) {
				// 使用和前端首页一致的图标URL方式
				iconURL := fmt.Sprintf("/icon?path=%s", fileURL)
				icon = fmt.Sprintf(`<img src="%s" alt="%s" style="width:40px;height:40px;object-fit:contain;">`, 
					utils.EscapeHTML(iconURL), utils.EscapeHTML(name))
			} else {
				icon = getFileIcon(name)
			}
		}

		// 生成文件元信息
		var meta string
		if file.IsDir() {
			meta = "目录 • " + info.ModTime().Format("2006-01-02 15:04:05")
		} else {
			meta = fmt.Sprintf("文件 • %s • %s", utils.FormatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))
		}

		// 为管理员和二级管理员添加复选框
		var checkbox string
		if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
			checkbox = fmt.Sprintf(`<input type="checkbox" name="selected-files" value="%s" style="margin-right: 15px; transform: scale(1.2);">`, fileURL)
		}

		// 获取文件分类信息（替换总流量显示）
		var categoryInfo string
		if !file.IsDir() {
			cm := utils.GetCategoryManager()
			// 标准化路径分隔符，确保与设置分类时使用的路径格式一致
			normalizedPath := strings.ReplaceAll(filePath, "\\", "/")
			categoryID := cm.GetFileCategory(normalizedPath)
			if categoryID != "" && categoryID != "default" {
				if category, err := cm.GetCategoryByID(categoryID); err == nil {
					categoryInfo = fmt.Sprintf(" • <span style=\"color: #7C3AED;\">%s %s</span>", category.Icon, category.Name)
				}
			}
		}

		// 获取文件下载统计信息（只保留下载次数，移除总流量）
		var downloadStats string
		if !file.IsDir() {
			// 获取下载数据 - 使用与DownloadHandler一致的路径格式
			// 确保路径格式与DownloadHandler中使用的格式完全相同
			relPath := filePath
			// 清理路径，移除多余的分隔符
			relPath = filepath.Clean(relPath)
			// 移除相对路径前缀，处理Windows和Linux的不同情况
			// 处理.前缀（Windows）和./前缀（Linux）
			relPath = strings.TrimPrefix(relPath, "./")
			relPath = strings.TrimPrefix(relPath, ".\\")
			// 标准化路径分隔符，使用与IncrementDownloadCount相同的方法
			relPath = utils.NormalizePath(relPath)
			count := GetFileDownloadCount(relPath)
			if count > 0 {
				downloadStats = fmt.Sprintf(" • <span style=\"color: #4285f4;\">下载 %d 次</span>", count)
			}
		}

		// 生成列表视图文件项
		var item string
		if file.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
						<div class="file-item-content">
							%s
							<div class="file-icon">%s</div>
							<div class="file-info">
								<div class="file-name"><a href="/files?path=%s">%s</a></div>
								<div class="file-meta">%s</div>
							</div>
						</div>
						<div class="file-actions">
						</div>
					</div>`, checkbox, icon, fileURL, utils.EscapeHTML(name), meta)
		} else {
			// 检查文件是否在待审核目录中
			pendingFilePath := filepath.Join(currentPath, name)
			pendingFullPath := filepath.Join(config.AppConfig.Server.PendingDir, pendingFilePath)
			_, pendingErr := os.Stat(pendingFullPath)
			isPending := pendingErr == nil

			// 如果是待审核文件，添加待审核状态
			if isPending {
				meta += " • <span style=\"color: orange;\">待审核</span>"
			}

			// 添加分类信息
			meta += categoryInfo
			
			// 添加下载统计信息
			meta += downloadStats

			item = fmt.Sprintf(`<div class="file-item">
						<div class="file-item-content">
							%s
							<div class="file-icon">%s</div>
							<div class="file-info">
								<div class="file-name">%s</div>
								<div class="file-meta">%s</div>
							</div>
						</div>
						<div class="file-actions">
							<a href="/download?path=%s" class="btn btn-secondary">下载</a>
							<button onclick="shareFile('%s')" class="btn btn-primary">分享</button>
						</div>
					</div>`, checkbox, icon, name, meta, fileURL, fileURL)
		}

		fileList += item
	}

	// 如果不是管理员，添加当前用户的待审核文件列表
	if sess != nil && sess.Role != constants.RoleAdmin {
		// 获取待审核目录的根路径
		pendingRoot := config.AppConfig.Server.PendingDir

		// 构建当前用户的待审核目录路径
		userPendingDir := filepath.Join(pendingRoot, sess.Username)

		// 确保用户待审核目录存在
		os.MkdirAll(userPendingDir, 0755)

		// 检查用户待审核目录是否存在
		if _, err := os.Stat(userPendingDir); err == nil {
			// 递归遍历用户待审核目录中的所有文件
			// 包括根目录和子目录中的待审核文件
			var allPendingFiles []string

			walkErr := filepath.Walk(userPendingDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					// 获取相对路径
					relPath, err := filepath.Rel(userPendingDir, filepath.Dir(path))
					if err != nil {
						return err
					}

					// 只有当文件所在的相对目录与当前浏览的目录匹配时，才添加到列表
					if relPath == currentPath {
						allPendingFiles = append(allPendingFiles, path)
					}
				}
				return nil
			})

			if walkErr != nil {

			} else {

				// 遍历匹配的待审核文件
				for _, filePath := range allPendingFiles {
					// 获取文件名
					filename := filepath.Base(filePath)

					// 获取文件信息
					fileInfo, err := os.Stat(filePath)
					if err != nil {

						continue
					}

					// 生成文件图标
					icon := "📄"

					// 生成文件元信息
					meta := fmt.Sprintf("文件 • %s • %s", utils.FormatFileSize(fileInfo.Size()), fileInfo.ModTime().Format("2006-01-02 15:04:05"))

					// 生成文件项（样式已统一收编到 static/styles.css）
					item := fmt.Sprintf(`<div class="file-item pending-file-item">
							<div class="file-item-content">
								<div class="file-icon">%s</div>
								<div class="file-info">
									<div class="file-name">%s</div>
									<div class="file-meta">%s</div>
								</div>
							</div>
							<div class="file-actions">
								<span class="status-badge pending">待审核</span>
							</div>
						</div>`, icon, filename, meta)

					fileList += item

				}
			}
		}
	}

	// 如果文件列表为空，添加空目录消息
	if fileList == "" {
		fileList = utils.GetEmptyMessage()
	}

	// 添加自定义排序的HTML和脚本（仅管理员可见）
	var customSortHTML string
	if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
		customSortHTML = `
		<!-- 自定义排序模态框 -->
		<div id="sort-modal" style="display: none; position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0, 0, 0, 0.5); z-index: 1000; justify-content: center; align-items: center;">
			<div style="background: #ffffff; border-radius: 16px; padding: 24px; width: 90%; max-width: 500px; max-height: 80vh; overflow-y: auto; box-shadow: 0 20px 60px rgba(0, 0, 0, 0.15);">
				<h3 style="margin: 0 0 8px 0; font-size: 18px; font-weight: 600; color: #101828;">自定义排序</h3>
				<p style="margin: 0 0 16px 0; font-size: 14px; color: #475467;">请为每个文件/目录输入排序编号，然后点击"确认排序"按钮。</p>
				<div id="sort-items" style="margin-bottom: 20px;">
				</div>
				<div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 20px;">
					<button type="button" id="cancel-sort" style="padding: 10px 20px; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; background: #F04438; color: #ffffff;">取消</button>
					<button type="button" id="confirm-sort" style="padding: 10px 20px; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; background: #12B76A; color: #ffffff;">确认排序</button>
				</div>
			</div>
		</div>
		
		<script>
		// 自定义排序功能
		document.addEventListener('DOMContentLoaded', function() {
			// 获取当前路径（从URL中解析）
			const currentUrl = new URL(window.location.href);
			const currentPath = currentUrl.searchParams.get('path') || '.';
			
			// 加载保存的自定义排序
			function loadCustomSort() {
				fetch('/api/get-custom-sort?path=' + encodeURIComponent(currentPath))
					.then(response => response.json())
					.then(data => {
						if (data.success && data.fileList && data.fileList.length > 0) {
							// 按保存的顺序排序文件
							const originalFileItems = document.querySelectorAll('.file-item');
							const backItem = Array.from(originalFileItems).find(item => {
								const meta = item.querySelector('.file-meta');
								return meta && meta.textContent.trim() === '返回上一级';
							});
							
							// 创建新的文件项数组
							const newFileItems = [];
							if (backItem) {
								newFileItems.push(backItem);
							}
							
							// 按保存的顺序添加文件项
							data.fileList.forEach(fileName => {
								const originalItem = Array.from(originalFileItems).find(item => {
									const nameElement = item.querySelector('.file-name');
									return nameElement && nameElement.textContent.trim() === fileName;
								});
								if (originalItem) {
									newFileItems.push(originalItem);
								}
							});
							
							// 添加未在排序列表中的剩余文件
							originalFileItems.forEach(item => {
								const nameElement = item.querySelector('.file-name');
								if (nameElement) {
									const fileName = nameElement.textContent.trim();
									const isBackItem = item.querySelector('.file-meta') && item.querySelector('.file-meta').textContent.trim() === '返回上一级';
									if (!isBackItem && !data.fileList.includes(fileName)) {
										newFileItems.push(item);
									}
								}
							});
							
							// 直接替换原始文件项，不重新构建整个容器
							const fileListContainer = document.querySelector('.file-list');
							if (fileListContainer) {
								// 获取所有原始文件项，包括返回上一级
								const allFileItems = fileListContainer.querySelectorAll('.file-item');
								
								// 移除所有原始文件项
								allFileItems.forEach(item => {
									item.remove();
								});
								
								// 添加新排序后的文件项
								newFileItems.forEach(item => {
									fileListContainer.appendChild(item);
								});
							}
						}
					});
			}
			
			// 初始加载保存的排序
			loadCustomSort();
			
			const customSortBtn = document.getElementById('custom-sort');
			const sortModal = document.getElementById('sort-modal');
			const cancelSortBtn = document.getElementById('cancel-sort');
			const confirmSortBtn = document.getElementById('confirm-sort');
			const sortItemsContainer = document.getElementById('sort-items');
			
			// 打开自定义排序模态框
			customSortBtn.addEventListener('click', function() {
				// 收集所有文件项
				const fileItems = document.querySelectorAll('.file-item');
				let sortItemsHTML = '';
				
				fileItems.forEach((item, index) => {
					// 跳过返回上一级目录的项
					if (item.querySelector('.file-meta') && item.querySelector('.file-meta').textContent.trim() === '返回上一级') {
						return;
					}
					
					const fileName = item.querySelector('.file-name').textContent.trim();
					const isDir = item.querySelector('.file-icon').textContent === '📁';
					const fileType = isDir ? '目录' : '文件';
					
					sortItemsHTML += '<div style="display: flex; align-items: center; gap: 10px; margin-bottom: 10px; padding: 10px; background: #f9f9f9; border-radius: 4px;">' +
							'<span style="font-size: 20px;">' + (isDir ? '📁' : '📄') + '</span>' +
							'<input type="number" min="1" value="' + (index + 1) + '" style="width: 60px; padding: 6px; border: 1px solid #ddd; border-radius: 4px; text-align: center;">' +
							'<div style="flex: 1;">' +
								'<div style="font-weight: bold;">' + fileName + '</div>' +
								'<div>' + fileType + '</div>' +
							'</div>' +
						'</div>';
				});
				
				sortItemsContainer.innerHTML = sortItemsHTML;
				sortModal.style.display = 'flex';
			});
			
			// 关闭模态框
			cancelSortBtn.addEventListener('click', function() {
				sortModal.style.display = 'none';
			});
			
			// 点击模态框外部关闭
			sortModal.addEventListener('click', function(e) {
				if (e.target === sortModal) {
					sortModal.style.display = 'none';
				}
			});
			
			// 确认排序
			confirmSortBtn.addEventListener('click', function() {
				// 获取当前路径（从URL中解析）
				const currentUrl = new URL(window.location.href);
				const currentPath = currentUrl.searchParams.get('path') || '.';
				
				// 获取所有排序项和输入的编号
				const sortItems = sortItemsContainer.querySelectorAll('div[style*="display: flex"]');
				const sortData = [];
				
				sortItems.forEach(item => {
					const input = item.querySelector('input');
					// 直接从文件名元素获取文本，而不是嵌套的div
					const fileNameElement = item.querySelector('div[style*="font-weight: bold"]');
					const fileName = fileNameElement ? fileNameElement.textContent.trim() : '';
					const sortNum = parseInt(input.value);
					
					if (fileName) {
						sortData.push({ name: fileName, sortNum: sortNum });
					}
			});
			
			// 按编号排序
			sortData.sort((a, b) => a.sortNum - b.sortNum);
			
			// 构建新的文件列表
			const originalFileItems = document.querySelectorAll('.file-item');
			const backItem = Array.from(originalFileItems).find(item => {
				const meta = item.querySelector('.file-meta');
				return meta && meta.textContent.trim() === '返回上一级';
			});
			
			// 创建新的文件项数组
			const newFileItems = [];
			const sortedFileNames = [];
			if (backItem) {
				newFileItems.push(backItem);
			}
			
			// 按排序数据顺序添加文件项
			sortData.forEach(sortItem => {
				const originalItem = Array.from(originalFileItems).find(item => {
					const nameElement = item.querySelector('.file-name');
					return nameElement && nameElement.textContent.trim() === sortItem.name;
				});
				if (originalItem) {
					newFileItems.push(originalItem);
					sortedFileNames.push(sortItem.name);
				}
			});
			
			// 添加未在排序列表中的剩余文件
			originalFileItems.forEach(item => {
				const nameElement = item.querySelector('.file-name');
				if (nameElement) {
					const fileName = nameElement.textContent.trim();
					const isBackItem = item.querySelector('.file-meta') && item.querySelector('.file-meta').textContent.trim() === '返回上一级';
					if (!isBackItem && !sortedFileNames.includes(fileName)) {
						newFileItems.push(item);
						sortedFileNames.push(fileName);
					}
				}
			});
			
			// 直接替换原始文件项，不重新构建整个容器
			const fileListContainer = document.querySelector('.file-list');
			if (fileListContainer) {
				// 获取所有原始文件项，包括返回上一级
				const allFileItems = fileListContainer.querySelectorAll('.file-item');
				
				// 移除所有原始文件项
				allFileItems.forEach(item => {
					item.remove();
				});
				
				// 添加新排序后的文件项
				newFileItems.forEach(item => {
					fileListContainer.appendChild(item);
				});
			}
			
			// 保存排序结果到后端
			fetch('/api/save-custom-sort', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({
						path: currentPath,
						fileList: sortedFileNames
					})
			})
			.then(response => response.json())
			.then(data => {
				if (!data.success) {
					console.error('保存排序失败:', data);
				}
			})
			.catch(error => {
				console.error('保存排序失败:', error);
			});
			
			// 关闭模态框
			sortModal.style.display = 'none';
		});
		});
		</script>
		`
	}

	// 添加对所有用户可见的排序加载脚本
	const sortLoadScript = `<script>
		// 加载保存的自定义排序（所有用户可见）
		document.addEventListener('DOMContentLoaded', function() {
			// 获取当前路径（从URL中解析）
			const currentUrl = new URL(window.location.href);
			const currentPath = currentUrl.searchParams.get('path') || '.';
			
			// 加载保存的自定义排序
			function loadCustomSort() {
				fetch('/api/get-custom-sort?path=' + encodeURIComponent(currentPath))
					.then(response => response.json())
					.then(data => {
						if (data.success && data.fileList && data.fileList.length > 0) {
							// 按保存的顺序排序文件
							const originalFileItems = document.querySelectorAll('.file-item');
							const backItem = Array.from(originalFileItems).find(item => {
								const meta = item.querySelector('.file-meta');
								return meta && meta.textContent.trim() === '返回上一级';
							});
							
							// 创建新的文件项数组
							const newFileItems = [];
							if (backItem) {
								newFileItems.push(backItem);
							}
							
							// 按保存的顺序添加文件项
							data.fileList.forEach(fileName => {
								const originalItem = Array.from(originalFileItems).find(item => {
									const nameElement = item.querySelector('.file-name');
									return nameElement && nameElement.textContent.trim() === fileName;
								});
								if (originalItem) {
									newFileItems.push(originalItem);
								}
							});
							
							// 添加未在排序列表中的剩余文件
							originalFileItems.forEach(item => {
								const nameElement = item.querySelector('.file-name');
								if (nameElement) {
									const fileName = nameElement.textContent.trim();
									const isBackItem = item.querySelector('.file-meta') && item.querySelector('.file-meta').textContent.trim() === '返回上一级';
									if (!isBackItem && !data.fileList.includes(fileName)) {
										newFileItems.push(item);
									}
								}
							});
							
							// 直接替换原始文件项，不重新构建整个容器
							const fileListContainer = document.querySelector('.file-list');
							if (fileListContainer) {
								// 获取所有原始文件项，包括返回上一级
								const allFileItems = fileListContainer.querySelectorAll('.file-item');
								
								// 移除所有原始文件项
								allFileItems.forEach(item => {
									item.remove();
								});
								
								// 添加新排序后的文件项
								newFileItems.forEach(item => {
									fileListContainer.appendChild(item);
								});
							}
						}
					});
			}
			
			// 初始加载保存的排序
			loadCustomSort();
		});
	</script>`

	// 返回完整的HTML，添加file-list容器用于自定义排序功能
	return batchActions + `<div class="file-list">` + fileList + `</div>` + batchScript + customSortHTML + sortLoadScript
}

// 批量删除处理函数
func BatchDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfToken := r.FormValue("csrf_token")
	if !utils.ValidateCSRFToken(sessionID, csrfToken) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以执行批量删除操作
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要删除的文件&type=error", http.StatusFound)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "batch_delete", "批量删除文件: "+fmt.Sprint(files))

	// 批量删除文件
	var deletedCount int
	var failedCount int
	// 记录需要失效的目录缓存
	var directoriesToInvalidate map[string]bool = make(map[string]bool)

	for _, filePath := range files {
		// URL解码路径
		decodedPath, err := utils.DecodePath(filePath)
		if err != nil {
			log.Printf("文件路径解码失败: %s, 错误: %v", filePath, err)
			failedCount++
			continue
		}

		// 构建完整路径
		fullPath := utils.SafeJoin(config.AppConfig.Server.DownloadDir, decodedPath)
		// 获取父目录路径
		parentDir := filepath.Dir(fullPath)
		// 记录需要失效的目录
		directoriesToInvalidate[parentDir] = true

		// 删除文件或目录
		err = os.RemoveAll(fullPath)
		if err != nil {
			failedCount++
			continue
		}

		deletedCount++
	}

	// 使相关目录缓存失效
	for dir := range directoriesToInvalidate {
		invalidateCache(dir)
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功删除 %d 个文件，失败 %d 个", deletedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 批量移动处理函数
func BatchMoveHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfToken := r.FormValue("csrf_token")
	if !utils.ValidateCSRFToken(sessionID, csrfToken) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以移动文件
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]
	targetPath := r.FormValue("target_path")

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要移动的文件&type=error", http.StatusFound)
		return
	}

	if targetPath == "" {
		http.Redirect(w, r, "/files?msg=请输入目标路径&type=error", http.StatusFound)
		return
	}

	// 使用DecodePath解码目标路径，确保下划线被还原为空格
	decodedTargetPath, err := utils.DecodePath(targetPath)
	// 如果解码失败，使用SanitizeFilename作为备选方案
	if err != nil || decodedTargetPath == "" {
		decodedTargetPath = utils.SanitizeFilename(targetPath)
	}

	// 清理目标路径
	decodedTargetPath = filepath.Clean(decodedTargetPath)
	if strings.HasPrefix(decodedTargetPath, "..") {
		http.Redirect(w, r, "/files?msg=无效的目标路径&type=error", http.StatusFound)
		return
	}

	// 构建完整的目标路径
	targetFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, decodedTargetPath)

	// 确保目标目录存在
	os.MkdirAll(targetFullPath, 0755)

	// 记录日志 - 包含源路径和解码后的目标路径
	utils.LogUserAction(r, "batch_move", fmt.Sprintf("批量移动文件: %v 到 %s (原始路径: %s)", files, decodedTargetPath, targetPath))

	// 批量移动文件
	var movedCount int
	var failedCount int
	// 记录需要失效的目录缓存
	var directoriesToInvalidate map[string]bool = make(map[string]bool)

	for _, filePath := range files {
		// 首先尝试标准URL解码
		decodedPath, err := url.QueryUnescape(filePath)
		// 如果失败，使用增强的DecodePath函数
		if err != nil {
			decodedPath, err = utils.DecodePath(filePath)
			if err != nil {
				log.Printf("文件路径解码失败: %s, 错误: %v", filePath, err)
				failedCount++
				continue
			}
		}

		// 构建源文件完整路径（使用SafeJoin确保安全性）
		sourceFullPath := utils.SafeJoin(config.AppConfig.Server.DownloadDir, decodedPath)
		// 获取源目录路径
		sourceDir := filepath.Dir(sourceFullPath)
		// 记录需要失效的目录
		directoriesToInvalidate[sourceDir] = true
		directoriesToInvalidate[targetFullPath] = true

		// 获取文件名
		filename := filepath.Base(decodedPath)

		// 构建目标文件完整路径
		targetFilePath := filepath.Join(targetFullPath, filename)

		// 检查目标文件是否已存在
		if _, err := os.Stat(targetFilePath); err == nil {
			// 文件已存在，生成新文件名
			ext := filepath.Ext(filename)
			nameWithoutExt := filename[:len(filename)-len(ext)]
			count := 1
			newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
			targetFilePath = filepath.Join(targetFullPath, newFilename)

			// 检查新文件名是否已存在
			for _, err := os.Stat(targetFilePath); err == nil; _, err = os.Stat(targetFilePath) {
				count++
				newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
				targetFilePath = filepath.Join(targetFullPath, newFilename)
			}
		}

		// 移动文件
		err = os.Rename(sourceFullPath, targetFilePath)
		if err != nil {
			failedCount++
			continue
		}

		movedCount++
	}

	// 使相关目录缓存失效
	for dir := range directoriesToInvalidate {
		invalidateCache(dir)
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功移动 %d 个文件，失败 %d 个", movedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 分享计数API处理函数
func IncrementShareHandler(w http.ResponseWriter, r *http.Request) {

	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取文件路径
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "缺少文件路径", http.StatusBadRequest)
		return
	}

	// 获取用户IP
	ip := utils.GetClientIP(r)

	// 增加分享计数 - 调用当前包中stats.go定义的IncrementShareCount函数
	IncrementShareCount(path, ip) // 这是stats.go中定义的函数

	// 返回成功响应
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("success"))
}

// 自定义排序数据结构
type CustomSort struct {
	Path     string    `json:"path"`
	FileList []string  `json:"fileList"`
	UpdateAt time.Time `json:"updateAt"`
}

// 保存自定义排序处理函数
func SaveCustomSortHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || (sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 解析请求体
	var sortData struct {
		Path     string   `json:"path"`
		FileList []string `json:"fileList"`
	}

	if err := json.NewDecoder(r.Body).Decode(&sortData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 确保config目录存在
	if err := os.MkdirAll("config", 0755); err != nil {
		log.Printf("创建配置目录失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 构建排序文件路径 - 使用单个sort.json文件
	sortFile := filepath.Join("config", "sort.json")

	// 读取现有的排序数据
	var allSortData map[string]CustomSort
	// 检查文件是否存在
	if _, err := os.Stat(sortFile); err == nil {
		// 文件存在，读取并解析
		file, err := os.ReadFile(sortFile)
		if err != nil {
			log.Printf("读取排序文件失败: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(file, &allSortData); err != nil {
			log.Printf("解析排序文件失败: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	} else {
		// 文件不存在，创建新的map
		allSortData = make(map[string]CustomSort)
	}

	// 更新或添加当前路径的排序数据
	allSortData[sortData.Path] = CustomSort{
		Path:     sortData.Path,
		FileList: sortData.FileList,
		UpdateAt: time.Now(),
	}

	// 保存所有排序数据到单个文件
	file, err := json.MarshalIndent(allSortData, "", "  ")
	if err != nil {
		log.Printf("序列化排序数据失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(sortFile, file, 0644); err != nil {
		log.Printf("写入排序文件失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 返回成功响应
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

// 获取自定义排序处理函数
func GetCustomSortHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取路径参数
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// 构建排序文件路径 - 使用单个sort.json文件
	sortFile := filepath.Join("config", "sort.json")

	// 读取排序文件
	file, err := os.ReadFile(sortFile)
	if err != nil {
		// 如果文件不存在，创建一个空的排序文件
		if os.IsNotExist(err) {
			// 创建空的排序数据map
			emptySortData := make(map[string]CustomSort)
			// 序列化空map
			emptyFile, err := json.MarshalIndent(emptySortData, "", "  ")
			if err != nil {
				log.Printf("序列化空排序数据失败: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			// 写入文件
			if err := os.WriteFile(sortFile, emptyFile, 0644); err != nil {
				log.Printf("创建排序文件失败: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}
		// 返回空的排序数据
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true, "fileList": []}`))
		return
	}

	// 解析排序数据 - 现在是包含所有路径的map
	var allSortData map[string]CustomSort
	if err := json.Unmarshal(file, &allSortData); err != nil {
		log.Printf("解析排序数据失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 获取当前路径的排序数据
	customSort, exists := allSortData[path]
	if !exists {
		// 如果当前路径没有排序数据，返回空列表
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true, "fileList": []}`))
		return
	}

	// 返回排序数据
	response := struct {
		Success  bool      `json:"success"`
		Path     string    `json:"path"`
		FileList []string  `json:"fileList"`
		UpdateAt time.Time `json:"updateAt"`
	}{
		Success:  true,
		Path:     customSort.Path,
		FileList: customSort.FileList,
		UpdateAt: customSort.UpdateAt,
	}

	responseJson, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("序列化响应数据失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJson)
}

// 批量复制处理函数
func BatchCopyHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以复制文件
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]
	targetPath := r.FormValue("target_path")

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要复制的文件&type=error", http.StatusFound)
		return
	}

	if targetPath == "" {
		http.Redirect(w, r, "/files?msg=请输入目标路径&type=error", http.StatusFound)
		return
	}

	// 尝试使用标准URL解码目标路径
	decodedTargetPath, err := url.QueryUnescape(targetPath)
	// 如果解码失败，使用DecodePath作为备选方案
	if err != nil {
		decodedTargetPath, _ = utils.DecodePath(targetPath)
		// 确保结果不为空
		if decodedTargetPath == "" {
			decodedTargetPath = utils.SanitizeFilename(targetPath)
		}
	}

	// 清理目标路径
	decodedTargetPath = filepath.Clean(decodedTargetPath)
	if strings.HasPrefix(decodedTargetPath, "..") {
		http.Redirect(w, r, "/files?msg=无效的目标路径&type=error", http.StatusFound)
		return
	}

	// 构建完整的目标路径
	targetFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, decodedTargetPath)

	// 确保目标目录存在
	os.MkdirAll(targetFullPath, 0755)

	// 记录日志 - 包含源路径和解码后的目标路径
	utils.LogUserAction(r, "batch_copy", fmt.Sprintf("批量复制文件: %v 到 %s (原始路径: %s)", files, decodedTargetPath, targetPath))

	// 批量复制文件
	var copiedCount int
	var failedCount int
	// 记录需要失效的目录缓存
	var directoriesToInvalidate map[string]bool = make(map[string]bool)

	for _, filePath := range files {
		// 首先尝试标准URL解码
		decodedPath, err := url.QueryUnescape(filePath)
		// 如果失败，使用增强的DecodePath函数
		if err != nil {
			decodedPath, err = utils.DecodePath(filePath)
			if err != nil {
				log.Printf("文件路径解码失败: %s, 错误: %v", filePath, err)
				failedCount++
				continue
			}
		}

		// 构建源文件完整路径（使用SafeJoin确保安全性）
		sourceFullPath := utils.SafeJoin(config.AppConfig.Server.DownloadDir, decodedPath)
		// 获取源目录路径
		sourceDir := filepath.Dir(sourceFullPath)
		// 记录需要失效的目录
		directoriesToInvalidate[sourceDir] = true
		directoriesToInvalidate[targetFullPath] = true

		// 获取文件信息
		sourceInfo, err := os.Stat(sourceFullPath)
		if err != nil {
			failedCount++
			continue
		}

		// 获取文件名
		filename := filepath.Base(decodedPath)

		// 构建目标文件完整路径
		targetFilePath := filepath.Join(targetFullPath, filename)

		// 检查目标文件是否已存在
		if _, err := os.Stat(targetFilePath); err == nil {
			// 文件已存在，生成新文件名
			ext := filepath.Ext(filename)
			nameWithoutExt := filename[:len(filename)-len(ext)]
			count := 1
			newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
			targetFilePath = filepath.Join(targetFullPath, newFilename)

			// 检查新文件名是否已存在
			for _, err := os.Stat(targetFilePath); err == nil; _, err = os.Stat(targetFilePath) {
				count++
				newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
				targetFilePath = filepath.Join(targetFullPath, newFilename)
			}
		}

		// 复制文件或目录
		if sourceInfo.IsDir() {
			// 复制目录
			err = copyDir(sourceFullPath, targetFilePath)
		} else {
			// 复制文件
			err = copyFile(sourceFullPath, targetFilePath)
		}

		if err != nil {
			failedCount++
			continue
		}

		copiedCount++
	}

	// 使相关目录缓存失效
	for dir := range directoriesToInvalidate {
		invalidateCache(dir)
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功复制 %d 个文件，失败 %d 个", copiedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 辅助函数：复制文件
func copyFile(src, dst string) error {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// 复制文件内容
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// 复制文件权限
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode())
}

// 辅助函数：检查字符串是否是URL编码的
func isURLEncoded(str string) bool {
	return strings.Contains(str, "%")
}

// 辅助函数：复制目录
func copyDir(src, dst string) error {
	// 创建目标目录
	os.MkdirAll(dst, 0755)

	// 读取源目录内容
	dirEntries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range dirEntries {
		name := entry.Name()
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if entry.IsDir() {
			// 递归复制子目录
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// 复制文件
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// 短链重定向处理函数
func ShortURLHandler(w http.ResponseWriter, r *http.Request) {
	// 获取短链代码
	shortCode := strings.TrimPrefix(r.URL.Path, "/s/")

	// 短链码只接受字母数字，长度固定，避免把子路径 / 目录穿越串带进查表
	if !isValidShortCode(shortCode) {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "short_url_invalid_code",
			fmt.Sprintf("IP: %s 请求非法短链码: %s", utils.GetClientIP(r), shortCode))
		shortCode = ""
	}

	if shortCode == "" {
		// 显示提示页面并自动跳转
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`
			<!DOCTYPE html>
			<html lang="zh-CN">
			<head>
				<meta charset="UTF-8">
				<meta name="viewport" content="width=device-width, initial-scale=1.0">
				<title>短链不存在</title>
				<meta http-equiv="refresh" content="3;url=/">

			</head>
			<body>
				<div class="container">
					<div class="title">短链不存在或已过期</div>
					<div class="message">当短链不存在或已过期，系统将自动为您返回首页</div>
					<div class="redirect">3秒后自动跳转...</div>
				</div>
			</body>
			</html>
		`))
		return
	}

	// 获取原始路径和文件名
	originalPath, _, exists := utils.GetOriginalPath(shortCode)
	if !exists {
		// 显示提示页面并自动跳转
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`
			<!DOCTYPE html>
			<html lang="zh-CN">
			<head>
				<meta charset="UTF-8">
				<meta name="viewport" content="width=device-width, initial-scale=1.0">
				<title>短链不存在</title>
				<meta http-equiv="refresh" content="3;url=/">

			</head>
			<body>
				<div class="container">
					<div class="title">短链不存在或已过期</div>
					<div class="message">当短链不存在或已过期，系统将自动为您返回首页</div>
					<div class="redirect">3秒后自动跳转...</div>
				</div>
			</body>
			</html>
		`))
		return
	}

	// 重定向到原始下载链接。
	// 路径必须转义，否则文件名中的 & # ? 等字符会被当成查询参数解析，造成参数注入。
	downloadURL := "/download?path=" + url.QueryEscape(originalPath)
	http.Redirect(w, r, downloadURL, http.StatusFound)
}

// isValidShortCode 校验短链码格式（6 位字母数字）
func isValidShortCode(code string) bool {
	if len(code) != utils.ShortCodeLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// DeleteShortURLHandler 撤销短链（管理员可删任意，普通用户仅可删自己创建的）
func DeleteShortURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := utils.GetClientIP(r)
	sess := session.GetCurrentUser(r)
	if sess == nil {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "short_url_delete_denied",
			fmt.Sprintf("IP: %s 未登录尝试删除短链", clientIP))
		writeShortURLJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false, "error": "请先登录",
		})
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		writeShortURLJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false, "error": "CSRF令牌验证失败，请刷新页面后重试",
		})
		return
	}

	code := r.URL.Query().Get("code")
	if !isValidShortCode(code) {
		writeShortURLJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "短链码格式不正确",
		})
		return
	}

	isAdmin := sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin
	if err := utils.DeleteShortURL(code, sess.Username, isAdmin); err != nil {
		status := http.StatusInternalServerError
		msg := "删除短链失败"
		switch {
		case errors.Is(err, utils.ErrShortURLNotFound):
			status, msg = http.StatusNotFound, "短链不存在"
		case errors.Is(err, utils.ErrShortURLForbidden):
			status, msg = http.StatusForbidden, "只能删除自己创建的短链"
		}
		writeShortURLJSON(w, status, map[string]interface{}{"success": false, "error": msg})
		return
	}

	utils.Log(utils.LogLevelInfo, sess.Username, "", "short_url_deleted",
		fmt.Sprintf("IP: %s 删除短链 %s", clientIP, code))
	writeShortURLJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// writeShortURLJSON 统一输出短链接口的 JSON 响应
func writeShortURLJSON(w http.ResponseWriter, status int, payload interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("序列化短链响应失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// 生成短链API处理函数
func GenerateShortURLHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := utils.GetClientIP(r)

	// 分享是公开能力，不要求登录。
	// 防滥用由 utils.GenerateShortURL 内部的「按目标文件去重」保证：
	// 同一文件永远复用同一条短链，无法无限注入。

	// 获取文件路径和文件名
	path := r.URL.Query().Get("path")
	filename := r.URL.Query().Get("filename")
	if path == "" {
		writeShortURLJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "缺少文件路径",
		})
		return
	}

	// 生成短链（内部完成路径校验、去重与限流）
	shortCode, err := utils.GenerateShortURL(path, clientIP)
	if err != nil {
		status := http.StatusInternalServerError
		msg := "生成短链失败，请稍后重试"

		switch {
		case errors.Is(err, utils.ErrShortURLInvalidPath):
			status = http.StatusBadRequest
			msg = "文件不存在或路径不合法"
		case errors.Is(err, utils.ErrShortURLQuotaExceeded):
			status = http.StatusTooManyRequests
			msg = err.Error()
		case errors.Is(err, utils.ErrShortURLRateLimited):
			status = http.StatusTooManyRequests
			msg = "短链生成过于频繁，请稍后再试"
		}

		// 只记录"路径探测"行为。限流与配额拒绝不记日志，
		// 否则攻击者可通过持续触发把日志写满。
		if errors.Is(err, utils.ErrShortURLInvalidPath) {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "short_url_probe",
				fmt.Sprintf("IP: %s 生成短链路径非法被拒绝，path=%s，原因: %v", clientIP, path, err))
		}

		writeShortURLJSON(w, status, map[string]interface{}{
			"success": false,
			"error":   msg,
		})
		return
	}

	// 构建短链URL
	shortURL := "/s/" + shortCode

	utils.Log(utils.LogLevelInfo, "anonymous", "guest", "short_url_created",
		fmt.Sprintf("IP: %s 短链 %s -> %s", clientIP, shortCode, path))

	// 返回短链和文件名
	response := struct {
		Success  bool   `json:"success"`
		ShortURL string `json:"short_url"`
		FullURL  string `json:"full_url"`
		Filename string `json:"filename"`
	}{
		Success:  true,
		ShortURL: shortURL,
		FullURL:  utils.GetRequestScheme(r) + "://" + r.Host + shortURL,
		Filename: filename,
	}

	responseJson, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("序列化响应数据失败: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJson)
}
