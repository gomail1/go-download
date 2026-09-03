package utils

import (
	"bytes"
	"crypto/md5"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
)

// 资源目录结构
type resourceDirectory struct {
	Characteristics      uint32
	TimeDateStamp        uint32
	MajorVersion         uint16
	MinorVersion         uint16
	NumberOfNamedEntries uint16
	NumberOfIdEntries    uint16
}

// 资源目录条目结构
type resourceDirectoryEntry struct {
	Name            uint32
	OffsetToData    uint32
}

// 资源数据条目结构
type resourceDataEntry struct {
	OffsetToData uint32
	Size         uint32
	CodePage     uint32
	Reserved     uint32
}

// IconCache 图标缓存结构
type IconCache struct {
	mu       sync.RWMutex
	cacheDir string
	icons    map[string]string // 文件哈希 -> 图标缓存路径
}

// GlobalIconCache 全局图标缓存实例
var GlobalIconCache *IconCache

// InitIconCache 初始化图标缓存
func InitIconCache(cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("创建图标缓存目录失败: %w", err)
	}

	GlobalIconCache = &IconCache{
		cacheDir: cacheDir,
		icons:    make(map[string]string),
	}

	// 加载已有的缓存图标
	GlobalIconCache.loadExistingIcons()

	return nil
}

// loadExistingIcons 加载已有的缓存图标
func (ic *IconCache) loadExistingIcons() {
	files, err := os.ReadDir(ic.cacheDir)
	if err != nil {
		return
	}

	ic.mu.Lock()
	defer ic.mu.Unlock()

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".png") {
			name := strings.TrimSuffix(file.Name(), ".png")
			ic.icons[name] = filepath.Join(ic.cacheDir, file.Name())
		}
	}
}

// getFileHash 获取文件内容的哈希值，用于缓存键
func getFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// 只读取文件前1MB用于哈希，避免大文件读取过慢
	hasher := md5.New()
	if _, err := io.CopyN(hasher, file, 1024*1024); err != nil && err != io.EOF {
		return "", err
	}

	// 同时包含文件大小和修改时间
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	hasher.Write([]byte(fmt.Sprintf("%d_%d", info.Size(), info.ModTime().Unix())))

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// readResourceDirectory 从读取器中读取资源目录
func readResourceDirectory(reader io.Reader) (*resourceDirectory, error) {
	var dir resourceDirectory
	if err := binary.Read(reader, binary.LittleEndian, &dir); err != nil {
		return nil, err
	}
	return &dir, nil
}

