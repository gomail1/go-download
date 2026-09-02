package ed2k

import (
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/md4" //nolint:gosec // ed2k 协议规定使用 MD4
)

// partSize 是 eD2K 协议中每个分块（part）的固定大小：9500 * 1024 字节。
// 最后一个分块可能更小。每个分块单独做 MD4，最终根哈希是对所有分块哈希拼接后再做 MD4。
const partSize = 9500 * 1024

// md4Sum 计算 MD4 摘要（golang.org/x/crypto/md4 不提供 Sum 函数，仅提供 New）。
func md4Sum(data []byte) []byte {
	h := md4.New() //nolint:gosec
	h.Write(data)
	return h.Sum(nil)
}

// ED2KHashResult 保存一条 ed2k 链接解析后的信息。
type ED2KHashResult struct {
	Name    string
	Size    int64
	Hash    string // 32 字符十六进制
	AICH    string // 可选，AICH 哈希（"h=..." 形式）
	Sources []string
}

// ComputeED2KHash 以流式方式计算文件的 eD2K 哈希：
//  1. 把文件按 partSize 切成若干块；
//  2. 每块做 MD4，得到各块 16 字节摘要；
//  3. 若只有一块，根哈希就是该块摘要本身；
//     若有多块，根哈希 = MD4(concat(各块摘要))。
//
// 这是 eD2K 网络（eMule / aMule / MLDonkey 等）公认的哈希算法。
func ComputeED2KHash(r io.Reader) (string, error) {
	const md4Size = 16
	partHashes := make([]byte, 0, md4Size*2)
	buf := make([]byte, 32*1024)
	var partBuf []byte

	for {
		// 读取一个完整分块（每块恰好 partSize 字节，最后一块可更小）。
		// 注意：单次 Read 可能把当前分块剩余字节一次性全部返回，
		// 因此这里把读取长度限制为「当前分块还差多少」，避免越过分块边界。
		partBuf = partBuf[:0]
		for len(partBuf) < partSize {
			want := partSize - len(partBuf)
			if want > len(buf) {
				want = len(buf)
			}
			n, err := r.Read(buf[:want])
			if n > 0 {
				partBuf = append(partBuf, buf[:n]...)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("读取数据失败: %w", err)
			}
		}
		if len(partBuf) == 0 {
			break // 文件已读完
		}

		sum := md4Sum(partBuf)
		partHashes = append(partHashes, sum[:]...)

		if len(partBuf) < partSize {
			break // 已经是最后一块（不足一个分块大小）
		}
	}

	if len(partHashes) == 0 {
		// 空文件：约定 eD2K 根哈希 = MD4("") = 31d6cfe0d16ae931b73c59d7e0c089c0
		return hex.EncodeToString(md4Sum(nil)), nil
	}

	if len(partHashes) == md4Size {
		// 只有一个分块：根哈希即该分块哈希
		return hex.EncodeToString(partHashes), nil
	}

	// 多分块：对所有分块哈希再做一次 MD4
	sum := md4Sum(partHashes)
	return hex.EncodeToString(sum[:]), nil
}

// ParseED2KLink 解析 ed2k:// 链接。
// 支持格式：ed2k://|file|<name>|<size>|<hash>|/  以及附加字段：
//   - |h=<aich>|  AICH 根哈希
//   - |sources,host:port,host:port|  已知来源（可选）
func ParseED2KLink(link string) (*ED2KHashResult, error) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(strings.ToLower(link), "ed2k://") {
		return nil, fmt.Errorf("不是合法的 ed2k 链接: %s", link)
	}

	// 去掉 "ed2k://" 前缀，按 "|" 拆分
	body := link[len("ed2k://"):]
	parts := strings.Split(body, "|")
	// 期望: ["", "file", name, size, hash, "/", ...可选]
	if len(parts) < 5 {
		return nil, fmt.Errorf("ed2k 链接字段不足: %s", link)
	}
	if strings.ToLower(parts[1]) != "file" {
		return nil, fmt.Errorf("仅支持 file 类型 ed2k 链接: %s", link)
	}

	name := parts[2]
	size, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || size < 0 {
		return nil, fmt.Errorf("ed2k 链接中文件大小非法: %q", parts[3])
	}
	hash := strings.ToLower(parts[4])
	if len(hash) != 32 {
		return nil, fmt.Errorf("ed2k 链接中哈希长度应为 32 个十六进制字符，实际为 %d", len(hash))
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return nil, fmt.Errorf("ed2k 链接中哈希不是合法十六进制: %w", err)
	}

	res := &ED2KHashResult{
		Name: name,
		Size: size,
		Hash: hash,
	}

	// 解析附加字段（第 6 个及之后）
	for i := 5; i < len(parts); i++ {
		tok := parts[i]
		if tok == "" || tok == "/" {
			continue
		}
		if strings.HasPrefix(tok, "h=") {
			res.AICH = strings.TrimPrefix(tok, "h=")
			continue
		}
		if strings.HasPrefix(tok, "sources,") {
			spec := strings.TrimPrefix(tok, "sources,")
			for _, s := range strings.Split(spec, ",") {
				if s != "" {
					res.Sources = append(res.Sources, s)
				}
			}
		}
	}

	return res, nil
}
