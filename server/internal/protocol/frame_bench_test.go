package protocol

import (
	"bytes"
	"testing"
)

func BenchmarkEncode(b *testing.B) {
	f := &Frame{MsgID: MsgEcho, Seq: 42, Body: bytes.Repeat([]byte("x"), 64)}
	b.SetBytes(int64(HeaderSize + len(f.Body)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Encode(f)
	}
}

func BenchmarkFrameReaderNext(b *testing.B) {
	raw := Encode(&Frame{MsgID: MsgEcho, Seq: 1, Body: bytes.Repeat([]byte("x"), 64)})
	payload := bytes.Repeat(raw, 1024) // 一个 buffer 里塞 1024 帧，模拟粘包流
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(payload)
		fr := NewFrameReader(r)
		for {
			if _, err := fr.Next(); err != nil {
				break // 读到 EOF
			}
		}
	}
}
