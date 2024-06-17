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

// InputRtp udp收流会先拷贝rtp包,交给jitter buffer处理后再发给source
func (u UDPSource) InputRtp(pkt *rtp.Packet) error {
	block := u.receiveBuffer.GetBlock()

	copy(block, pkt.Payload)
	pkt.Payload = block[:len(pkt.Payload)]
	u.jitterBuffer.Push(pkt)

	for rtp, _ := u.jitterBuffer.Pop(); rtp != nil; rtp, _ = u.jitterBuffer.Pop() {
		u.PublishSource.Input(rtp.Payload)
	}

	return nil
}
