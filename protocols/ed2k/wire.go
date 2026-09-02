package ed2k

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// eD2K 协议字节（报文头第 1 字节）
const (
	protoED2K   = 0xE3 // 标准 eDonkey2000 协议
	protoEMule  = 0xC5 // eMule 扩展协议
	protoPacked = 0xD4 // 载荷经 zlib 压缩（仅用于接收端解压）
)

// 客户端 ↔ 服务器 常用 opcode
const (
	opLoginRequest = 0x01 // C→S 登录请求
	opServerHello  = 0x02 // S→C 服务器问候（含服务器 IP/端口/标签）
	opGetSources   = 0x14 // C→S 按哈希查询文件来源
	opOfferFiles   = 0x04 // C→S 共享文件广播
	opServerStatus = 0x34 // S→C 服务器状态（用户数/文件数）
	opServerIdent  = 0x41 // S→C 服务器标识
	opServerMsg    = 0x38 // S→C 服务器消息
	opIDChange     = 0x40 // S→C 分配客户端 ID
	opFoundSources = 0x42 // S→C 来源列表（也兼容 0x43）
	opFoundSourcesAlt = 0x43
)

// 客户端 ↔ 客户端 常用 opcode
const (
	opHello        = 0x01 // C↔C 握手
	opHelloAnswer  = 0x02 // C↔C 握手应答
	opReject       = 0x04 // C↔C 拒绝连接
	opFileRequest  = 0x05 // C→C 请求某个文件
	opFileReqAnswer = 0x06 // C↔C 请求应答（0x00 接受 / 0x01 无此文件）
	opRequestParts = 0x14 // C→C 请求若干分块
	opPartData     = 0x15 // C↔C 分块数据
	opEndOfDownload = 0x49 // C↔C 下载完成通知
	opQueueRank    = 0x60 // C↔C 队列排名（表示被排队，需要等待）
)

// 标签类型（TLV）
const (
	tagTypeString = 0x02
	tagTypeDWORD  = 0x03
	tagTypeFloat  = 0x04
	tagTypeBool   = 0x05
	tagTypeBlob   = 0x06 // 旧式 blob（name 为字符串）
)

// 标签名常量（单字节）
const (
	ctName         = 0x01 // 昵称
	ctVersion      = 0x11 // 协议版本
	ctPort         = 0x0f // 端口
	ctMuleVersion  = 0xfb // eMule 版本
	ctFlags        = 0x20 // 能力标志
	ctServerFlags  = 0x21 // 服务器能力标志（newtags/unicode/largefiles 等）
)

// 客户端能力标志（CT_FLAGS 的值）
const (
	flZlib     = 0x01 // 支持 zlib 压缩
	flIPInLogin = 0x02 // 登录时上报自身 IP
	flAuxPort  = 0x04
	flNewTags  = 0x08 // 支持新式标签
	flUnicode  = 0x10 // 支持 Unicode 字符串
)

// serverTCPFlags 服务器 IDChange 中的能力位（这里仅用到的）
const (
	srvTCPFLGCompression = 0x00000001
	srvTCPFLGNewTags    = 0x00000008
	srvTCPFLGLargeFiles = 0x00000010
)

// leWriter 是简易的小端写入缓冲，便于构造报文。
type leWriter struct {
	buf bytes.Buffer
}

func (w *leWriter) byte(b byte)   { w.buf.WriteByte(b) }
func (w *leWriter) bytes(b []byte) { w.buf.Write(b) }

func (w *leWriter) u16(v uint16) {
	var t [2]byte
	binary.LittleEndian.PutUint16(t[:], v)
	w.buf.Write(t[:])
}

func (w *leWriter) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	w.buf.Write(t[:])
}

func (w *leWriter) hash(h string) {
	b, _ := hexDecodeHash(h)
	w.buf.Write(b)
}

func (w *leWriter) string(s string) {
	w.buf.WriteString(s)
}

func (w *leWriter) data() []byte { return w.buf.Bytes() }

// leReader 是简易的小端读取器，带越界保护。
type leReader struct {
	data []byte
	pos  int
	err  error
}

func newReader(b []byte) *leReader { return &leReader{data: b} }

func (r *leReader) failf(format string, a ...interface{}) {
	if r.err == nil {
		r.err = fmt.Errorf(format, a...)
	}
}

