package utils

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-download-server/config"
)

// 短链安全策略默认值（可用 SetShortURLPolicy 覆盖）
const (
	DefaultShortURLMaxTotal   = 5000        // 全局短链总量上限（兜底，正常应等于文件数）
	DefaultShortURLRateLimit  = 120         // 单 IP 限流窗口内允许生成次数（兜底）
	DefaultShortURLRateWindow = time.Minute // 限流窗口
	MaxShortURLPathLen        = 512         // 短链路径最大长度

	ShortCodeLength      = 6
	shortCodeMaxAttempts = 12
	shortCodeCharset     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// 短链相关错误（调用方可用 errors.Is 判定）
var (
	ErrShortURLInvalidPath   = errors.New("短链路径不合法")
	ErrShortURLQuotaExceeded = errors.New("短链数量已达上限")
	ErrShortURLRateLimited   = errors.New("短链生成过于频繁，请稍后再试")
	ErrShortURLCodeExhausted = errors.New("短链码空间已耗尽")
	ErrShortURLNotFound      = errors.New("短链不存在")
	ErrShortURLForbidden     = errors.New("无权操作该短链")
)

// ShortURLPolicy 短链安全策略。
// 主要防线是「按目标文件去重」，MaxTotal / RateLimit 只是兜底。
type ShortURLPolicy struct {
	MaxTotal   int           // 全局总量上限（兜底）
	RateLimit  int           // 单 IP 限流窗口内允许次数（兜底）
	RateWindow time.Duration // 限流窗口
}

// anonymousCreator 分享短链无需登录，创建者统一记为 anonymous
const anonymousCreator = "anonymous"

var (
	shortURLPolicy     = defaultShortURLPolicy()
	shortURLPolicyLock sync.RWMutex
)

func defaultShortURLPolicy() ShortURLPolicy {
	return ShortURLPolicy{
		MaxTotal:   DefaultShortURLMaxTotal,
		RateLimit:  DefaultShortURLRateLimit,
		RateWindow: DefaultShortURLRateWindow,
	}
}

// SetShortURLPolicy 覆盖短链安全策略（0 值回退为默认值）
func SetShortURLPolicy(p ShortURLPolicy) {
	def := defaultShortURLPolicy()
	if p.MaxTotal <= 0 {
		p.MaxTotal = def.MaxTotal
	}
	if p.RateLimit <= 0 {
		p.RateLimit = def.RateLimit
	}
	if p.RateWindow <= 0 {
		p.RateWindow = def.RateWindow
	}
	shortURLPolicyLock.Lock()
	shortURLPolicy = p
	shortURLPolicyLock.Unlock()
}

// GetShortURLPolicy 获取当前短链安全策略
func GetShortURLPolicy() ShortURLPolicy {
	shortURLPolicyLock.RLock()
	defer shortURLPolicyLock.RUnlock()
	return shortURLPolicy
}

// ShortURL 短链结构
type ShortURL struct {
	ID           string    `json:"id"`
	OriginalPath string    `json:"original_path"`
	Filename     string    `json:"filename"`
	Creator      string    `json:"creator"`    // 创建者，用于归属判定与配额统计
	CreatorIP    string    `json:"creator_ip"` // 创建者 IP，用于审计
	CreatedAt    time.Time `json:"created_at"`
}

// ShortURLStore 短链存储
type ShortURLStore struct {
	ShortURLs map[string]ShortURL `json:"short_urls"`
	Mutex     sync.RWMutex        `json:"-"`
}

// 全局短链存储实例
var (
	shortURLStore *ShortURLStore
	storeMutex    sync.Mutex
)

// 短链生成限流器（仅内存，不持久化）
var shortURLRateLimiter = newRateLimiter()

type rateLimiter struct {
	sync.Mutex
	records map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{records: make(map[string][]time.Time)}
}

// rateLimiterMaxRecords 单个 key 最多保留的记录数。
// 达到 limit 后本函数就会拒绝，因此保留更多记录没有意义，
// 这个值同时把单次请求的内存开销钉死在上限内。
const rateLimiterMaxRecords = 1024

// rateLimiterMaxKeys 限流器最多跟踪的 key（IP）数量，超出后清理过期条目
const rateLimiterMaxKeys = 10000

// allow 判断 key 在窗口内是否还有配额，通过则记一笔。
//
// 注意：这里绝不能按 limit 预分配（make(..., 0, limit)）。
// limit 是配置项，取值可能很大；那样每次请求都会分配 limit*sizeof(time.Time)
// 的内存，高频调用时足以打爆内存。预分配只按「实际记录数」且设上限。
func (rl *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-window)

	rl.Lock()
	defer rl.Unlock()

	old := rl.records[key]

	// 预分配上限：实际记录数 +1，且不超过 rateLimiterMaxRecords
	capHint := len(old) + 1
	if capHint > rateLimiterMaxRecords {
		capHint = rateLimiterMaxRecords
	}
	kept := make([]time.Time, 0, capHint)

	for _, t := range old {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= limit {
		rl.records[key] = kept
		return false
	}

	// 记录数已达上限时丢弃最旧的一笔，防止单 key 无界增长
	if len(kept) >= rateLimiterMaxRecords {
		kept = append(kept[1:], now)
	} else {
		kept = append(kept, now)
	}
	rl.records[key] = kept

	// 防止 key 数量无限增长（大量不同 IP 探测时）
	if len(rl.records) > rateLimiterMaxKeys {
		for k, v := range rl.records {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(rl.records, k)
			}
		}
	}
	return true
}

