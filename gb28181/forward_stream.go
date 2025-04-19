package gb28181

import (
	"encoding/binary"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
)

// ForwardStream 国标级联转发流, 下级推什么, 就向上级发什么.
type ForwardStream struct {
	stream.BaseTransStream
	rtpBuffers *collections.Queue[*collections.ReferenceCounter[[]byte]]
}

func (f *ForwardStream) WriteHeader() error {
	return nil
}

func (f *ForwardStream) Input(packet *avformat.AVPacket) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	size := 2 + uint16(len(packet.Data))
	if size > stream.UDPReceiveBufferSize {
		log.Sugar.Errorf("国标级联转发流失败 rtp包过长, 长度：%d, 最大允许：%d", len(packet.Data), stream.UDPReceiveBufferSize)
		return nil, 0, false, nil
	}

	// 释放rtp包
	for f.rtpBuffers.Size() > 0 {
		rtp := f.rtpBuffers.Peek(0)
		if rtp.UseCount() > 1 {
			break
		}

		f.rtpBuffers.Pop()

		// 放回池中
		data := rtp.Get()
		stream.UDPReceiveBufferPool.Put(data[:cap(data)])
	}

	bytes := stream.UDPReceiveBufferPool.Get().([]byte)
	binary.BigEndian.PutUint16(bytes, size-2)
	copy(bytes[2:], packet.Data)

	rtp := collections.NewReferenceCounter(bytes[:size])
	f.rtpBuffers.Push(rtp)
	// 每帧都当关键帧, 直接发给上级
	return []*collections.ReferenceCounter[[]byte]{rtp}, -1, true, nil
}

func NewTransStream() (stream.TransStream, error) {
	return &ForwardStream{
		BaseTransStream: stream.BaseTransStream{Protocol: stream.TransStreamGBStreamForward},
		rtpBuffers:      collections.NewQueue[*collections.ReferenceCounter[[]byte]](1024),
	}, nil
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return NewTransStream()
}
