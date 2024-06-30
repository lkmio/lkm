package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/lkm/stream"
)

// UDPSource GB28181 UDP推流源
type UDPSource struct {
	BaseGBSource

	jitterBuffer  *stream.JitterBuffer
	receiveBuffer *stream.ReceiveBuffer
}

func NewUDPSource() *UDPSource {
	u := &UDPSource{
		receiveBuffer: stream.NewReceiveBuffer(1500, stream.ReceiveBufferUdpBlockCount+50),
	}

	u.jitterBuffer = stream.NewJitterBuffer(u.OnOrderedRtp)
	return u
}

func (u *UDPSource) TransportType() TransportType {
	return TransportTypeUDP
}

func (u *UDPSource) OnOrderedRtp(packet interface{}) {
	u.PublishSource.Input(packet.(*rtp.Packet).Payload)
}

// InputRtp udp收流会先拷贝rtp包,交给jitter buffer处理后再发给source
func (u *UDPSource) InputRtp(pkt *rtp.Packet) error {
	block := u.receiveBuffer.GetBlock()

	copy(block, pkt.Payload)
	pkt.Payload = block[:len(pkt.Payload)]
	u.jitterBuffer.Push(pkt.SequenceNumber, pkt)
	return nil
}

func (u *UDPSource) Close() {
	u.jitterBuffer.Flush()
	u.jitterBuffer.Close()
	u.BaseGBSource.Close()
}
