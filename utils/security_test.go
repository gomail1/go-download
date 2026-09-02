package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateSafePath 测试路径安全验证
func TestValidateSafePath(t *testing.T) {
	// 创建临时目录作为基础目录
	tempDir, err := os.MkdirTemp("", "test_safe_path")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		userPath string
		isSafe   bool
	}{
		{"正常路径", "test.txt", true},
		{"子目录路径", "subdir/test.txt", true},
		{"当前目录", ".", true},
		{"路径遍历", "../test.txt", false},
		{"多级路径遍历", "../../test.txt", false},
		{"中间路径遍历", "subdir/../../test.txt", false},
		{"空路径", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateSafePath(tempDir, tt.userPath)
			if result.IsSafe != tt.isSafe {
				t.Errorf("ValidateSafePath(%q) = %v, want %v", tt.userPath, result.IsSafe, tt.isSafe)
			}
		})
	}
}

// TestIsPathSafe 测试简单路径安全检查
func TestIsPathSafe(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_path_safe")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if !IsPathSafe(tempDir, "test.txt") {
		t.Error("正常路径应该被认为是安全的")
	}

	if IsPathSafe(tempDir, "../test.txt") {
		t.Error("路径遍历应该被认为是不安全的")
	}
}

// TestSanitizeFilenameSafe 测试文件名清理
func TestSanitizeFilenameSafe(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"正常文件名", "test.txt", "test.txt"},
		{"包含路径分隔符", "path/test.txt", "path_test.txt"},
		{"包含反斜杠", "path\\test.txt", "path_test.txt"},
		{"包含点点", "../test.txt", "__test.txt"},
		{"包含特殊字符", "test:*.txt", "test__.txt"},
		{"开头有点", ".hidden", "hidden"},
		{"空文件名", "", "unnamed_file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilenameSafe(tt.filename)
			if result != tt.want {
				t.Errorf("SanitizeFilenameSafe(%q) = %q, want %q", tt.filename, result, tt.want)
			}
		})
	}
}

// TestEscapeHTML 测试HTML实体编码
func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"正常文本", "hello world", "hello world"},
		{"包含小于号", "<script>", "&lt;script&gt;"},
		{"包含大于号", "a > b", "a &gt; b"},
		{"包含和号", "a & b", "a &amp; b"},
		{"包含引号", `"hello"`, "&#34;hello&#34;"},
		{"包含单引号", `'hello'`, "&#39;hello&#39;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeHTML(tt.input)
			if result != tt.want {
				t.Errorf("EscapeHTML(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

// TestIsValidURL 测试URL验证
func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"正常HTTP URL", "http://example.com", true},
		{"正常HTTPS URL", "https://example.com", true},
		{"相对路径", "/path/to/file", true},
		{"javascript协议", "javascript:alert(1)", false},
		{"vbscript协议", "vbscript:msgbox(1)", false},
		{"data图片", "data:image/png;base64,abc", true},
		{"data脚本", "data:text/html,<script>alert(1)</script>", false},
		{"空URL", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidURL(tt.url)
			if result != tt.want {
				t.Errorf("IsValidURL(%q) = %v, want %v", tt.url, result, tt.want)
			}
		})
	}
}

// TestGenerateCSRFToken 测试CSRF令牌生成
func TestGenerateCSRFToken(t *testing.T) {
	token1, err := GenerateCSRFToken()
	if err != nil {
		t.Fatalf("生成CSRF令牌失败: %v", err)
	}

	token2, err := GenerateCSRFToken()
	if err != nil {
		t.Fatalf("生成CSRF令牌失败: %v", err)
	}

	if token1 == token2 {
		t.Error("两次生成的CSRF令牌不应该相同")
	}

	if len(token1) != 64 { // 32字节的十六进制编码应该是64个字符
		t.Errorf("CSRF令牌长度应该是64，实际是 %d", len(token1))
	}
}

// TestCSRFTokenValidation 测试CSRF令牌验证
func TestCSRFTokenValidation(t *testing.T) {
	sessionID := "test_session"

	// 设置CSRF令牌
	token, err := SetCSRFToken(sessionID)
	if err != nil {
		t.Fatalf("设置CSRF令牌失败: %v", err)
	}

	// 验证正确的令牌
	if !ValidateCSRFToken(sessionID, token) {
		t.Error("正确的CSRF令牌应该验证通过")
	}

	// 验证错误的令牌
	if ValidateCSRFToken(sessionID, "wrong_token") {
		t.Error("错误的CSRF令牌应该验证失败")
	}

	// 验证不存在的会话
	if ValidateCSRFToken("nonexistent_session", token) {
		t.Error("不存在的会话应该验证失败")
	}

	// 清除CSRF令牌
	ClearCSRFToken(sessionID)

	// 验证已清除的令牌
	if ValidateCSRFToken(sessionID, token) {
		t.Error("已清除的CSRF令牌应该验证失败")
	}
}

