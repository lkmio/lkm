package gb28181

import (
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/rtp"
	"net"
)

// UDPSource 国标UDP推流源
type UDPSource struct {
	stream.StreamServer[interface{}]
	BaseGBSource
	jitterBuffer *stream.JitterBuffer[*rtp.Packet]
}

func (u *UDPSource) SetupType() SetupType {
	return SetupUDP
}

// OnOrderedRtp 有序RTP包回调
func (u *UDPSource) OnOrderedRtp(packet *rtp.Packet) {
	_ = u.ProcessPacket(packet.Raw)
	// 处理完后, 归还buffer
	stream.UDPReceiveBufferPool.Put(packet.Raw[:cap(packet.Raw)])
}

// InputRtpPacket 将RTP包排序后，交给Source处理
func (u *UDPSource) InputRtpPacket(pkt *rtp.Packet) error {
	block := stream.UDPReceiveBufferPool.Get().([]byte)
	copy(block, pkt.Raw)

	pkt.Raw = block[:len(pkt.Raw)]
	u.jitterBuffer.Push(pkt.SequenceNumber, pkt)
	for pop := u.jitterBuffer.Pop(true); pop != nil; pop = u.jitterBuffer.Pop(true) {
		u.OnOrderedRtp(pop)
	}
	return nil
}

func (u *UDPSource) Close() {
	// 清空剩余的包
	for pop := u.jitterBuffer.Pop(false); pop != nil; pop = u.jitterBuffer.Pop(false) {
		u.OnOrderedRtp(pop)
	}

	u.BaseGBSource.Close()
}

func (u *UDPSource) OnPacket(conn net.Conn, data []byte) []byte {
	u.StreamServer.OnPacket(conn, data)

	packet := rtp.Packet{}
	err := packet.Unmarshal(data)
	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())
		return nil
	} else if u.Conn == nil {
		u.Conn = conn
	}

	packet.Raw = data
	_ = u.InputRtpPacket(&packet)
	return nil
}

func NewUDPSource() *UDPSource {
	source := &UDPSource{
		jitterBuffer: stream.NewJitterBuffer[*rtp.Packet](),
	}

	source.StreamServer = stream.StreamServer[interface{}]{
		SourceType: stream.SourceType28181,
	}

	return source
}
