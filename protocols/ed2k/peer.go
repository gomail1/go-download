package ed2k

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"go-download-server/internal/logger"
)

// gap 表示一个待下载的字节区间 [start, end)。
type gap struct {
	start int64
	end   int64
}

// ed2kPeer 表示一个已握手的对等端连接。
type ed2kPeer struct {
	conn net.Conn
	hash string // 对端 userHash
	id   uint32
	port uint16
}

// dialPeer 连接到对等端并完成 Hello/HelloAnswer 握手。
func dialPeer(ctx context.Context, src peerSource, ourHash string, ourID uint32, ourPort uint16, nick string) (*ed2kPeer, error) {
	addr := net.JoinHostPort(idToIP(src.id), strconv.Itoa(int(src.port)))
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接对等端 %s 失败: %w", addr, err)
	}

	p := &ed2kPeer{conn: conn, id: src.id, port: src.port, hash: src.userHash}

	// 发送 Hello
	if err := p.sendHello(ourHash, ourID, ourPort, nick); err != nil {
		conn.Close()
		return nil, err
	}

	// 读取 HelloAnswer
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	pkt, err := readPacket(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("读取对等端握手响应失败: %w", err)
	}
	if pkt.opcode == opReject {
		conn.Close()
		return nil, fmt.Errorf("对等端 %s 拒绝了连接", addr)
	}
	if pkt.opcode != opHelloAnswer {
		conn.Close()
		return nil, fmt.Errorf("对等端 %s 返回意外握手 opcode=0x%02X", addr, pkt.opcode)
	}
	p.parseHelloAnswer(pkt.payload)
	logger.Debugf("ed2k: 已与对等端 %s 完成握手", addr)
	return p, nil
}

func (p *ed2kPeer) sendHello(ourHash string, ourID uint32, ourPort uint16, nick string) error {
	w := &leWriter{}
	w.hash(ourHash)
	w.u32(ourID)
	w.u16(ourPort)
	w.u32(0) // serverIP（未连接到任一服务器时填 0）
	w.u16(0) // serverPort
	w.u32(2)
	writeTags(w, []tag{
		{ctName, tagTypeString, nick},
		{ctVersion, tagTypeDWORD, uint32(60)},
	})
	return writePacket(p.conn, opHello, w.data())
}

func (p *ed2kPeer) parseHelloAnswer(payload []byte) {
	r := newReader(payload)
	p.hash = r.hash()
	p.id = r.u32()
	p.port = r.u16()
	_ = r.u32() // serverIP
	_ = r.u16() // serverPort
	_ = readTags(r, r.u32())
}

// sendFileRequest 通知对端我们对其某个文件感兴趣（eMule 流程中的一步）。
func (p *ed2kPeer) sendFileRequest(fileHash string) error {
	w := &leWriter{}
	w.hash(fileHash)
	return writePacket(p.conn, opFileRequest, w.data())
}

// sendRequestParts 请求若干分块区间。
func (p *ed2kPeer) sendRequestParts(fileHash string, gaps []gap) error {
	w := &leWriter{}
	w.hash(fileHash)
	w.u16(uint16(len(gaps)))
	for _, g := range gaps {
		w.u32(uint32(g.start))
		w.u32(uint32(g.end))
	}
	return writePacket(p.conn, opRequestParts, w.data())
}

// readPacket 读取一个来自对等端的报文。
func (p *ed2kPeer) readPacket() (*packet, error) {
	return readPacket(p.conn)
}

func (p *ed2kPeer) setReadDeadline(t time.Time) {
	_ = p.conn.SetReadDeadline(t)
}

func (p *ed2kPeer) close() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

// partIndexToOffset 由分块索引得到文件内偏移。
func partIndexToOffset(idx int) int64 { return int64(idx) * partSize }

// offsetToPartIndex 由偏移得到分块索引。
func offsetToPartIndex(off int64) int { return int(off / partSize) }

// parsePartData 解析 PartData(0x15) 报文，返回 [offset, data]。
// eMule 格式：fileID(16) + partIndex(2) + 数据。
func parsePartData(payload []byte) (offset int64, data []byte, err error) {
	r := newReader(payload)
	_ = r.hash() // fileID
	idx := r.u16()
	if r.err != nil {
		return 0, nil, r.err
	}
	offset = partIndexToOffset(int(idx))
	data = r.rest()
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("PartData 为空")
	}
	return offset, data, nil
}

var _ = logger.Infof
