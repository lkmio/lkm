package gb28181

import (
	"encoding/binary"
	"github.com/lkmio/lkm/stream"
)

// ForwardStream 国标级联转发流, 下级推什么, 就向上级发什么.
type ForwardStream struct {
	stream.BaseTransStream
	buffer *stream.ReceiveBuffer
}

func (f *ForwardStream) WriteHeader() error {
	return nil
}

func (f *ForwardStream) WrapData(data []byte) []byte {
	block := f.buffer.GetBlock()
	copy(block[2:], data)
	binary.BigEndian.PutUint16(block, uint16(len(data)))
	return block
}

func (f *ForwardStream) OutStreamBufferCapacity() int {
	return f.buffer.BlockCount()
}

func NewTransStream() (stream.TransStream, error) {
	return &ForwardStream{
		BaseTransStream: stream.BaseTransStream{Protocol: stream.TransStreamGBStreamForward},
		buffer:          stream.NewReceiveBuffer(1500, 512),
	}, nil
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return NewTransStream()
}