// readResourceDirectoryEntry 从读取器中读取资源目录条目
func readResourceDirectoryEntry(reader io.Reader) (*resourceDirectoryEntry, error) {
	var entry resourceDirectoryEntry
	if err := binary.Read(reader, binary.LittleEndian, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// readResourceDataEntry 从读取器中读取资源数据条目
func readResourceDataEntry(reader io.Reader) (*resourceDataEntry, error) {
	var entry resourceDataEntry
	if err := binary.Read(reader, binary.LittleEndian, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// virtualAddressToFileOffset 将虚拟地址转换为文件偏移
func virtualAddressToFileOffset(peFile *pe.File, virtualAddress uint32) int64 {
	for _, section := range peFile.Sections {
		if section.VirtualAddress <= virtualAddress &&
			section.VirtualAddress+section.VirtualSize > virtualAddress {
			return int64(section.Offset) + int64(virtualAddress-section.VirtualAddress)
		}
	}
	return 0
}

// ExtractIconFromPE 从Windows可执行文件中提取图标
func ExtractIconFromPE(filePath string) (image.Image, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 解析PE文件
	peFile, err := pe.NewFile(file)
	if err != nil {
		return nil, fmt.Errorf("解析PE文件失败: %w", err)
	}
	defer peFile.Close()

	// 获取资源目录（数据目录索引为2）
	var resourceDir *pe.DataDirectory
	switch optHeader := peFile.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		resourceDir = &optHeader.DataDirectory[2]
	case *pe.OptionalHeader64:
		resourceDir = &optHeader.DataDirectory[2]
	default:
		return nil, fmt.Errorf("不支持的PE文件格式")
	}

	if resourceDir == nil || resourceDir.VirtualAddress == 0 {
		return nil, fmt.Errorf("未找到资源目录")
	}

	// 计算资源段的文件偏移
	resourceOffset := virtualAddressToFileOffset(peFile, resourceDir.VirtualAddress)
	if resourceOffset == 0 {
		return nil, fmt.Errorf("无法计算资源段偏移")
	}

	// 创建读取器
	reader := io.NewSectionReader(file, 0, 1<<62)

	// 读取根资源目录
	reader.Seek(resourceOffset, io.SeekStart)
	rootDir, err := readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("读取资源目录头失败: %w", err)
	}

	// 查找图标组（类型ID为14）
	numEntries := int(rootDir.NumberOfNamedEntries + rootDir.NumberOfIdEntries)
	var iconGroupEntry *resourceDirectoryEntry
	for i := 0; i < numEntries; i++ {
		entry, err := readResourceDirectoryEntry(reader)
		if err != nil {
			continue
		}
		// 类型ID为14表示图标组
		if entry.Name == 14 {
			iconGroupEntry = entry
			break
		}
	}

	if iconGroupEntry == nil {
		return nil, fmt.Errorf("未找到图标组资源")
	}

	// 读取图标组目录
	iconGroupOffset := resourceOffset + int64(iconGroupEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(iconGroupOffset, io.SeekStart)
	iconGroupDir, err := readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标组目录失败: %w", err)
	}

	// 获取第一个图标组
	numIconEntries := int(iconGroupDir.NumberOfNamedEntries + iconGroupDir.NumberOfIdEntries)
	if numIconEntries == 0 {
		return nil, fmt.Errorf("图标组目录为空")
	}

	firstIconEntry, err := readResourceDirectoryEntry(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标组条目失败: %w", err)
	}

	// 读取图标组数据目录
	iconDataDirOffset := resourceOffset + int64(firstIconEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(iconDataDirOffset, io.SeekStart)
	iconDataDir, err := readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标数据目录失败: %w", err)
	}

	numDataEntries := int(iconDataDir.NumberOfNamedEntries + iconDataDir.NumberOfIdEntries)
	if numDataEntries == 0 {
		return nil, fmt.Errorf("图标数据目录为空")
	}

	// 读取第一个数据条目（语言）
	dataEntry, err := readResourceDirectoryEntry(reader)
	if err != nil {
		return nil, fmt.Errorf("读取数据条目失败: %w", err)
	}

	// 读取数据目录条目
	dataEntryOffset := resourceOffset + int64(dataEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(dataEntryOffset, io.SeekStart)
	dataDirEntry, err := readResourceDataEntry(reader)
	if err != nil {
		return nil, fmt.Errorf("读取数据目录条目失败: %w", err)
	}

	// 读取图标组数据
	iconGroupFileOffset := virtualAddressToFileOffset(peFile, dataDirEntry.OffsetToData)
	if iconGroupFileOffset == 0 {
		return nil, fmt.Errorf("无法计算图标组数据偏移")
	}

	iconGroupData := make([]byte, dataDirEntry.Size)
	reader.ReadAt(iconGroupData, iconGroupFileOffset)

	// 解析图标组（GRPICONDIR结构）
	if len(iconGroupData) < 6 {
		return nil, fmt.Errorf("图标组数据太短")
	}

	iconCount := int(binary.LittleEndian.Uint16(iconGroupData[4:6]))
	if iconCount == 0 {
		return nil, fmt.Errorf("图标组中没有图标")
	}

	// 找到最大的图标
	var bestIconID uint16
	var bestIconSize uint32
	for i := 0; i < iconCount; i++ {
		entryOffset := 6 + i*14
		if entryOffset+14 > len(iconGroupData) {
			break
		}
		width := iconGroupData[entryOffset]
		height := iconGroupData[entryOffset+1]
		iconID := binary.LittleEndian.Uint16(iconGroupData[entryOffset+12 : entryOffset+14])
		size := uint32(width) * uint32(height)
		if size > bestIconSize {
			bestIconSize = size
			bestIconID = iconID
		}
	}

	if bestIconID == 0 {
		return nil, fmt.Errorf("未找到合适的图标")
	}

	// 重新读取根资源目录，查找图标资源（类型ID为3）
	reader.Seek(resourceOffset, io.SeekStart)
	rootDir, err = readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("重新读取资源目录头失败: %w", err)
	}

	var iconEntry *resourceDirectoryEntry
	for i := 0; i < numEntries; i++ {
		entry, err := readResourceDirectoryEntry(reader)
		if err != nil {
			continue
		}
		// 类型ID为3表示图标
		if entry.Name == 3 {
			iconEntry = entry
			break
		}
	}

	if iconEntry == nil {
		return nil, fmt.Errorf("未找到图标资源")
	}

	// 读取图标目录
	iconDirOffset := resourceOffset + int64(iconEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(iconDirOffset, io.SeekStart)
	iconDir, err := readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标目录失败: %w", err)
	}

	numIconDirEntries := int(iconDir.NumberOfNamedEntries + iconDir.NumberOfIdEntries)

	// 查找指定ID的图标
	var targetIconEntry *resourceDirectoryEntry
	for i := 0; i < numIconDirEntries; i++ {
		entry, err := readResourceDirectoryEntry(reader)
		if err != nil {
			continue
		}
		if entry.Name == uint32(bestIconID) {
			targetIconEntry = entry
			break
		}
	}

	if targetIconEntry == nil {
		return nil, fmt.Errorf("未找到指定ID的图标")
	}

	// 读取图标数据目录
	targetIconDataDirOffset := resourceOffset + int64(targetIconEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(targetIconDataDirOffset, io.SeekStart)
	targetIconDataDir, err := readResourceDirectory(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标数据目录失败: %w", err)
	}

	numTargetDataEntries := int(targetIconDataDir.NumberOfNamedEntries + targetIconDataDir.NumberOfIdEntries)
	if numTargetDataEntries == 0 {
		return nil, fmt.Errorf("图标数据目录为空")
	}

	// 读取第一个数据条目
	targetDataEntry, err := readResourceDirectoryEntry(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标数据条目失败: %w", err)
	}

	// 读取数据目录条目
	targetDataEntryOffset := resourceOffset + int64(targetDataEntry.OffsetToData&0x7FFFFFFF)
	reader.Seek(targetDataEntryOffset, io.SeekStart)
	targetDataDirEntry, err := readResourceDataEntry(reader)
	if err != nil {
		return nil, fmt.Errorf("读取图标数据目录条目失败: %w", err)
	}

	// 读取图标数据
	iconFileOffset := virtualAddressToFileOffset(peFile, targetDataDirEntry.OffsetToData)
	if iconFileOffset == 0 {
		return nil, fmt.Errorf("无法计算图标数据偏移")
	}

	iconData := make([]byte, targetDataDirEntry.Size)
	reader.ReadAt(iconData, iconFileOffset)

	// 解析原始图标数据（PNG或BMP格式）
	img, err := parseIconData(iconData)
	if err != nil {
		return nil, fmt.Errorf("解析图标数据失败: %w", err)
	}

	return img, nil
}

// parseIconData 解析原始图标数据（PNG或BMP格式）
func parseIconData(data []byte) (image.Image, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("图标数据太短")
	}

	// 检查是否是PNG格式（Windows Vista及以后的图标常用PNG格式）
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		// PNG格式
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("解码PNG图标失败: %w", err)
		}
		return img, nil
	}

	// BMP格式（传统图标格式）
	// PE文件中的图标数据格式：BITMAPINFOHEADER + 颜色表 + 像素数据 + AND掩码
	img, err := parseBMPIcon(data)
	if err != nil {
		return nil, fmt.Errorf("解析BMP图标失败: %w", err)
	}

	return img, nil
}

