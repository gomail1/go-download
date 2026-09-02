//go:build !windows
// +build !windows

package utils

import (
	"math"
	"os"
	"syscall"
)

// getDiskUsage 获取磁盘使用率（Linux/Mac 平台实现）
func getDiskUsage() float64 {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		return 0
	}

	// 使用 Statfs 获取文件系统信息
	var stat syscall.Statfs_t
	err = syscall.Statfs(wd, &stat)
	if err != nil {
		return 0
	}

	// 计算总空间和可用空间
	// 在 Linux/Mac 上，Bsize 是块大小，Blocks 是总块数，Bavail 是可用块数
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)

	// 计算磁盘使用率
	if totalBytes == 0 {
		return 0
	}
	usedBytes := totalBytes - freeBytes
	usagePercent := float64(usedBytes) / float64(totalBytes) * 100

	// 保留两位小数
	return math.Round(usagePercent*100) / 100
}
