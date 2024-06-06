package gb28181

import (
	"fmt"
	"github.com/pion/rtp"
	"github.com/yangjiechina/lkm/jitterbuffer"
	"github.com/yangjiechina/lkm/stream"
)

type UDPSource struct {
	BaseGBSource

	rtpDeMuxer *jitterbuffer.JitterBuffer

	rtpBuffer stream.MemoryPool
}

func NewUDPSource() *UDPSource {
	return &UDPSource{
		rtpDeMuxer: jitterbuffer.New(),
		rtpBuffer:  stream.NewDirectMemoryPool(JitterBufferSize),
	}
}

func (u UDPSource) TransportType() TransportType {
	return TransportTypeUDP
}

func (u UDPSource) InputRtp(pkt *rtp.Packet) error {
	n := u.rtpBuffer.Capacity() - u.rtpBuffer.Size()
	if n < len(pkt.Payload) {
		return fmt.Errorf("RTP receive buffer overflow")
	}

	allocate := u.rtpBuffer.Allocate(len(pkt.Payload))
	copy(allocate, pkt.Payload)
	pkt.Payload = allocate
	u.rtpDeMuxer.Push(pkt)

	for {
		pkt, _ := u.rtpDeMuxer.Pop()
		if pkt == nil {
			return nil
		}

		u.rtpBuffer.FreeHead()

		u.PublishSource.AddEvent(stream.SourceEventInput, pkt.Payload)
	}
}
