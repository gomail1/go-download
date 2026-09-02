package ed2k

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-download-server/internal/core"
	"go-download-server/internal/logger"
)

// defaultED2KServers 是内置的公开 eD2K 服务器列表（可能离线）。
// 可通过环境变量 ED2K_SERVERS（逗号分隔的 host:port）覆盖，例如：
//
//	ED2K_SERVERS="server1.example.com:4661,server2.example.com:4661" ./go-download-server
var defaultED2KServers = []string{
	"78.46.114.246:4661",
	"91.220.209.182:4661",
	"193.120.144.253:4661",
}

// ED2KProtocol 是 core.Protocol 接口的 eD2K 实现（真实 eDonkey2000 客户端）。
type ED2KProtocol struct {
	isRunning    bool
	isPaused     bool
	status       core.Status
	statistics   core.Statistics
	config       core.ProtocolConfig
	resourceCtrl *core.ResourceController
	connPool     *core.ConnectionPool

	clientHash string
	clientID   uint32
	tcpPort    uint16
	nick       string
	mu         sync.Mutex
}

// NewED2KProtocol 创建 eD2K 协议实例。
func NewED2KProtocol() *ED2KProtocol {
	return &ED2KProtocol{
		status: core.Status{
			IsRunning: false,
			IsPaused:  false,
		},
		statistics: core.Statistics{
			StartTime: time.Now(),
		},
		clientHash: randomClientHash(),
		tcpPort:    0, // 我们作为纯下载端，不监听入站
		nick:       "QuadFetch",
	}
}

// randomClientHash 生成 16 字节随机 userHash（第 6、15 字节按协议约定固定）。
func randomClientHash() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[5] = 14
	b[14] = 111
	return hex.EncodeToString(b)
}

// CanHandle 判断 URL 是否为 ed2k 链接。
func (e *ED2KProtocol) CanHandle(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "ed2k://")
}

// GetMetadata 解析 ed2k 链接的元数据。
func (e *ED2KProtocol) GetMetadata(ctx context.Context, url string) (*core.Metadata, error) {
	res, err := ParseED2KLink(url)
	if err != nil {
		return nil, err
	}
	return &core.Metadata{
		Filename: res.Name,
		Size:     res.Size,
		MimeType: "application/octet-stream",
		Headers:  make(map[string]string),
		ProtocolSpecific: map[string]interface{}{
			"hash":    res.Hash,
			"aich":    res.AICH,
			"sources": res.Sources,
			"url":     url,
		},
	}, nil
}

// Download 执行真实的 eD2K 下载流程。
func (e *ED2KProtocol) Download(ctx context.Context, task *core.Task, progress chan<- core.Progress) error {
	e.mu.Lock()
	e.isRunning = true
	e.isPaused = false
	e.status.IsRunning = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.isRunning = false
		e.status.IsRunning = false
		e.mu.Unlock()
	}()

	if task.Metadata == nil {
		return fmt.Errorf("缺少元数据，无法下载")
	}
	meta := task.Metadata
	fileHash, _ := meta.ProtocolSpecific["hash"].(string)
	if fileHash == "" {
		return fmt.Errorf("ed2k 链接缺少文件哈希")
	}
	size := meta.Size
	if size <= 0 {
		return fmt.Errorf("ed2k 链接缺少文件大小")
	}
	name := meta.Filename
	if name == "" {
		name = fileHash
	}

	saveDir := task.Config.SavePath
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("创建保存目录失败: %w", err)
	}
	finalPath := filepath.Join(saveDir, name)
	partPath := finalPath + ".part"
	bitmapPath := partPath + ".parts"

	// 加载已完成分块位图
	bitmap, err := loadBitmap(bitmapPath, size)
	if err != nil {
		logger.Warnf("ed2k: 位图加载失败（将从头下载）: %v", err)
		bitmap = newBitmap(size)
	}
	bitmap.path = bitmapPath

	// 若最终文件已完整则直接完成（无需下载）
	if fileComplete(finalPath, size) {
		logger.Infof("ed2k: 文件已存在且大小匹配，跳过下载: %s", finalPath)
		sendProgress(progress, 100, size, size, 0, 0, "已完成", 0, 0)
		return nil
	}

	totalParts := int((size + partSize - 1) / partSize)
	incomplete := bitmap.incompleteParts(totalParts)

	if len(incomplete) == 0 {
		// 位图显示全部完成，但 .part 可能不完整，做一次完整性确认
		if fileComplete(partPath, size) {
			return e.finalizeAndVerify(finalPath, partPath, bitmapPath, fileHash, size, task, progress)
		}
		incomplete = allParts(totalParts)
	}

	e.statistics.StartTime = time.Now()

	// 收集来源：优先使用链接内嵌来源，其次向服务器查询
	sources := e.collectSources(ctx, fileHash, meta)
	if len(sources) == 0 {
		sendProgress(progress, 0, 0, size, 0, 0, "未发现可用来源，请检查 eD2K 服务器或链接内嵌来源", 0, 0)
		return fmt.Errorf("未发现任何可用 eD2K 来源（hash=%s），请配置可连接的服务器或在链接中附带 |sources,host:port|", fileHash)
	}
	logger.Infof("ed2k: 共收集到 %d 个来源", len(sources))

	// 逐个来源尝试下载
	startTime := time.Now()
	var lastProgress time.Time
	for len(incomplete) > 0 && len(sources) > 0 {
		src := sources[0]
		sources = sources[1:]

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		peer, err := dialPeer(ctx, src, e.clientHash, e.clientID, e.tcpPort, e.nick)
		if err != nil {
			logger.Debugf("ed2k: 连接来源 %s 失败: %v", idToIP(src.id), err)
			continue
		}
		logger.Infof("ed2k: 已连接来源 %s，剩余 %d 个分块", idToIP(src.id), len(incomplete))

		downloaded, done := e.downloadFromPeer(ctx, peer, fileHash, size, bitmap, &incomplete, partPath, progress, startTime, totalParts, &lastProgress)
		peer.close()

		if done {
			break
		}
		_ = downloaded
	}

	if len(incomplete) > 0 {
		sendProgress(progress, 0, 0, size, 0, 0, "来源不足，下载未完成", 0, 0)
		return fmt.Errorf("eD2K 下载未完成：仍有 %d 个分块未获取（来源不足或网络不可达）", len(incomplete))
	}

	// 全部完成：校验并重命名
	return e.finalizeAndVerify(finalPath, partPath, bitmapPath, fileHash, size, task, progress)
}

