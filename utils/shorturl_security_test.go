package utils

// 短链存储层安全用例（白盒，可直接操作内部状态）
// 运行：go test ./utils/ -run TestShortURL -v

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-download-server/config"
)

// useTempDownloadDir 将下载目录指向临时目录，避免依赖真实 config 与工作目录
func useTempDownloadDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config.AppConfig.Server.DownloadDir = dir
	return dir
}

// 历史脏数据（v1.1.0 时期被越权注入的非法路径）在解析时必须失效
func TestShortURLPoisonedEntryRejected(t *testing.T) {
	poisoned := map[string]string{
		"ABCDef": "../../config/config.json",
		"GHIJkl": "/etc/passwd",
		"MNOPqr": `..\..\..\Windows\win.ini`,
	}
	for code, p := range poisoned {
		shortURLStore.ShortURLs[code] = ShortURL{
			ID: code, OriginalPath: p, Filename: filepath.Base(p),
			Creator: "attacker", CreatorIP: "1.2.3.4",
		}
	}

	for code := range poisoned {
		if _, _, ok := GetOriginalPath(code); ok {
			t.Errorf("✗ 非法路径短链 %s 仍可被解析", code)
		}
	}
	for code := range poisoned {
		delete(shortURLStore.ShortURLs, code)
	}
	t.Logf("✓ %d 条脏数据短链已在解析阶段被拒绝", len(poisoned))
}

// short_urls 为 null 时 map 为 nil，旧实现写入会 panic
func TestShortURLNilMapNotPanic(t *testing.T) {
	backup := shortURLStore
	defer func() { shortURLStore = backup }()

	shortURLStore = &ShortURLStore{} // ShortURLs == nil

	dir := useTempDownloadDir(t)
	name := "nilmap-test.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("✗ 写入 nil map 触发 panic: %v", r)
		}
	}()
	code, err := GenerateShortURL(name, "127.0.0.1")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	t.Logf("✓ nil map 场景未 panic，生成短链 %s", code)
}

// 按目标文件去重：同一路径重复调用必须复用同一条短链（防无限注入的核心）
func TestShortURLDedupByPath(t *testing.T) {
	backup := shortURLStore
	defer func() { shortURLStore = backup }()

	dir := useTempDownloadDir(t)
	shortURLStore = &ShortURLStore{ShortURLs: make(map[string]ShortURL)}
	SetShortURLPolicy(ShortURLPolicy{MaxTotal: 1000, RateLimit: 5000, RateWindow: 600000000000})

	name := "dedup.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	first, err := GenerateShortURL(name, "1.1.1.1")
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}

	// 不断变换 IP 反复请求，模拟攻击者
	for i := 0; i < 500; i++ {
		got, err := GenerateShortURL(name, fmtIP(i))
		if err != nil {
			t.Fatalf("第 %d 次复用失败: %v", i, err)
		}
		if got != first {
			t.Fatalf("✗ 同一文件返回了不同短链: %s != %s", got, first)
		}
	}

	if n := len(shortURLStore.ShortURLs); n != 1 {
		t.Errorf("✗ 同一文件应只有 1 条短链，实际 %d 条", n)
	} else {
		t.Logf("✓ 500 次复用后短链总数仍为 1")
	}
}

// 全局总量上限兜底
func TestShortURLGlobalCap(t *testing.T) {
	backup := shortURLStore
	defer func() { shortURLStore = backup }()

	dir := useTempDownloadDir(t)
	shortURLStore = &ShortURLStore{ShortURLs: make(map[string]ShortURL)}
	SetShortURLPolicy(ShortURLPolicy{MaxTotal: 5, RateLimit: 5000, RateWindow: 600000000000})

	created := 0
	for i := 0; i < 10; i++ {
		name := filepath.Join("cap", fmt.Sprintf("f%d.txt", i))
		os.MkdirAll(filepath.Join(dir, "cap"), 0755)
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)

		_, err := GenerateShortURL(name, fmtIP(i))
		if err == nil {
			created++
			continue
		}
		if errors.Is(err, ErrShortURLQuotaExceeded) {
			t.Logf("第 %d 个文件被总量上限拦截: %v", created+1, err)
			break
		}
		t.Fatalf("意外错误: %v", err)
	}
	if created != 5 {
		t.Errorf("✗ 全局上限应为 5，实际放行 %d 条", created)
	} else {
		t.Logf("✓ 全局上限生效，放行 %d 条后拦截", created)
	}
}

func fmtIP(i int) string {
	return fmt.Sprintf("10.%d.%d.%d", i/65536, i/256%256, i%256)
}

// 回归测试：曾经按 limit 预分配 make([]time.Time,0,limit)，
// 当 limit 是配置项且取值很大时，每次请求分配 limit*24 字节，
// 高频调用会瞬间吃光内存把进程/机器拖死。
// 修复后只按「实际记录数 +1」预分配且封顶 rateLimiterMaxRecords，
// 因此即便传入超大 limit，单 key 记录数也恒定有界。
func TestRateLimiterNoPrealloc(t *testing.T) {
	rl := newRateLimiter()
	const hugeLimit = 1000000 // 模拟被配置成不合理的超大值

	// 模拟窗口内高频调用：旧实现这里会累计分配 ~24MB/次
	for i := 0; i < 5000; i++ {
		if !rl.allow("attacker", hugeLimit, time.Hour) {
			t.Fatalf("第 %d 次调用被误拦截（窗口内不应满）", i)
		}
	}

	rl.Lock()
	n := len(rl.records["attacker"])
	rl.Unlock()

	if n > rateLimiterMaxRecords {
		t.Errorf("✗ 单 key 记录数 %d 超出上限 %d，存在内存膨胀风险", n, rateLimiterMaxRecords)
	} else {
		t.Logf("✓ 即便 limit=%d，单 key 记录数仍被钉死在 %d（内存有界）", hugeLimit, n)
	}
}


// 撤销权限：非管理员只能删除自己的短链
func TestShortURLDeletePermission(t *testing.T) {
	backup := shortURLStore
	defer func() { shortURLStore = backup }()

	shortURLStore = &ShortURLStore{ShortURLs: map[string]ShortURL{
		"aaaaaa": {ID: "aaaaaa", OriginalPath: "x.txt", Creator: "alice"},
	}}

	if err := DeleteShortURL("aaaaaa", "bob", false); !errors.Is(err, ErrShortURLForbidden) {
		t.Errorf("✗ 普通用户删他人短链未被拦截: %v", err)
	}
	if err := DeleteShortURL("aaaaaa", "bob", true); err != nil {
		t.Errorf("✗ 管理员删他人短链失败: %v", err)
	}
	if err := DeleteShortURL("aaaaaa", "bob", true); !errors.Is(err, ErrShortURLNotFound) {
		t.Errorf("✗ 删除不存在的短链未返回 NotFound: %v", err)
	}
	t.Logf("✓ 撤销权限判定正确")
}