// 初始化短链存储
func init() {
	loadShortURLStore()
}

// loadShortURLStore 加载短链存储
func loadShortURLStore() {
	storeMutex.Lock()
	defer storeMutex.Unlock()

	store := ShortURLStore{ShortURLs: make(map[string]ShortURL)}

	storeFile := shortURLStorePath()
	if err := os.MkdirAll(filepath.Dir(storeFile), 0755); err != nil {
		log.Printf("创建配置目录失败: %v", err)
		shortURLStore = &store
		return
	}

	data, err := os.ReadFile(storeFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取短链存储文件失败: %v", err)
		}
		shortURLStore = &store
		saveShortURLStoreLocked()
		return
	}

	if err := json.Unmarshal(data, &store); err != nil {
		log.Printf("解析短链存储文件失败: %v", err)
		store = ShortURLStore{ShortURLs: make(map[string]ShortURL)}
	}

	// 关键：JSON 为 {} 或 short_urls 为 null 时 map 会是 nil，
	// 直接写入会 panic（assignment to entry in nil map）
	if store.ShortURLs == nil {
		store.ShortURLs = make(map[string]ShortURL)
	}

	shortURLStore = &store
}

func shortURLStorePath() string {
	return filepath.Join("config", "shorturls.json")
}

// saveShortURLStore 保存短链存储
func saveShortURLStore() {
	storeMutex.Lock()
	defer storeMutex.Unlock()
	saveShortURLStoreLocked()
}

// saveShortURLStoreLocked 保存短链存储，调用方需持有 storeMutex
func saveShortURLStoreLocked() {
	storeFile := shortURLStorePath()

	if err := os.MkdirAll(filepath.Dir(storeFile), 0755); err != nil {
		log.Printf("创建配置目录失败: %v", err)
		return
	}

	data, err := json.MarshalIndent(shortURLStore, "", "  ")
	if err != nil {
		log.Printf("序列化短链存储失败: %v", err)
		return
	}

	// 原子写入：先写临时文件再 rename，避免进程中断留下损坏的 shorturls.json
	tmpFile := storeFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		log.Printf("写入短链临时文件失败: %v", err)
		return
	}
	if err := os.Rename(tmpFile, storeFile); err != nil {
		log.Printf("替换短链存储文件失败: %v", err)
		os.Remove(tmpFile)
	}
}

// generateShortCode 使用 crypto/rand 生成无模偏差的随机短码
func generateShortCode(length int) (string, error) {
	max := big.NewInt(int64(len(shortCodeCharset)))
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("生成随机数失败: %w", err)
		}
		code[i] = shortCodeCharset[n.Int64()]
	}
	return string(code), nil
}

// generateUniqueShortCode 生成未占用的短码，调用方需持有 shortURLStore 写锁
func generateUniqueShortCode() (string, error) {
	for i := 0; i < shortCodeMaxAttempts; i++ {
		code, err := generateShortCode(ShortCodeLength)
		if err != nil {
			return "", err
		}
		if _, exists := shortURLStore.ShortURLs[code]; !exists {
			return code, nil
		}
	}
	return "", ErrShortURLCodeExhausted
}

// ValidateShortURLPath 校验短链目标路径是否合法。
// 规则：非空、非绝对路径、无路径穿越、解析后仍位于下载目录内、且为已存在的普通文件。
// 返回规范化后的相对路径。
func ValidateShortURLPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: 路径为空", ErrShortURLInvalidPath)
	}
	if len(path) > MaxShortURLPathLen {
		return "", fmt.Errorf("%w: 路径长度超过 %d", ErrShortURLInvalidPath, MaxShortURLPathLen)
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("%w: 路径含空字符", ErrShortURLInvalidPath)
	}
	// 拒绝绝对路径（含 /etc/passwd、C:\...、\\server\share 等形态）
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") || filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: 不允许绝对路径", ErrShortURLInvalidPath)
	}

	cleaned := filepath.Clean(path)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 不允许路径穿越", ErrShortURLInvalidPath)
	}

	// 解析为绝对路径后再次确认没有越出下载目录（防御符号链接等边界情况）
	rootAbs, err := filepath.Abs(filepath.Clean(config.AppConfig.Server.DownloadDir))
	if err != nil {
		return "", fmt.Errorf("%w: 下载目录解析失败", ErrShortURLInvalidPath)
	}
	fullAbs, err := filepath.Abs(filepath.Join(rootAbs, cleaned))
	if err != nil {
		return "", fmt.Errorf("%w: 路径解析失败", ErrShortURLInvalidPath)
	}
	rel, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 路径越出下载目录", ErrShortURLInvalidPath)
	}

	fi, err := os.Stat(fullAbs)
	if err != nil {
		return "", fmt.Errorf("%w: 文件不存在", ErrShortURLInvalidPath)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("%w: 不能指向目录", ErrShortURLInvalidPath)
	}

	return cleaned, nil
}