// collectSources 收集可用来源：链接内嵌 + 服务器查询。
func (e *ED2KProtocol) collectSources(ctx context.Context, fileHash string, meta *core.Metadata) []peerSource {
	var sources []peerSource

	// 1) 链接内嵌来源（最可靠）
	if raw, ok := meta.ProtocolSpecific["sources"].([]string); ok {
		for _, s := range raw {
			if ps, ok := parseHostPortSource(s); ok {
				sources = append(sources, ps)
			}
		}
	}

	// 2) 向服务器查询（尽力而为，失败不致命）
	serverList := serverListFromEnv()
	for _, srv := range serverList {
		host, portStr, err := splitHostPort(srv)
		if err != nil {
			continue
		}
		port, _ := strconv.Atoi(portStr)
		srvConn, err := dialServer(host, port, 15*time.Second, e.clientHash, e.tcpPort, e.nick)
		if err != nil {
			logger.Debugf("ed2k: 连接服务器 %s 失败: %v", srv, err)
			continue
		}
		e.clientID = srvConn.clientID
		srcs, qerr := srvConn.querySources(ctx, fileHash, 12*time.Second)
		srvConn.close()
		if qerr != nil {
			logger.Debugf("ed2k: 服务器 %s 查询来源失败: %v", srv, qerr)
			continue
		}
		sources = append(sources, srcs...)
		if len(sources) > 0 {
			break // 找到一个可用服务器即可
		}
	}

	// 去重
	seen := map[uint32]bool{}
	var unique []peerSource
	for _, s := range sources {
		if seen[s.id] {
			continue
		}
		seen[s.id] = true
		unique = append(unique, s)
	}
	return unique
}

