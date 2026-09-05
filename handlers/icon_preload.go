package handlers

import (
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/utils"
)

// PreloadFileIcons 后台预提取下载目录中所有可执行文件（.exe/.dll/.msi 等）的
// 内嵌图标并落盘缓存（config/icons/cache/）。
//
// 幂等且可重复执行：已缓存的文件内部直接命中（零额外开销），
// 因此既可用于启动首扫，也可用作周期性增量扫描。
// 配合公开的 /icon 接口，前端任何人（含未登录）访问文件图标时均可直接命中缓存。
func PreloadFileIcons() {
	root := config.AppConfig.Server.DownloadDir
	if root == "" || utils.GlobalIconCache == nil {
		return
	}

	start := time.Now()
	var total, okCount int
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// 跳过不可访问项与目录
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !utils.IsExecutableFile(ext) {
			return nil
		}
		total++
		// 已缓存文件内部直接命中，返回无额外开销
		if _, err := utils.GlobalIconCache.GetFileIcon(path); err == nil {
			okCount++
		}
		return nil
	})
	log.Printf("图标预提取完成：共 %d 个可执行文件，成功缓存 %d 个，耗时 %v", total, okCount, time.Since(start))
}

// IconPreloadInterval 图标增量预提取周期。服务运行期间周期性重扫下载目录，
// 使新上传 / 后台批量添加 / 直接拷入下载目录的可执行文件在下一周期自动提取缓存，
// 无需等待首次被访问（懒提取仍作为兜底即时生效）。
const IconPreloadInterval = time.Minute

// StartIconPreloadLoop 启动图标增量预提取循环：立即首扫一次，之后每个周期重扫。
// 阻塞式运行，调用方应以 go 启动。panic 不影响主服务。
func StartIconPreloadLoop() {
	PreloadFileIcons()

	ticker := time.NewTicker(IconPreloadInterval)
	defer ticker.Stop()
	for range ticker.C {
		func() {
			defer func() { _ = recover() }()
			PreloadFileIcons()
		}()
	}
}
