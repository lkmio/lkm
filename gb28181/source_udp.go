package gb28181

import (
	"fmt"
	"github.com/pion/rtp"
	"github.com/yangjiechina/live-server/jitterbuffer"
	"github.com/yangjiechina/live-server/stream"
)

type UDPSource struct {
	GBSourceImpl

	rtpDeMuxer *jitterbuffer.JitterBuffer

	rtpBuffer stream.MemoryPool
}

func NewUDPSource() *UDPSource {
	return &UDPSource{
		rtpDeMuxer: jitterbuffer.New(),
		rtpBuffer:  stream.NewDirectMemoryPool(JitterBufferSize),
	}
}

func (u UDPSource) Transport() Transport {
	return TransportUDP
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

		u.SourceImpl.AddEvent(stream.SourceEventInput, pkt.Payload)
	}
}
