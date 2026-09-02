package handlers

// 短链安全验证用例（v1.2.0）
//
// 修复目标：分享短链无需登录，但外部不能通过反复请求向服务器无限注入短链。
// 手段：按目标文件去重——同一文件永远复用同一条短链，短链总数上界 = 文件总数。
//
// 运行：go test ./handlers/ -run TestShortURL -v

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-download-server/config"
	"go-download-server/utils"
)

const shareTestFile = "a&b#c.iso" // 含 URL 保留字符，用于验证转义

// setupDownloadDir 准备下载目录与测试文件，返回文件名
func setupDownloadDir(t *testing.T) string {
	t.Helper()

	if config.AppConfig.Server.DownloadDir == "" {
		config.AppConfig.Server.DownloadDir = filepath.Join(os.TempDir(), "gd-shorturl-test")
	}
	if err := os.MkdirAll(config.AppConfig.Server.DownloadDir, 0755); err != nil {
		t.Fatalf("创建下载目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(config.AppConfig.Server.DownloadDir, shareTestFile), []byte("x"), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	return shareTestFile
}

// makeFile 在下载目录内新建一个文件，返回其相对路径
func makeFile(t *testing.T, name string) string {
	t.Helper()
	dir := config.AppConfig.Server.DownloadDir
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	return name
}

func postShare(t *testing.T, rawQuery string, ip string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/generate-short-url?"+rawQuery, strings.NewReader(""))
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	GenerateShortURLHandler(w, req)

	var out map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// 1. 分享无需登录：不带任何会话也必须能生成
func TestShortURLWorksWithoutLogin(t *testing.T) {
    utils.ResetShortURLStore()

	rel := setupDownloadDir(t)
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5000, RateWindow: 600000000000})

	w, out := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", "10.1.1.1")
	t.Logf("未登录请求 -> HTTP %d, body=%s", w.Code, strings.TrimSpace(w.Body.String()))
	if w.Code != http.StatusOK {
		t.Errorf("✗ 分享不应要求登录，但返回 HTTP %d", w.Code)
	}
	if out["short_url"] == nil {
		t.Errorf("✗ 未返回短链: %v", out)
	}
}

// 2. 核心：同一文件反复请求，只能有 1 条短链，不能无限注入
func TestShortURLNoUnlimitedInjection(t *testing.T) {
    utils.ResetShortURLStore()

	rel := setupDownloadDir(t)
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5000, RateWindow: 600000000000})

	before := utils.CountShortURLs()

	// 先生成一次，拿到基准短链
	_, first := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", "10.2.2.2")
	base, _ := first["short_url"].(string)
	if base == "" {
		t.Fatalf("首次生成失败: %v", first)
	}

	const rounds = 2000
	same := 0
	for i := 0; i < rounds; i++ {
		// 模拟攻击者变换 IP 反复请求
		_, out := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", fmt.Sprintf("10.3.%d.%d", i/250, i%250+1))
		if out["short_url"] == base {
			same++
		}
	}

	grown := utils.CountShortURLs() - before
	t.Logf("2000 次请求后：新增短链 %d 条，其中 %d 次复用了同一条", grown, same)

	// 去重后的不变量：无论该文件之前是否已有短链，2000 次请求最多只新增 1 条，
	// 且每一次都应复用同一条（same == rounds）。这证明外部无法无限注入。
	if grown > 1 {
		t.Errorf("✗ 同一文件 2000 次请求竟新增了 %d 条短链（漏洞未修复）", grown)
	}
	if same != rounds {
		t.Errorf("✗ 有 %d 次请求没有复用既有短链", rounds-same)
	}
}

