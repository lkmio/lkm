package stream

import (
	"encoding/binary"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
)

type RtpStream struct {
	BaseTransStream
	rtpBuffers *collections.Queue[*collections.ReferenceCounter[[]byte]]
}

func (f *RtpStream) WriteHeader() error {
	return nil
}

func (f *RtpStream) Input(packet *avformat.AVPacket) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	size := 2 + uint16(len(packet.Data))
	if size > UDPReceiveBufferSize {
		log.Sugar.Errorf("转发%s流失败 rtp包过长, 长度：%d, 最大允许：%d", f.Protocol, len(packet.Data), UDPReceiveBufferSize)
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
		UDPReceiveBufferPool.Put(data[:cap(data)])
	}

	bytes := UDPReceiveBufferPool.Get().([]byte)
	binary.BigEndian.PutUint16(bytes, size-2)
	copy(bytes[2:], packet.Data)

	rtp := collections.NewReferenceCounter(bytes[:size])
	f.rtpBuffers.Push(rtp)

	// 每帧都当关键帧, 直接发给上级
	return []*collections.ReferenceCounter[[]byte]{rtp}, -1, true, nil
}

func NewRtpTransStream(protocol TransStreamProtocol, capacity int) *RtpStream {
	return &RtpStream{
		BaseTransStream: BaseTransStream{Protocol: protocol},
		rtpBuffers:      collections.NewQueue[*collections.ReferenceCounter[[]byte]](capacity),
	}
}