// downloadFromPeer 从一个对等端下载缺失的分块，返回已下载字节与是否全部完成。
func (e *ED2KProtocol) downloadFromPeer(ctx context.Context, peer *ed2kPeer, fileHash string, size int64, bitmap *bitmapT, incomplete *[]int, partPath string, progress chan<- core.Progress, startTime time.Time, totalParts int, lastProgress *time.Time) (int64, bool) {
	file, err := os.OpenFile(partPath, os.O_RDWR, 0644)
	if err != nil {
		logger.Errorf("ed2k: 打开临时文件失败: %v", err)
		return 0, false
	}
	defer file.Close()

	_ = peer.sendFileRequest(fileHash)

	idleRounds := 0
	for {
		select {
		case <-ctx.Done():
			return 0, false
		default:
		}

		if len(*incomplete) == 0 {
			return size, true
		}

		// 发送当前缺失分块请求
		gaps := gapsForParts(*incomplete, size)
		_ = peer.sendRequestParts(fileHash, gaps)

		// 读取报文（带超时）
		peer.setReadDeadline(time.Now().Add(8 * time.Second))
		pkt, err := peer.readPacket()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				idleRounds++
				if idleRounds > 6 {
					logger.Debugf("ed2k: 来源 %s 长时间无数据，切换", idToIP(peer.id))
					return 0, false
				}
				continue
			}
			logger.Debugf("ed2k: 来源 %s 连接断开: %v", idToIP(peer.id), err)
			return 0, false
		}
		idleRounds = 0

		switch pkt.opcode {
		case opPartData:
			offset, data, perr := parsePartData(pkt.payload)
			if perr != nil {
				logger.Debugf("ed2k: 解析 PartData 失败: %v", perr)
				continue
			}
			idx := offsetToPartIndex(offset)
			partLen := partLenAt(idx, size)
			if int64(len(data)) != partLen {
				logger.Warnf("ed2k: 分块 %d 数据长度不符（期望 %d，实际 %d），跳过该来源", idx, partLen, len(data))
				return 0, false
			}
			if _, werr := file.WriteAt(data, offset); werr != nil {
				logger.Errorf("ed2k: 写入分块 %d 失败: %v", idx, werr)
				return 0, false
			}
			bitmap.set(idx)
			_ = bitmap.save()
			*incomplete = removePart(*incomplete, idx)

			downloaded := size - remainingSize(*incomplete, size)
			emitProgress(progress, lastProgress, downloaded, size, startTime, len(*incomplete), totalParts)

		case opFileReqAnswer:
			if len(pkt.payload) > 0 && pkt.payload[0] == 0x01 {
				logger.Debugf("ed2k: 来源 %s 没有该文件", idToIP(peer.id))
				return 0, false
			}
		case opQueueRank:
			// 被排队，稍后重试
			time.Sleep(2 * time.Second)
		case opReject:
			logger.Debugf("ed2k: 来源 %s 拒绝", idToIP(peer.id))
			return 0, false
		default:
			logger.Debugf("ed2k: 忽略对端 opcode=0x%02X", pkt.opcode)
		}
	}
}

// finalizeAndVerify 校验哈希（可选）并把 .part 重命名为最终文件。
func (e *ED2KProtocol) finalizeAndVerify(finalPath, partPath, bitmapPath, fileHash string, size int64, task *core.Task, progress chan<- core.Progress) error {
	if task.Config.VerifyHash {
		logger.Infof("ed2k: 校验文件哈希 hash=%s ...", fileHash)
		f, err := os.Open(partPath)
		if err == nil {
			got, herr := ComputeED2KHash(f)
			f.Close()
			if herr == nil && got != fileHash {
				return fmt.Errorf("eD2K 哈希校验失败：期望 %s，实际 %s", fileHash, got)
			}
			if herr == nil {
				logger.Infof("ed2k: 哈希校验通过 %s", got)
			}
		}
	}
	return e.finalize(finalPath, bitmapPath)
}

// finalize 将 .part 重命名为最终文件并清理位图。
func (e *ED2KProtocol) finalize(finalPath, bitmapPath string) error {
	// 把 .part 移到 finalPath（同目录相当于覆盖）
	if err := os.Rename(finalPath+".part", finalPath); err != nil {
		return fmt.Errorf("重命名下载文件失败: %w", err)
	}
	_ = os.Remove(bitmapPath)
	sendProgress(nil, 100, 0, 0, 0, 0, "下载完成", 0, 0)
	return nil
}

// ----- 进度上报辅助 -----

func emitProgress(progress chan<- core.Progress, last *time.Time, downloaded, total int64, start time.Time, incomplete, totalParts int) {
	percentage := 0.0
	if total > 0 {
		percentage = float64(downloaded) / float64(total) * 100
	}
	elapsed := time.Since(start).Seconds()
	if elapsed < 0.1 {
		elapsed = 0.1
	}
	speed := int64(float64(downloaded) / elapsed)
	var eta time.Duration
	if speed > 0 && total > downloaded {
		eta = time.Duration(float64(total-downloaded)/float64(speed)) * time.Second
	}
	now := time.Now()
	if last != nil && now.Sub(*last) < 500*time.Millisecond {
		return
	}
	if last != nil {
		*last = now
	}
	sendProgress(progress, percentage, downloaded, total, speed, eta, "下载中", totalParts-incomplete, totalParts)
}

