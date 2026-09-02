package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Category 分类数据结构
type Category struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"created_at"`
}

// FileCategoryMapping 文件分类映射
type FileCategoryMapping struct {
	FilePath   string `json:"file_path"`
	CategoryID string `json:"category_id"`
	UpdatedAt  string `json:"updated_at"`
}

// CategoryManager 分类管理器
type CategoryManager struct {
	categories     []Category
	fileMappings   map[string]string // file_path -> category_id
	categoriesFile string
	mappingsFile   string
	mu             sync.RWMutex
}

var (
	categoryManager     *CategoryManager
	categoryManagerOnce sync.Once
)

// GetCategoryManager 获取分类管理器单例
func GetCategoryManager() *CategoryManager {
	categoryManagerOnce.Do(func() {
		dataDir := filepath.Join(".", "data")
		os.MkdirAll(dataDir, 0755)

		categoryManager = &CategoryManager{
			categories:     make([]Category, 0),
			fileMappings:   make(map[string]string),
			categoriesFile: filepath.Join(dataDir, "categories.json"),
			mappingsFile:   filepath.Join(dataDir, "file_category_mappings.json"),
		}
		categoryManager.load()
	})
	return categoryManager
}

// load 从文件加载分类数据
func (cm *CategoryManager) load() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 加载分类
	if data, err := os.ReadFile(cm.categoriesFile); err == nil {
		json.Unmarshal(data, &cm.categories)
	}

	// 如果没有分类，创建默认分类
	if len(cm.categories) == 0 {
		cm.categories = []Category{
			{ID: "default", Name: "全部", Icon: "📁", Sort: 0, CreatedAt: time.Now().Format("2006-01-02 15:04:05")},
			{ID: "software", Name: "常用软件", Icon: "💿", Sort: 1, CreatedAt: time.Now().Format("2006-01-02 15:04:05")},
			{ID: "system", Name: "系统镜像", Icon: "💻", Sort: 2, CreatedAt: time.Now().Format("2006-01-02 15:04:05")},
			{ID: "archive", Name: "压缩包", Icon: "📦", Sort: 3, CreatedAt: time.Now().Format("2006-01-02 15:04:05")},
			{ID: "document", Name: "办公文档", Icon: "📄", Sort: 4, CreatedAt: time.Now().Format("2006-01-02 15:04:05")},
		}
		cm.saveCategories()
	}

	// 加载文件分类映射
	if data, err := os.ReadFile(cm.mappingsFile); err == nil {
		var mappings []FileCategoryMapping
		json.Unmarshal(data, &mappings)
		for _, m := range mappings {
			cm.fileMappings[m.FilePath] = m.CategoryID
		}
	}
}

// saveCategories 保存分类到文件
func (cm *CategoryManager) saveCategories() error {
	data, err := json.MarshalIndent(cm.categories, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.categoriesFile, data, 0644)
}

// saveMappings 保存文件分类映射到文件
func (cm *CategoryManager) saveMappings() error {
	mappings := make([]FileCategoryMapping, 0)
	for filePath, categoryID := range cm.fileMappings {
		mappings = append(mappings, FileCategoryMapping{
			FilePath:   filePath,
			CategoryID: categoryID,
			UpdatedAt:  time.Now().Format("2006-01-02 15:04:05"),
		})
	}
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.mappingsFile, data, 0644)
}

// GetCategories 获取所有分类（按排序）
func (cm *CategoryManager) GetCategories() []Category {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]Category, len(cm.categories))
	copy(result, cm.categories)
	return result
}

// GetCategoryByID 根据ID获取分类
func (cm *CategoryManager) GetCategoryByID(id string) (*Category, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, c := range cm.categories {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("分类不存在: %s", id)
}

// CreateCategory 创建分类
func (cm *CategoryManager) CreateCategory(name, icon string) (*Category, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 检查名称是否重复
	for _, c := range cm.categories {
		if c.Name == name {
			return nil, fmt.Errorf("分类名称已存在: %s", name)
		}
	}

	// 生成ID
	id := fmt.Sprintf("cat_%d", time.Now().UnixNano())
	maxSort := 0
	for _, c := range cm.categories {
		if c.Sort > maxSort {
			maxSort = c.Sort
		}
	}

	category := Category{
		ID:        id,
		Name:      name,
		Icon:      icon,
		Sort:      maxSort + 1,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	cm.categories = append(cm.categories, category)
	cm.saveCategories()

	return &category, nil
}

// UpdateCategory 更新分类
func (cm *CategoryManager) UpdateCategory(id, name, icon string) (*Category, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i, c := range cm.categories {
		if c.ID == id {
			// 检查名称是否重复（排除自己）
			for _, other := range cm.categories {
				if other.ID != id && other.Name == name {
					return nil, fmt.Errorf("分类名称已存在: %s", name)
				}
			}

			cm.categories[i].Name = name
			cm.categories[i].Icon = icon
			cm.saveCategories()
			return &cm.categories[i], nil
		}
	}
	return nil, fmt.Errorf("分类不存在: %s", id)
}

// DeleteCategory 删除分类
func (cm *CategoryManager) DeleteCategory(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 不允许删除"全部"分类
	if id == "default" {
		return fmt.Errorf("不能删除默认分类")
	}

	found := false
	for i, c := range cm.categories {
		if c.ID == id {
			cm.categories = append(cm.categories[:i], cm.categories[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("分类不存在: %s", id)
	}

	// 将该分类下的文件移到"全部"分类
	for filePath, categoryID := range cm.fileMappings {
		if categoryID == id {
			cm.fileMappings[filePath] = "default"
		}
	}

	cm.saveCategories()
	cm.saveMappings()
	return nil
}

// GetFileCategory 获取文件的分类ID
func (cm *CategoryManager) GetFileCategory(filePath string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 统一路径分隔符为正斜杠，确保key一致
	normalizedPath := NormalizePath(filePath)

	if categoryID, ok := cm.fileMappings[normalizedPath]; ok {
		return categoryID
	}
	return "default"
}

// SetFileCategory 设置文件的分类
func (cm *CategoryManager) SetFileCategory(filePath, categoryID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 检查分类是否存在
	found := false
	for _, c := range cm.categories {
		if c.ID == categoryID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("分类不存在: %s", categoryID)
	}

	// 统一路径分隔符为正斜杠，确保key一致
	normalizedPath := NormalizePath(filePath)

	cm.fileMappings[normalizedPath] = categoryID
	cm.saveMappings()
	return nil
}

// GetFilesByCategory 获取指定分类下的文件路径列表
func (cm *CategoryManager) GetFilesByCategory(categoryID string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]string, 0)
	for filePath, catID := range cm.fileMappings {
		if catID == categoryID {
			result = append(result, filePath)
		}
	}
	return result
}
