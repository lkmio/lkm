package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

type UDPServer struct {
	udp    *transport.UDPServer
	filter Filter
}

func (U UDPServer) OnConnected(conn net.Conn) []byte {
	return nil
}

func (U UDPServer) OnPacket(conn net.Conn, data []byte) []byte {
	packet := rtp.Packet{}
	err := packet.Unmarshal(data)
	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())
		return nil
	}

	source := U.filter.FindSource(packet.SSRC)
	if source == nil {
		log.Sugar.Errorf("ssrc匹配source失败 ssrc:%x conn:%s", packet.SSRC, conn.RemoteAddr().String())
		return nil
	}

	if stream.SessionStateHandshakeDone == source.State() {
		source.PreparePublish(conn, packet.SSRC, source)
	}

	source.InputRtp(&packet)
	return nil
}

func (U UDPServer) OnDisConnected(conn net.Conn, err error) {

}

func NewUDPServer(addr net.Addr, filter Filter) (*UDPServer, error) {
	server := &UDPServer{
		filter: filter,
	}

	udp, err := transport.NewUDPServer(addr, server)
	if err != nil {
		return nil, err
	}

	server.udp = udp
	return server, nil
}