// 3. 不同文件各自一条，去重不能误伤正常分享
func TestShortURLDifferentFilesGetDifferentLinks(t *testing.T) {
    utils.ResetShortURLStore()

	setupDownloadDir(t)
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5000, RateWindow: 600000000000})

	before := utils.CountShortURLs()
	seen := map[string]bool{}

	const n = 50
	for i := 0; i < n; i++ {
		rel := makeFile(t, fmt.Sprintf("share-%d.txt", i))
		_, out := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", "10.4.4.4")
		code, _ := out["short_url"].(string)
		if code == "" {
			t.Fatalf("第 %d 个文件生成失败: %v", i, out)
		}
		if seen[code] {
			t.Errorf("✗ 不同文件复用了同一条短链: %s", code)
		}
		seen[code] = true
	}

	if grew := utils.CountShortURLs() - before; grew != n {
		t.Errorf("✗ %d 个不同文件应新增 %d 条短链，实际 %d 条", n, n, grew)
	} else {
		t.Logf("✓ %d 个不同文件各得 1 条短链，共新增 %d 条", n, grew)
	}
}

// 4. 非法路径一律拒绝，且一条都不落库
func TestShortURLArbitraryPathRejected(t *testing.T) {
    utils.ResetShortURLStore()

	setupDownloadDir(t)
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5000, RateWindow: 600000000000})

	cases := []string{
		"../../../../Windows/System32/config/SAM",
		"/etc/passwd",
		`..\..\config\config.json`,
		"../config.json",
		"nonexistent-file-xyz.iso",
		"a.txt&path=../../config.json",
	}
	before := utils.CountShortURLs()
	for _, p := range cases {
		w, out := postShare(t, "path="+url.QueryEscape(p)+"&filename=x", "10.5.5.5")
		t.Logf("path=%-45s -> HTTP %d msg=%v", p, w.Code, out["error"])
		if w.Code == http.StatusOK {
			t.Errorf("✗ 非法路径被接受: %s", p)
		}
	}
	if grew := utils.CountShortURLs() - before; grew != 0 {
		t.Errorf("✗ 非法路径污染了存储，新增 %d 条", grew)
	}
}

// 5. IP 限流兜底
func TestShortURLRateLimitBackstop(t *testing.T) {
    utils.ResetShortURLStore()

	setupDownloadDir(t)
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5, RateWindow: 600000000000})

	limited := 0
	for i := 0; i < 30; i++ {
		// 每轮换一个新文件，绕开去重，专测限流
		rel := makeFile(t, fmt.Sprintf("rl-%d.txt", i))
		code, _ := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", "10.6.6.6")
		if code.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	t.Logf("限流窗口内 30 次请求，拦截 %d 次", limited)
	if limited == 0 {
		t.Errorf("✗ IP 限流未生效")
	}
}

// 6. 重定向必须转义，避免查询参数注入
func TestShortURLRedirectEscaping(t *testing.T) {
    utils.ResetShortURLStore()

	rel := setupDownloadDir(t) // 文件名含 & 与 #
	utils.SetShortURLPolicy(utils.ShortURLPolicy{MaxTotal: 100000, RateLimit: 5000, RateWindow: 600000000000})

	_, out := postShare(t, "path="+url.QueryEscape(rel)+"&filename=x", "10.7.7.7")
	code, _ := out["short_url"].(string)
	if code == "" {
		t.Fatalf("生成失败: %v", out)
	}

	w := httptest.NewRecorder()
	// code 形如 /s/QbBDgJ，本身就是合法的绝对路径请求目标，直接作为请求 URL
	ShortURLHandler(w, httptest.NewRequest("GET", code, nil))

	loc := w.Header().Get("Location")
	t.Logf("重定向目标 = %s", loc)
	if strings.Contains(loc, "&path=") {
		t.Errorf("✗ 重定向 URL 未转义，存在查询参数注入: %s", loc)
	}
	if !strings.Contains(loc, "%26") {
		t.Errorf("✗ 文件名中的 & 未被转义: %s", loc)
	}
}

// 7. 非法短链码不应命中
func TestShortURLInvalidCode(t *testing.T) {
    utils.ResetShortURLStore()

	for _, c := range []string{"", "abc", "ab/cd", "../..", strings.Repeat("a", 100)} {
		w := httptest.NewRecorder()
		ShortURLHandler(w, httptest.NewRequest("GET", "/s/"+c, nil))
		if w.Code == http.StatusFound {
			t.Errorf("✗ 非法短链码 %q 触发了重定向: %s", c, w.Header().Get("Location"))
		}
	}
	t.Logf("非法短链码均被拦截")
}