func (r *leReader) byte() byte {
	if r.err != nil {
		return 0
	}
	if r.pos+1 > len(r.data) {
		r.failf("读取 byte 越界")
		return 0
	}
	b := r.data[r.pos]
	r.pos += 1
	return b
}

func (r *leReader) u16() uint16 {
	if r.err != nil {
		return 0
	}
	if r.pos+2 > len(r.data) {
		r.failf("读取 u16 越界")
		return 0
	}
	v := binary.LittleEndian.Uint16(r.data[r.pos : r.pos+2])
	r.pos += 2
	return v
}

func (r *leReader) u32() uint32 {
	if r.err != nil {
		return 0
	}
	if r.pos+4 > len(r.data) {
		r.failf("读取 u32 越界")
		return 0
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v
}

func (r *leReader) hash() string {
	if r.err != nil {
		return ""
	}
	if r.pos+16 > len(r.data) {
		r.failf("读取 hash 越界")
		return ""
	}
	h := hexEncode(r.data[r.pos : r.pos+16])
	r.pos += 16
	return h
}

func (r *leReader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.pos+n > len(r.data) {
		r.failf("读取 %d 字节越界", n)
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *leReader) rest() []byte {
	if r.err != nil {
		return nil
	}
	b := r.data[r.pos:]
	r.pos = len(r.data)
	return b
}

// tag 是 eD2K 的 TLV 结构。
type tag struct {
	name  byte
	kind  byte // tagType*
	value interface{}
}

// writeTags 按经典 eDonkey 标签格式写入若干标签。
// 格式：type(1) + nameLen(2) + name + value
//   - 字符串：valueLen(2) + value 字节
//   - DWORD：4 字节小端
func writeTags(w *leWriter, tags []tag) {
	for _, t := range tags {
		w.byte(t.kind)
		w.u16(1) // name 长度固定 1 字节（单字节标签名）
		w.byte(t.name)
		switch t.kind {
		case tagTypeString:
			s := t.value.(string)
			w.u16(uint16(len(s)))
			w.string(s)
		case tagTypeDWORD:
			w.u32(uint32(t.value.(uint32)))
		case tagTypeFloat:
			w.u32(t.value.(uint32))
		default:
			w.u32(t.value.(uint32))
		}
	}
}

// readTags 解析经典标签序列，count 为标签个数。
func readTags(r *leReader, count uint32) []tag {
	var out []tag
	for i := uint32(0); i < count && r.err == nil; i++ {
		kind := r.byte()
		nameLen := r.u16()
		var name byte
		if nameLen > 0 {
			nb := r.bytes(int(nameLen))
			if len(nb) > 0 {
				name = nb[0]
			}
		}
		var value interface{}
		switch kind {
		case tagTypeString:
			vl := r.u16()
			value = string(r.bytes(int(vl)))
		case tagTypeDWORD, tagTypeFloat:
			value = r.u32()
		case tagTypeBool:
			value = r.byte()
		default:
			// 未知类型：尽量跳过（假定 4 字节）
			value = r.u32()
		}
		if r.err != nil {
			break
		}
		out = append(out, tag{name: name, kind: kind, value: value})
	}
	return out
}

// packet 表示从连接上读到的一个完整 eD2K 报文。
type packet struct {
	opcode  byte
	payload []byte
}

// readPacket 从连接读取一个完整报文（处理 0xD4 zlib 压缩）。
func readPacket(conn io.Reader) (*packet, error) {
	hdr := make([]byte, 6)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, err
	}
	proto := hdr[0]
	size := binary.LittleEndian.Uint32(hdr[1:5])
	opcode := hdr[5]
	if size > 16*1024*1024 {
		return nil, fmt.Errorf("报文过大: %d 字节", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}

	if proto == protoPacked {
		// 0xD4：载荷被 zlib 压缩，解压后得到原始 payload
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("zlib 解压失败: %w", err)
		}
		defer zr.Close()
		dec, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("zlib 读取失败: %w", err)
		}
		body = dec
	}

	return &packet{opcode: opcode, payload: body}, nil
}

// writePacket 发送一个未压缩的 eD2K 报文（proto=0xE3）。
func writePacket(conn io.Writer, opcode byte, payload []byte) error {
	var hdr [6]byte
	hdr[0] = protoED2K
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(len(payload)))
	hdr[5] = opcode
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}