// GenerateShortURL 为指定文件生成短链。
//
// 分享是公开能力，**不要求登录**。防滥用不靠鉴权，而是靠「按目标文件去重」：
// 同一个文件永远只对应一条短链，已存在则直接复用，不新增记录。
// 因此短链总数上限天然等于「下载目录里的文件总数」——外部无法通过反复请求
// 向服务器无限注入短链，这正是原来那个漏洞的根因。
//
// creatorIP 仅用于审计与限流。
func GenerateShortURL(originalPath, creatorIP string) (string, error) {
	cleanPath, err := ValidateShortURLPath(originalPath)
	if err != nil {
		return "", err
	}

	policy := GetShortURLPolicy()

	shortURLStore.Mutex.Lock()
	defer shortURLStore.Mutex.Unlock()

	// 防御：map 为 nil 时直接写入会 panic（assignment to entry in nil map）
	if shortURLStore.ShortURLs == nil {
		shortURLStore.ShortURLs = make(map[string]ShortURL)
	}

	// 核心：按目标文件去重。已存在则返回既有短链，不写库、不限流。
	// 这是最热的路径（同一个文件被反复分享），必须尽量轻。
	for _, su := range shortURLStore.ShortURLs {
		if su.OriginalPath == cleanPath {
			return su.ID, nil
		}
	}

	// 只有真正要新增记录时才做限流与配额判定（第二、三道防线）
	if !shortURLRateLimiter.allow(creatorIP, policy.RateLimit, policy.RateWindow) {
		return "", ErrShortURLRateLimited
	}

	// 兜底：全局总量上限
	if len(shortURLStore.ShortURLs) >= policy.MaxTotal {
		return "", fmt.Errorf("%w: 全局上限 %d 条，请先清理历史短链", ErrShortURLQuotaExceeded, policy.MaxTotal)
	}

	code, err := generateUniqueShortCode()
	if err != nil {
		return "", err
	}

	shortURLStore.ShortURLs[code] = ShortURL{
		ID:           code,
		OriginalPath: cleanPath,
		Filename:     filepath.Base(cleanPath),
		Creator:      anonymousCreator,
		CreatorIP:    creatorIP,
		CreatedAt:    time.Now(),
	}

	saveShortURLStore()
	return code, nil
}

// GetOriginalPath 通过短链获取原始路径。
// 返回前会重新校验路径，历史数据中已被注入的非法条目会自动失效。
func GetOriginalPath(shortCode string) (string, string, bool) {
	shortURLStore.Mutex.RLock()
	su, exists := shortURLStore.ShortURLs[shortCode]
	shortURLStore.Mutex.RUnlock()

	if !exists {
		return "", "", false
	}

	// 防御：库中残留的非法路径一律视为失效
	if _, err := ValidateShortURLPath(su.OriginalPath); err != nil {
		log.Printf("短链 %s 指向非法路径已拒绝: %v", shortCode, err)
		return "", "", false
	}

	return su.OriginalPath, su.Filename, true
}

// DeleteShortURL 撤销短链。isAdmin 为 true 时可删除任意短链，否则仅限本人创建。
func DeleteShortURL(shortCode, operator string, isAdmin bool) error {
	shortURLStore.Mutex.Lock()
	defer shortURLStore.Mutex.Unlock()

	su, exists := shortURLStore.ShortURLs[shortCode]
	if !exists {
		return ErrShortURLNotFound
	}
	if !isAdmin && su.Creator != operator {
		return ErrShortURLForbidden
	}

	delete(shortURLStore.ShortURLs, shortCode)
	saveShortURLStore()
	return nil
}

// CountShortURLs 返回当前短链总数
func CountShortURLs() int {
	shortURLStore.Mutex.RLock()
	defer shortURLStore.Mutex.RUnlock()
	return len(shortURLStore.ShortURLs)
}

// ResetShortURLStore 清空全部短链（内存 + 磁盘）。
// 用途：管理端「清空全部短链」，以及测试隔离。
func ResetShortURLStore() {
	storeMutex.Lock()
	shortURLStore = &ShortURLStore{ShortURLs: make(map[string]ShortURL)}
	storeMutex.Unlock()
	saveShortURLStore()
}

// ListShortURLs 返回当前全部短链（供管理界面使用）
func ListShortURLs() []ShortURL {
	shortURLStore.Mutex.RLock()
	defer shortURLStore.Mutex.RUnlock()

	list := make([]ShortURL, 0, len(shortURLStore.ShortURLs))
	for _, su := range shortURLStore.ShortURLs {
		list = append(list, su)
	}
	return list
}
