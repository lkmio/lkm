package gb28181

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
)

// ForwardStream 国标级联转发流, 下级推什么, 就向上级发什么.
type ForwardStream struct {
	stream.BaseTransStream
}

func (f *ForwardStream) WriteHeader() error {
	return nil
}

func NewTransStream() (stream.TransStream, error) {
	return &ForwardStream{BaseTransStream: stream.BaseTransStream{Protocol_: stream.TransStreamGBStreamForward}}, nil
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, streams []utils.AVStream) (stream.TransStream, error) {
	return NewTransStream()
}
