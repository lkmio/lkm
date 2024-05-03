package gb28181

import (
	"github.com/yangjiechina/avformat/transport"
	"net"
)

type UDPServer struct {
	udp    *transport.UDPTransport
	filter Filter
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

func (U UDPServer) OnConnected(conn net.Conn) {

}

func (U UDPServer) OnPacket(conn net.Conn, data []byte) {
	U.filter.Input(conn, data)
}

func (U UDPServer) OnDisConnected(conn net.Conn, err error) {

}
