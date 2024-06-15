package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/lkm/jitterbuffer"
	"github.com/yangjiechina/lkm/stream"
)

// UDPSource GB28181 UDP推流源
type UDPSource struct {
	BaseGBSource

	jitterBuffer  *jitterbuffer.JitterBuffer
	receiveBuffer *stream.ReceiveBuffer
}

func NewUDPSource() *UDPSource {
	return &UDPSource{
		jitterBuffer:  jitterbuffer.New(),
		receiveBuffer: stream.NewReceiveBuffer(1500, stream.ReceiveBufferUdpBlockCount+50),
	}
}

func (u UDPSource) TransportType() TransportType {
	return TransportTypeUDP
}

// InputRtp UDP收流会先拷贝rtp包,交给jitter buffer处理后再发给source
func (u UDPSource) InputRtp(pkt *rtp.Packet) error {
	block := u.receiveBuffer.GetBlock()

	copy(block, pkt.Payload)
	pkt.Payload = block[:len(pkt.Payload)]
	u.jitterBuffer.Push(pkt)

	for {
		pkt, _ := u.jitterBuffer.Pop()
		if pkt == nil {
			return nil
		}

		u.PublishSource.Input(pkt.Payload)
	}
}
