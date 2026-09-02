package ed2k

import (
	"bytes"
	"encoding/hex"
	"io"
	"net"
	"testing"
)

func md4hex(b []byte) string {
	return hex.EncodeToString(md4Sum(b))
}

func TestComputeED2KHashEmpty(t *testing.T) {
	got, err := ComputeED2KHash(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 空文件的 eD2K 根哈希 = MD4("")
	want := md4hex(nil)
	if got != want {
		t.Errorf("空文件哈希错误: got %s want %s", got, want)
	}
}

func TestComputeED2KHashSinglePart(t *testing.T) {
	data := []byte("hello world")
	// 单分块：eD2K 根哈希 = MD4(data)
	want := md4hex(data)

	got, err := ComputeED2KHash(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("单分块哈希错误: got %s want %s", got, want)
	}
}

func TestComputeED2KHashMultiPart(t *testing.T) {
	// 构造一个略大于一个分块的数据（多一个分块）
	data := bytes.Repeat([]byte("A"), partSize+5)
	part0 := md4Sum(data[:partSize])
	part1 := md4Sum(data[partSize:])
	root := md4Sum(append(part0, part1...))
	want := hex.EncodeToString(root)

	got, err := ComputeED2KHash(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("多分块哈希错误: got %s want %s", got, want)
	}
}

func TestParseED2KLink(t *testing.T) {
	link := "ed2k://|file|test%20file.iso|123456789|ABCDEF0123456789ABCDEF0123456789|/|h=0123456789ABCDEF0123456789ABCDEF|sources,1.2.3.4:4662,5.6.7.8:4661|"
	res, err := ParseED2KLink(link)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if res.Name != "test%20file.iso" {
		t.Errorf("name = %q", res.Name)
	}
	if res.Size != 123456789 {
		t.Errorf("size = %d", res.Size)
	}
	if res.Hash != "abcdef0123456789abcdef0123456789" {
		t.Errorf("hash = %q", res.Hash)
	}
	if res.AICH != "0123456789ABCDEF0123456789ABCDEF" {
		t.Errorf("aich = %q", res.AICH)
	}
	if len(res.Sources) != 2 {
		t.Errorf("sources = %v", res.Sources)
	}
}

func TestParseED2KLinkInvalid(t *testing.T) {
	for _, l := range []string{"http://example.com", "ed2k://|file|name|notanumber|short|/", "ed2k://|file|name|100|zzzz|/"} {
		if _, err := ParseED2KLink(l); err == nil {
			t.Errorf("期望解析失败，但成功: %q", l)
		}
	}
}

func TestTagCodec(t *testing.T) {
	w := &leWriter{}
	w.u32(2)
	writeTags(w, []tag{
		{ctName, tagTypeString, "QuadFetch"},
		{ctVersion, tagTypeDWORD, uint32(60)},
	})
	r := newReader(w.data())
	cnt := r.u32()
	if cnt != 2 {
		t.Fatalf("tag count = %d", cnt)
	}
	tags := readTags(r, cnt)
	if r.err != nil {
		t.Fatalf("readTags error: %v", r.err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags len = %d", len(tags))
	}
	if tags[0].name != ctName {
		t.Errorf("tag0 name = %d", tags[0].name)
	}
	if tags[0].value.(string) != "QuadFetch" {
		t.Errorf("tag0 value = %v", tags[0].value)
	}
	if tags[1].name != ctVersion {
		t.Errorf("tag1 name = %d", tags[1].name)
	}
	if tags[1].value.(uint32) != 60 {
		t.Errorf("tag1 value = %v", tags[1].value)
	}
}

func TestPacketRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	payload := []byte("payload-data")
	go func() {
		if err := writePacket(client, opLoginRequest, payload); err != nil {
			t.Errorf("writePacket: %v", err)
		}
		_ = client.Close()
	}()

	pkt, err := readPacket(server)
	if err != nil && err != io.EOF {
		t.Fatalf("readPacket: %v", err)
	}
	if pkt.opcode != opLoginRequest {
		t.Errorf("opcode = 0x%02X", pkt.opcode)
	}
	if !bytes.Equal(pkt.payload, payload) {
		t.Errorf("payload mismatch: got %q want %q", pkt.payload, payload)
	}
}

func TestParsePartData(t *testing.T) {
	// fileID(16) + partIndex(2)=3 + data
	w := &leWriter{}
	w.hash("00000000000000000000000000000000")
	w.u16(3)
	w.string("hello")
	payload := w.data()

	offset, data, err := parsePartData(payload)
	if err != nil {
		t.Fatalf("parsePartData error: %v", err)
	}
	if offset != partIndexToOffset(3) {
		t.Errorf("offset = %d want %d", offset, partIndexToOffset(3))
	}
	if string(data) != "hello" {
		t.Errorf("data = %q", data)
	}
}
