package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/lkm/log"
	"net"
)

type Filter interface {
	AddSource(ssrc uint32, source GBSource) bool

	Input(conn net.Conn, data []byte) GBSource

	ParseRtpPacket(conn net.Conn, data []byte) (*rtp.Packet, error)
}

type FilterImpl struct {
}

func (r FilterImpl) ParseRtpPacket(conn net.Conn, data []byte) (*rtp.Packet, error) {
	packet := rtp.Packet{}
	err := packet.Unmarshal(data)

	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())
		return nil, err
	}

	return &packet, err
}
