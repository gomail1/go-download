package utils

import (
	"html"
	"regexp"
	"strings"
)

// XSS防护工具

// EscapeHTML 对字符串进行HTML实体编码，防止XSS攻击
func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

// UnescapeHTML 对HTML实体编码的字符串进行解码
func UnescapeHTML(s string) string {
	return html.UnescapeString(s)
}

// SanitizeHTML 清理HTML内容，移除危险的标签和属性
// 注意：这是一个基础实现，对于复杂的HTML内容建议使用专业的HTML清理库
func SanitizeHTML(s string) string {
	if s == "" {
		return s
	}

	// 移除script标签及其内容
	s = removeTag(s, "script")
	// 移除style标签及其内容
	s = removeTag(s, "style")
	// 移除iframe标签
	s = removeTag(s, "iframe")
	// 移除object标签
	s = removeTag(s, "object")
	// 移除embed标签
	s = removeTag(s, "embed")
	// 移除form标签
	s = removeTag(s, "form")
	// 移除input标签
	s = removeTag(s, "input")
	// 移除button标签
	s = removeTag(s, "button")
	// 移除textarea标签
	s = removeTag(s, "textarea")
	// 移除select标签
	s = removeTag(s, "select")
	// 移除link标签
	s = removeTag(s, "link")
	// 移除meta标签
	s = removeTag(s, "meta")

	// 移除危险的事件处理属性
	s = removeEventHandlers(s)

	// 移除javascript:协议
	s = removeJavaScriptProtocol(s)

	// 移除data:协议（可能包含恶意脚本）
	// 注意：这可能会影响正常的base64图片，根据需要启用
	// s = removeDataProtocol(s)

	return s
}

// removeTag 移除指定的HTML标签及其内容
func removeTag(s, tag string) string {
	// 匹配开始标签到结束标签的内容（包括标签本身）
	// 使用非贪婪匹配
	re := regexp.MustCompile(`(?i)<` + tag + `[^>]*>.*?</` + tag + `>`)
	s = re.ReplaceAllString(s, "")

	// 移除自闭合标签
	reSelf := regexp.MustCompile(`(?i)<` + tag + `[^>]*/>`)
	s = reSelf.ReplaceAllString(s, "")

	// 移除没有结束标签的开始标签
	reOpen := regexp.MustCompile(`(?i)<` + tag + `[^>]*>`)
	s = reOpen.ReplaceAllString(s, "")

	return s
}

// removeEventHandlers 移除危险的事件处理属性
func removeEventHandlers(s string) string {
	// 匹配on开头的事件处理属性（如onclick, onload, onerror等）
	re := regexp.MustCompile(`(?i)\s+on\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	return re.ReplaceAllString(s, "")
}

// removeJavaScriptProtocol 移除javascript:协议
func removeJavaScriptProtocol(s string) string {
	// 匹配href, src等属性中的javascript:协议
	re := regexp.MustCompile(`(?i)(href|src|action|formaction)\s*=\s*("|')?\s*javascript:[^"'>\s]*("|')?`)
	return re.ReplaceAllString(s, "")
}

// removeDataProtocol 移除data:协议（可能包含恶意脚本）
func removeDataProtocol(s string) string {
	re := regexp.MustCompile(`(?i)(href|src)\s*=\s*("|')?\s*data:[^"'>\s]*("|')?`)
	return re.ReplaceAllString(s, "")
}

// IsValidURL 验证URL是否安全（防止javascript:等危险协议）
func IsValidURL(url string) bool {
	if url == "" {
		return false
	}

	// 转换为小写进行比较
	lowerURL := strings.ToLower(strings.TrimSpace(url))

	// 检查是否以javascript:开头
	if strings.HasPrefix(lowerURL, "javascript:") {
		return false
	}

	// 检查是否以data:开头（可能包含恶意脚本）
	if strings.HasPrefix(lowerURL, "data:") {
		// 允许常见的图片data URI
		if strings.HasPrefix(lowerURL, "data:image/") {
			return true
		}
		return false
	}

	// 检查是否以vbscript:开头
	if strings.HasPrefix(lowerURL, "vbscript:") {
		return false
	}

	return true
}

// SanitizeURL 清理URL，移除危险协议
func SanitizeURL(url string) string {
	if !IsValidURL(url) {
		return "#"
	}
	return url
}

// TruncateString 截断字符串到指定长度
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// NormalizeWhitespace 规范化空白字符
func NormalizeWhitespace(s string) string {
	// 移除首尾空白
	s = strings.TrimSpace(s)
	// 将多个连续空白字符替换为单个空格
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(s, " ")
}

// RemoveControlCharacters 移除控制字符
func RemoveControlCharacters(s string) string {
	re := regexp.MustCompile(`[\x00-\x1F\x7F]`)
	return re.ReplaceAllString(s, "")
}

// SafeText 安全文本处理（组合多个安全处理）
func SafeText(s string) string {
	// 移除控制字符
	s = RemoveControlCharacters(s)
	// 规范化空白字符
	s = NormalizeWhitespace(s)
	// HTML实体编码
	s = EscapeHTML(s)
	return s
}

// SafeAttribute 安全的HTML属性值处理
func SafeAttribute(s string) string {
	// 移除控制字符
	s = RemoveControlCharacters(s)
	// HTML实体编码
	s = EscapeHTML(s)
	// 移除引号（防止属性注入）
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}
