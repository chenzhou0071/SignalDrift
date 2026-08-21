// frame_test.go — 帧编解码测试：往返/粘包分包/超限/畸形输入
package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
	"testing/iotest"
)

func TestEncodeLayout(t *testing.T) {
	f := &Frame{MsgID: 7, Seq: 42, Body: []byte("abc")}
	b := Encode(f)
	if len(b) != HeaderSize+3 {
		t.Fatalf("len=%d", len(b))
	}
	if binary.BigEndian.Uint16(b[0:2]) != Magic {
		t.Fatal("bad magic")
	}
	if binary.BigEndian.Uint16(b[2:4]) != 7 || binary.BigEndian.Uint32(b[4:8]) != 42 {
		t.Fatal("bad msgid/seq")
	}
	if binary.BigEndian.Uint32(b[8:12]) != 3 || !bytes.Equal(b[12:], []byte("abc")) {
		t.Fatal("bad body")
	}
}

func TestEncodeEmptyBody(t *testing.T) {
	b := Encode(&Frame{MsgID: 1, Seq: 1})
	if len(b) != HeaderSize {
		t.Fatalf("len=%d", len(b))
	}
}

func TestFrameReaderMultiFrame(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(Encode(&Frame{MsgID: 1, Seq: 1, Body: []byte("aa")}))
	buf.Write(Encode(&Frame{MsgID: 2, Seq: 2, Body: []byte("bbbb")}))
	fr := NewFrameReader(&buf)
	f1, err := fr.Next()
	if err != nil || f1.MsgID != 1 || string(f1.Body) != "aa" {
		t.Fatalf("f1=%+v err=%v", f1, err)
	}
	f2, err := fr.Next()
	if err != nil || f2.Seq != 2 || string(f2.Body) != "bbbb" {
		t.Fatalf("f2=%+v err=%v", f2, err)
	}
}

func TestFrameReaderFragmented(t *testing.T) {
	// iotest.OneByteReader 模拟极端拆包：每次只读 1 字节
	raw := Encode(&Frame{MsgID: 9, Seq: 3, Body: []byte("hello")})
	fr := NewFrameReader(iotest.OneByteReader(bytes.NewReader(raw)))
	f, err := fr.Next()
	if err != nil || f.MsgID != 9 || string(f.Body) != "hello" {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}

func TestFrameReaderBadMagic(t *testing.T) {
	raw := Encode(&Frame{MsgID: 1, Seq: 1})
	raw[0] = 0xFF
	if _, err := NewFrameReader(bytes.NewReader(raw)).Next(); err != ErrBadMagic {
		t.Fatalf("err=%v", err)
	}
}

func TestFrameReaderBodyTooLarge(t *testing.T) {
	raw := Encode(&Frame{MsgID: 1, Seq: 1})
	binary.BigEndian.PutUint32(raw[8:12], MaxBodySize+1)
	if _, err := NewFrameReader(bytes.NewReader(raw)).Next(); err != ErrBodyTooLarge {
		t.Fatalf("err=%v", err)
	}
}