// parseBMPIcon 解析BMP格式图标数据
func parseBMPIcon(data []byte) (image.Image, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("BMP数据太短")
	}

	// 解析BITMAPINFOHEADER
	biSize := binary.LittleEndian.Uint32(data[0:4])
	biWidth := int32(binary.LittleEndian.Uint32(data[4:8]))
	biHeight := int32(binary.LittleEndian.Uint32(data[8:12]))
	biPlanes := binary.LittleEndian.Uint16(data[12:14])
	biBitCount := binary.LittleEndian.Uint16(data[14:16])
	biCompression := binary.LittleEndian.Uint32(data[16:20])

	// 图标的高度是实际高度的2倍（包含XOR掩码和AND掩码）
	actualHeight := biHeight / 2

	if biWidth <= 0 || actualHeight <= 0 {
		return nil, fmt.Errorf("无效的图标尺寸: %dx%d", biWidth, actualHeight)
	}

	// 只支持未压缩的BMP
	if biCompression != 0 {
		return nil, fmt.Errorf("不支持压缩的BMP格式")
	}

	// 计算颜色表大小
	var colorTableSize int
	switch biBitCount {
	case 1:
		colorTableSize = 2
	case 4:
		colorTableSize = 16
	case 8:
		colorTableSize = 256
	case 16, 24, 32:
		colorTableSize = 0
	default:
		return nil, fmt.Errorf("不支持的颜色深度: %d", biBitCount)
	}

	// 颜色表偏移（BITMAPINFOHEADER大小 + 颜色表）
	colorTableOffset := int(biSize)
	pixelDataOffset := colorTableOffset + colorTableSize*4

	if pixelDataOffset >= len(data) {
		return nil, fmt.Errorf("像素数据偏移越界")
	}

	// 创建RGBA图像
	img := image.NewRGBA(image.Rect(0, 0, int(biWidth), int(actualHeight)))

	// 计算每行像素数据的字节数（4字节对齐）
	rowSize := ((int(biWidth)*int(biBitCount) + 31) / 32) * 4

	// 解析像素数据
	for y := int32(0); y < actualHeight; y++ {
		// BMP数据是倒序存储的
		srcY := actualHeight - 1 - y
		rowOffset := pixelDataOffset + int(srcY)*rowSize

		if rowOffset+rowSize > len(data) {
			break
		}

		for x := int32(0); x < biWidth; x++ {
			var r, g, b, a uint8

			switch biBitCount {
			case 32:
				pixelOffset := rowOffset + int(x)*4
				if pixelOffset+3 < len(data) {
					b = data[pixelOffset]
					g = data[pixelOffset+1]
					r = data[pixelOffset+2]
					a = data[pixelOffset+3]
				}
			case 24:
				pixelOffset := rowOffset + int(x)*3
				if pixelOffset+2 < len(data) {
					b = data[pixelOffset]
					g = data[pixelOffset+1]
					r = data[pixelOffset+2]
					a = 255
				}
			case 16:
				pixelOffset := rowOffset + int(x)*2
				if pixelOffset+1 < len(data) {
					color := binary.LittleEndian.Uint16(data[pixelOffset : pixelOffset+2])
					r = uint8((color>>10)&0x1F) << 3
					g = uint8((color>>5)&0x1F) << 3
					b = uint8(color&0x1F) << 3
					a = 255
				}
			case 8:
				pixelOffset := rowOffset + int(x)
				if pixelOffset < len(data) {
					colorIndex := data[pixelOffset]
					colorOffset := colorTableOffset + int(colorIndex)*4
					if colorOffset+3 < len(data) {
						b = data[colorOffset]
						g = data[colorOffset+1]
						r = data[colorOffset+2]
						a = 255
					}
				}
			case 4:
				byteOffset := rowOffset + int(x)/2
				if byteOffset < len(data) {
					var colorIndex uint8
					if x%2 == 0 {
						colorIndex = data[byteOffset] >> 4
					} else {
						colorIndex = data[byteOffset] & 0x0F
					}
					colorOffset := colorTableOffset + int(colorIndex)*4
					if colorOffset+3 < len(data) {
						b = data[colorOffset]
						g = data[colorOffset+1]
						r = data[colorOffset+2]
						a = 255
					}
				}
			case 1:
				byteOffset := rowOffset + int(x)/8
				bitOffset := uint(7 - (x % 8))
				if byteOffset < len(data) {
					colorIndex := (data[byteOffset] >> bitOffset) & 0x01
					colorOffset := colorTableOffset + int(colorIndex)*4
					if colorOffset+3 < len(data) {
						b = data[colorOffset]
						g = data[colorOffset+1]
						r = data[colorOffset+2]
						a = 255
					}
				}
			}

			// 处理AND掩码（透明通道）
			andMaskOffset := pixelDataOffset + int(actualHeight)*rowSize
			andRowSize := ((int(biWidth) + 31) / 32) * 4
			andRowOffset := andMaskOffset + int(srcY)*andRowSize
			andByteOffset := andRowOffset + int(x)/8
			andBitOffset := uint(7 - (x % 8))

			if andByteOffset < len(data) {
				andBit := (data[andByteOffset] >> andBitOffset) & 0x01
				if andBit == 1 {
					a = 0 // 透明
				}
			}

			img.SetRGBA(int(x), int(y), color.RGBA{R: r, G: g, B: b, A: a})
		}
	}

	_ = biPlanes // 保留变量，避免未使用警告
	return img, nil
}

