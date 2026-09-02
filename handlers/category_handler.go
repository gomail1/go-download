package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// CategoryResponse 分类响应
type CategoryResponse struct {
	Success bool         `json:"success"`
	Message string       `json:"message,omitempty"`
	Data    interface{}  `json:"data,omitempty"`
}

// writeAuthError 返回未登录/无权限的JSON响应
func writeAuthError(w http.ResponseWriter) {
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(CategoryResponse{
		Success: false,
		Message: "未登录或无权限执行此操作",
	})
}

// requireCategoryAdmin 校验分类管理写操作需要管理员/二级管理员登录
func requireCategoryAdmin(w http.ResponseWriter, r *http.Request) bool {
	sess := session.GetCurrentUser(r)
	if sess == nil {
		writeAuthError(w)
		return false
	}
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		writeAuthError(w)
		return false
	}
	return true
}

// writeCSRFError 返回CSRF校验失败的JSON响应
func writeCSRFError(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(CategoryResponse{
		Success: false,
		Message: "CSRF令牌验证失败，请刷新页面后重试",
	})
}

// CategoriesHandler 分类管理API
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	cm := utils.GetCategoryManager()

	switch r.Method {
	case "GET":
		// 获取所有分类
		categories := cm.GetCategories()
		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Data:    categories,
		})

	case "POST":
		// 校验管理员权限
		if !requireCategoryAdmin(w, r) {
			return
		}

		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			writeCSRFError(w)
			return
		}

		// 创建分类
		var req struct {
			Name string `json:"name"`
			Icon string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "请求参数错误",
			})
			return
		}

		if req.Name == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "分类名称不能为空",
			})
			return
		}

		if req.Icon == "" {
			req.Icon = "📁"
		}

		category, err := cm.CreateCategory(req.Name, req.Icon)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Message: "分类创建成功",
			Data:    category,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(CategoryResponse{
			Success: false,
			Message: "不支持的请求方法",
		})
	}
}

// CategoryDetailHandler 分类详情API（更新、删除）
func CategoryDetailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 从URL路径中提取分类ID
	path := strings.TrimPrefix(r.URL.Path, "/api/categories/")
	id := strings.TrimSuffix(path, "/")

	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CategoryResponse{
			Success: false,
			Message: "分类ID不能为空",
		})
		return
	}

	cm := utils.GetCategoryManager()

	switch r.Method {
	case "PUT":
		// 校验管理员权限
		if !requireCategoryAdmin(w, r) {
			return
		}

		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			writeCSRFError(w)
			return
		}

		// 更新分类
		var req struct {
			Name string `json:"name"`
			Icon string `json:"icon"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "请求参数错误",
			})
			return
		}

		if req.Name == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "分类名称不能为空",
			})
			return
		}

		category, err := cm.UpdateCategory(id, req.Name, req.Icon)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Message: "分类更新成功",
			Data:    category,
		})

	case "DELETE":
		// 校验管理员权限
		if !requireCategoryAdmin(w, r) {
			return
		}

		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			writeCSRFError(w)
			return
		}

		// 删除分类
		err := cm.DeleteCategory(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Message: "分类删除成功",
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(CategoryResponse{
			Success: false,
			Message: "不支持的请求方法",
		})
	}
}

// FileCategoryHandler 文件分类API
func FileCategoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	cm := utils.GetCategoryManager()

	switch r.Method {
	case "GET":
		// 获取文件的分类
		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "文件路径不能为空",
			})
			return
		}

		categoryID := cm.GetFileCategory(filePath)
		category, _ := cm.GetCategoryByID(categoryID)

		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Data: map[string]interface{}{
				"category_id":   categoryID,
				"category_name": category.Name,
			},
		})

	case "POST":
		// 校验管理员权限
		if !requireCategoryAdmin(w, r) {
			return
		}

		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			writeCSRFError(w)
			return
		}

		// 设置文件的分类
		var req struct {
			FilePath   string `json:"file_path"`
			CategoryID string `json:"category_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "请求参数错误",
			})
			return
		}

		if req.FilePath == "" || req.CategoryID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: "文件路径和分类ID不能为空",
			})
			return
		}

		err := cm.SetFileCategory(req.FilePath, req.CategoryID)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(CategoryResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(CategoryResponse{
			Success: true,
			Message: "文件分类设置成功",
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(CategoryResponse{
			Success: false,
			Message: "不支持的请求方法",
		})
	}
}
