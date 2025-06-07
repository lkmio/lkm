package stream

import (
	"encoding/binary"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
)

type RtpStream struct {
	BaseTransStream
	rtpBuffer *RtpBuffer
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

	counter := f.rtpBuffer.Get()
	bytes := counter.Get()
	binary.BigEndian.PutUint16(bytes, size-2)
	copy(bytes[2:], packet.Data)
	counter.ResetData(bytes[:2+len(bytes)])

	// 每帧都当关键帧, 直接发给上级
	return []*collections.ReferenceCounter[[]byte]{counter}, -1, true, nil
}

func NewRtpTransStream(protocol TransStreamProtocol, capacity int) *RtpStream {
	return &RtpStream{
		BaseTransStream: BaseTransStream{Protocol: protocol},
		rtpBuffer:       NewRtpBuffer(capacity),
	}
}

func GBCascadedTransStreamFactory(_ Source, _ TransStreamProtocol, _ []*Track, _ Sink) (TransStream, error) {
	return NewRtpTransStream(TransStreamGBCascaded, 1024), nil
}