// GetFileIcon 获取文件图标（带缓存）
func (ic *IconCache) GetFileIcon(filePath string) (string, error) {
	// 检查是否是支持的文件类型
	ext := strings.ToLower(filepath.Ext(filePath))
	if !IsExecutableFile(ext) {
		return "", fmt.Errorf("不支持的文件类型: %s", ext)
	}

	// 获取文件哈希作为缓存键
	hash, err := getFileHash(filePath)
	if err != nil {
		return "", fmt.Errorf("计算文件哈希失败: %w", err)
	}

	// 检查缓存
	ic.mu.RLock()
	if cachedPath, exists := ic.icons[hash]; exists {
		ic.mu.RUnlock()
		return cachedPath, nil
	}
	ic.mu.RUnlock()

	// 提取图标
	img, err := ExtractIconFromPE(filePath)
	if err != nil {
		return "", fmt.Errorf("提取图标失败: %w", err)
	}

	// 调整图标大小为64x64
	img = imaging.Resize(img, 64, 64, imaging.Lanczos)

	// 保存为PNG
	iconPath := filepath.Join(ic.cacheDir, hash+".png")
	if err := saveImageAsPNG(img, iconPath); err != nil {
		return "", fmt.Errorf("保存图标失败: %w", err)
	}

	// 更新缓存
	ic.mu.Lock()
	ic.icons[hash] = iconPath
	ic.mu.Unlock()

	return iconPath, nil
}

// saveImageAsPNG 保存图像为PNG文件
func saveImageAsPNG(img image.Image, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return png.Encode(file, img)
}

// IsExecutableFile 检查是否是可执行文件类型
func IsExecutableFile(ext string) bool {
	executableExts := []string{".exe", ".msi", ".dll", ".ocx", ".cpl", ".scr"}
	for _, e := range executableExts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// GetIconURL 获取图标的URL路径
func GetIconURL(iconPath string, baseURL string) string {
	if iconPath == "" {
		return ""
	}
	// 转换为相对路径
	relPath, err := filepath.Rel(".", iconPath)
	if err != nil {
		return ""
	}
	// 替换反斜杠为正斜杠
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	return baseURL + "/" + relPath
}

// ExtractIconToBuffer 提取图标到内存缓冲区
func ExtractIconToBuffer(filePath string) ([]byte, error) {
	img, err := ExtractIconFromPE(filePath)
	if err != nil {
		return nil, err
	}

	// 调整大小
	img = imaging.Resize(img, 64, 64, imaging.Lanczos)

	// 编码为PNG
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