// TestSanitizeLogContent 测试日志内容脱敏
func TestSanitizeLogContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		check   func(string) bool
	}{
		{
			name:    "IP地址脱敏",
			content: "用户IP: 192.168.1.100 登录成功",
			check:   func(s string) bool { return !strings.Contains(s, "192.168.1.100") && strings.Contains(s, "192.168.***.***") },
		},
		{
			name:    "密码脱敏",
			content: "password=secret123",
			check:   func(s string) bool { return !strings.Contains(s, "secret123") && strings.Contains(s, "password=***") },
		},
		{
			name:    "令牌脱敏",
			content: "token=abc123def456",
			check:   func(s string) bool { return !strings.Contains(s, "abc123def456") && strings.Contains(s, "token=***") },
		},
		{
			name:    "邮箱脱敏",
			content: "用户邮箱: test@example.com",
			check:   func(s string) bool { return !strings.Contains(s, "test@example.com") && strings.Contains(s, "t***@example.com") },
		},
		{
			name:    "手机号脱敏",
			content: "手机号: 13812345678",
			check:   func(s string) bool { return !strings.Contains(s, "13812345678") && strings.Contains(s, "138****5678") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeLogContent(tt.content)
			if !tt.check(result) {
				t.Errorf("SanitizeLogContent(%q) = %q, 检查失败", tt.content, result)
			}
		})
	}
}

// TestGetFileExtension 测试文件扩展名获取
func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"正常扩展名", "test.txt", ".txt"},
		{"大写扩展名", "TEST.TXT", ".txt"},
		{"多个点", "test.tar.gz", ".gz"},
		{"无扩展名", "test", ""},
		{"隐藏文件", ".gitignore", ".gitignore"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFileExtension(tt.filename)
			if result != tt.want {
				t.Errorf("GetFileExtension(%q) = %q, want %q", tt.filename, result, tt.want)
			}
		})
	}
}

// TestIsDangerousFileType 测试危险文件类型检查
func TestIsDangerousFileType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"PHP文件", "test.php", true},
		{"ASP文件", "test.asp", true},
		{"JSP文件", "test.jsp", true},
		{"Python文件", "test.py", true},
		{"Shell脚本", "test.sh", true},
		{"EXE文件", "test.exe", true},
		{"文本文件", "test.txt", false},
		{"图片文件", "test.jpg", false},
		{"PDF文件", "test.pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDangerousFileType(tt.filename)
			if result != tt.want {
				t.Errorf("IsDangerousFileType(%q) = %v, want %v", tt.filename, result, tt.want)
			}
		})
	}
}

// TestFileExists 测试文件存在检查
func TestFileExists(t *testing.T) {
	// 创建临时文件
	tempFile, err := os.CreateTemp("", "test_exists")
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	if !FileExists(tempFile.Name()) {
		t.Error("存在的文件应该返回true")
	}

	if FileExists(filepath.Join(os.TempDir(), "nonexistent_file_12345.txt")) {
		t.Error("不存在的文件应该返回false")
	}
}

// TestNormalizeWhitespace 测试空白字符规范化
func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"正常文本", "hello world", "hello world"},
		{"多个空格", "hello   world", "hello world"},
		{"制表符", "hello\tworld", "hello world"},
		{"换行符", "hello\nworld", "hello world"},
		{"首尾空白", "  hello world  ", "hello world"},
		{"混合空白", "  hello   \t  world  \n", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeWhitespace(tt.input)
			if result != tt.want {
				t.Errorf("NormalizeWhitespace(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

// TestRemoveControlCharacters 测试控制字符移除
func TestRemoveControlCharacters(t *testing.T) {
	input := "hello\x00\x01\x02world\x7F"
	want := "helloworld"
	result := RemoveControlCharacters(input)
	if result != want {
		t.Errorf("RemoveControlCharacters(%q) = %q, want %q", input, result, want)
	}
}

// TestTruncateString 测试字符串截断
func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"短字符串", "hello", 10, "hello"},
		{"长字符串", "hello world", 8, "hello..."},
		{"正好长度", "hello", 5, "hello"},
		{"最大长度3", "hello", 3, "hel"},
		{"最大长度0", "hello", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateString(tt.input, tt.maxLen)
			if result != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.want)
			}
		})
	}
}