func sendProgress(progress chan<- core.Progress, percentage float64, downloaded, total, speed int64, eta time.Duration, status string, active, totalPeers int) {
	if progress == nil {
		return
	}
	p := core.Progress{
		Percentage:   percentage,
		Downloaded:   downloaded,
		TotalSize:    total,
		Speed:        speed,
		ETA:          eta,
		CurrentChunk: 1,
		TotalChunks:  1,
		Status:       status,
		ActivePeers:  active,
		TotalPeers:   totalPeers,
	}
	select {
	case progress <- p:
	default:
	}
}

// ----- 分块位图（断点续传） -----

type bitmapT struct {
	parts []byte // 每个分块 1 字节：1=完成 0=未完成
	size  int64
	path  string
	mu    sync.Mutex
}

func newBitmap(size int64) *bitmapT {
	n := int((size + partSize - 1) / partSize)
	return &bitmapT{parts: make([]byte, n), size: size}
}

func loadBitmap(path string, size int64) (*bitmapT, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &bitmapT{parts: data, size: size, path: path}, nil
}

func (b *bitmapT) save() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.path == "" {
		return nil
	}
	return os.WriteFile(b.path, b.parts, 0644)
}

func (b *bitmapT) set(idx int) {
	b.mu.Lock()
	if idx >= 0 && idx < len(b.parts) {
		b.parts[idx] = 1
	}
	b.mu.Unlock()
}

func (b *bitmapT) incompleteParts(total int) []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []int
	for i := 0; i < total && i < len(b.parts); i++ {
		if b.parts[i] == 0 {
			out = append(out, i)
		}
	}
	return out
}

// ----- 各类小工具 -----

func partLenAt(idx int, size int64) int64 {
	start := partIndexToOffset(idx)
	if start+partSize > size {
		return size - start
	}
	return partSize
}

func gapsForParts(parts []int, size int64) []gap {
	gaps := make([]gap, 0, len(parts))
	for _, idx := range parts {
		start := partIndexToOffset(idx)
		gaps = append(gaps, gap{start: start, end: start + partLenAt(idx, size)})
	}
	return gaps
}

func remainingSize(incomplete []int, size int64) int64 {
	var s int64
	for _, idx := range incomplete {
		s += partLenAt(idx, size)
	}
	return s
}

func allParts(total int) []int {
	out := make([]int, total)
	for i := range out {
		out[i] = i
	}
	return out
}

func removePart(parts []int, idx int) []int {
	out := parts[:0]
	for _, p := range parts {
		if p != idx {
			out = append(out, p)
		}
	}
	return out
}

func fileComplete(path string, size int64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() == size
}

func parseHostPortSource(s string) (peerSource, bool) {
	host, portStr, err := splitHostPort(s)
	if err != nil {
		return peerSource{}, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return peerSource{}, false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return peerSource{}, false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return peerSource{}, false
	}
	id := uint32(ip4[0]) | uint32(ip4[1])<<8 | uint32(ip4[2])<<16 | uint32(ip4[3])<<24
	return peerSource{id: id, port: uint16(port)}, true
}

func splitHostPort(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty")
	}
	// 支持 host:port 或 [host]:port
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i], s[i+1:], nil
	}
	return "", "", fmt.Errorf("无端口")
}

func serverListFromEnv() []string {
	if v := os.Getenv("ED2K_SERVERS"); v != "" {
		parts := strings.Split(v, ",")
		var out []string
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				out = append(out, strings.TrimSpace(p))
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return defaultED2KServers
}

// ----- core.Protocol 的其余方法 -----

func (e *ED2KProtocol) Pause() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isRunning {
		return fmt.Errorf("not running")
	}
	e.isPaused = true
	e.status.IsPaused = true
	return nil
}

func (e *ED2KProtocol) Resume() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.isRunning {
		return fmt.Errorf("not running")
	}
	e.isPaused = false
	e.status.IsPaused = false
	return nil
}

func (e *ED2KProtocol) Cancel() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isRunning = false
	e.status.IsRunning = false
	return nil
}

func (e *ED2KProtocol) GetStatus() core.Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *ED2KProtocol) GetStatistics() core.Statistics {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.statistics
}

func (e *ED2KProtocol) ApplyConfig(config core.ProtocolConfig) error {
	e.config = config
	return nil
}

func (e *ED2KProtocol) GetCapabilities() core.Capabilities {
	return core.Capabilities{
		CanResume:           true,
		CanVerify:           true,
		SupportsChunks:      true,
		SupportsP2P:         true,
		SupportedURLSchemes: []string{"ed2k"},
	}
}

func (e *ED2KProtocol) SetResourceController(rc *core.ResourceController) { e.resourceCtrl = rc }
func (e *ED2KProtocol) SetConnectionPool(pool *core.ConnectionPool)      { e.connPool = pool }
