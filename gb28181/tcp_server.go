package gb28181

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/log"
	"net"
)

type TCPServer struct {
	tcp    *transport.TCPServer
	filter Filter
}

type TCPSession struct {
	source  GBSource
	decoder *transport.LengthFieldFrameDecoder
}

func NewTCPServer(addr net.Addr, filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	tcp := &transport.TCPServer{}
	tcp.SetHandler(server)
	if err := tcp.Bind(addr); err != nil {
		return server, err
	}

	server.tcp = tcp
	return server, nil
}

func (T *TCPServer) OnConnected(conn net.Conn) {
	log.Sugar.Infof("客户端链接 conn:%s", conn.RemoteAddr().String())
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) {
	con := conn.(*transport.Conn)
	if con.Data == nil {
		session := &TCPSession{}
		session.decoder = transport.NewLengthFieldFrameDecoder(0xFFFF, 2, func(bytes []byte) {
			source := T.filter.Input(con, bytes[2:])
			if source != nil && session.source == nil {
				session.source = source
			}
		})

		con.Data = session
	}

	con.Data.(*TCPSession).decoder.Input(data)
}

func (T *TCPServer) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Infof("客户端断开链接 conn:%s", conn.RemoteAddr().String())

	con := conn.(*transport.Conn)
	if con.Data != nil {
		con.Data.(*TCPSession).source.Close()
		con.Data = nil
	}
}
