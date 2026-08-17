package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
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
