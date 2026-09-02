//go:build windows
// +build windows

package utils

import (
	"math"
	"os"
	"syscall"
	"unsafe"
)

// getDiskUsage 获取磁盘使用率（Windows 平台实现）
func getDiskUsage() float64 {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		return 0
	}

	// 动态加载 kernel32.dll 中的 GetDiskFreeSpaceExW 函数
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64

	// 将路径转换为 UTF-16 指针
	pathPtr, err := syscall.UTF16PtrFromString(wd)
	if err != nil {
		return 0
	}

	// 调用 GetDiskFreeSpaceExW 获取磁盘空间信息
	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if ret == 0 {
		return 0
	}

	// 计算磁盘使用率
	if totalNumberOfBytes == 0 {
		return 0
	}
	usedBytes := totalNumberOfBytes - totalNumberOfFreeBytes
	usagePercent := float64(usedBytes) / float64(totalNumberOfBytes) * 100

	// 保留两位小数
	return math.Round(usagePercent*100) / 100
}
