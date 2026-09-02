package ed2k

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"go-download-server/internal/logger"
)

// peerSource 描述一个对等端的网络位置。
type peerSource struct {
	userHash string
	id       uint32 // 高 ID，等价于对端 IP（小端 uint32）
	port     uint16
}

// ed2kServer 表示一个已连接的 eD2K 服务器连接。
type ed2kServer struct {
	host      string
	port      int
	conn      net.Conn
	clientID  uint32 // 登录后由服务器分配
	clientHash string
	tcpPort   uint16
	nick      string
}

// dialServer 建立到 eD2K 服务器的 TCP 连接。
func dialServer(host string, port int, timeout time.Duration, clientHash string, tcpPort uint16, nick string) (*ed2kServer, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("连接 eD2K 服务器 %s 失败: %w", addr, err)
	}
	s := &ed2kServer{
		host:       host,
		port:       port,
		conn:       conn,
		clientHash: clientHash,
		tcpPort:    tcpPort,
		nick:       nick,
	}
	if err := s.login(); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

// login 发送登录请求并处理服务器的问候 / ID 变更。
func (s *ed2kServer) login() error {
	s.conn.SetDeadline(time.Now().Add(30 * time.Second))

	w := &leWriter{}
	w.hash(s.clientHash)
	w.u32(0) // 首次登录 clientID 为 0
	w.u16(s.tcpPort)
	w.u32(2) // 标签数
	writeTags(w, []tag{
		{ctName, tagTypeString, s.nick},
		{ctVersion, tagTypeDWORD, uint32(60)},
	})
	if err := writePacket(s.conn, opLoginRequest, w.data()); err != nil {
		return fmt.Errorf("发送登录请求失败: %w", err)
	}

	// 读取服务器响应，直到拿到 IDChange（0x40）或超时
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s.conn.SetDeadline(deadline)
		pkt, err := readPacket(s.conn)
		if err != nil {
			return fmt.Errorf("读取服务器响应失败: %w", err)
		}
		switch pkt.opcode {
		case opServerHello:
			logger.Debugf("ed2k: 收到服务器 %s 问候", s.host)
		case opServerStatus:
			// users(4) + files(4)，忽略
		case opServerIdent:
			// serverHash(16)+ip(4)+port(2)+tags，忽略
		case opServerMsg:
			msg := string(pkt.payload)
			logger.Infof("ed2k: 服务器 %s 消息: %s", s.host, strings.TrimSpace(msg))
		case opIDChange:
			r := newReader(pkt.payload)
			s.clientID = r.u32()
			_ = r.u32() // 服务器能力标志
			logger.Infof("ed2k: 已登录服务器 %s，分配客户端 ID: %d (0x%08X)", s.host, s.clientID, s.clientID)
			return nil
		default:
			logger.Debugf("ed2k: 服务器 %s 未知报文 opcode=0x%02X len=%d", s.host, pkt.opcode, len(pkt.payload))
		}
	}
	return fmt.Errorf("登录服务器 %s 超时（未收到 IDChange）", s.host)
}

// querySources 向服务器查询文件来源，并持续读取一段时间收集来源。
// 由于来源可能分多包返回，这里阻塞读取直到 ctx 取消或超时。
func (s *ed2kServer) querySources(ctx context.Context, fileHash string, collect time.Duration) ([]peerSource, error) {
	w := &leWriter{}
	w.hash(fileHash)
	if err := writePacket(s.conn, opGetSources, w.data()); err != nil {
		return nil, fmt.Errorf("发送来源查询失败: %w", err)
	}
	logger.Infof("ed2k: 已向服务器 %s 查询文件来源 hash=%s", s.host, fileHash)

	var sources []peerSource
	deadline := time.Now().Add(collect)
	for {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		s.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		pkt, err := readPacket(s.conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				// 暂时无数据，继续等待
				continue
			}
			// 连接断开
			break
		}
		switch pkt.opcode {
		case opFoundSources, opFoundSourcesAlt:
			srcs := parseFoundSources(pkt.payload)
			for _, src := range srcs {
				if src.id >= 0x01000000 && src.port != 0 { // 仅取高 ID（可直接连接）的来源
					sources = append(sources, src)
				}
			}
			logger.Debugf("ed2k: 服务器 %s 返回 %d 个来源", s.host, len(srcs))
		case opIDChange:
			// 忽略（可能在查询期间再次收到）
		default:
			logger.Debugf("ed2k: 来源查询期间忽略 opcode=0x%02X", pkt.opcode)
		}
	}
	return sources, nil
}

// parseFoundSources 解析服务器返回的来源列表。
// 采用 eMule 扩展格式：fileHash(16) + count(4) + [userHash(16)+clientID(4)+port(2)+sourceType(1)+tags(4+...)].
// 为兼容不同服务器，解析失败或计数异常时优雅停止，不 panic。
func parseFoundSources(payload []byte) []peerSource {
	r := newReader(payload)
	_ = r.hash() // fileHash，忽略
	cnt := r.u32()
	if r.err != nil || cnt == 0 || cnt > 10000 {
		return nil
	}
	var out []peerSource
	for i := uint32(0); i < cnt && r.err == nil; i++ {
		uh := r.hash()     // 16
		id := r.u32()      // 4
		port := r.u16()    // 2
		_ = r.byte()       // 1 字节 source type（eMule 扩展）
		_ = readTags(r, r.u32()) // 可选标签块
		if r.err != nil {
			break
		}
		out = append(out, peerSource{userHash: uh, id: id, port: port})
	}
	return out
}

// idToIP 将 eD2K 客户端 ID（等价于 IP 的小端 uint32）转为点分十进制。
func idToIP(id uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		id&0xff, (id>>8)&0xff, (id>>16)&0xff, (id>>24)&0xff)
}

func (s *ed2kServer) close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
}
