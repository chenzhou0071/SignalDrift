package protocol

import (
	"encoding/binary"
	"errors"
)

const (
	Magic       uint16 = 0x5344 // "SD"
	HeaderSize         = 12
	MaxBodySize        = 64 * 1024
)

var (
	ErrBadMagic     = errors.New("protocol: bad magic")
	ErrBodyTooLarge = errors.New("protocol: body too large")
)

type Frame struct {
	MsgID uint16
	Seq   uint32
	Body  []byte
}

func Encode(f *Frame) []byte {
	b := make([]byte, HeaderSize+len(f.Body))
	binary.BigEndian.PutUint16(b[0:2], Magic)
	binary.BigEndian.PutUint16(b[2:4], f.MsgID)
	binary.BigEndian.PutUint32(b[4:8], f.Seq)
	binary.BigEndian.PutUint32(b[8:12], uint32(len(f.Body)))
	copy(b[HeaderSize:], f.Body)
	return b
}
