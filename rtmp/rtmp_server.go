package rtmp

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"net"
)

type IServer interface {
	Start(addr net.Addr) error

	Close()
}

type serverImpl struct {
	tcp *transport.TCPServer
}

func (s *serverImpl) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	server := &transport.TCPServer{}
	server.SetHandler(s)
	err := server.Bind(addr.String())

	if err != nil {
		return err
	}

	s.tcp = server
	return nil
}

func (s *serverImpl) Close() {

}

func (s *serverImpl) OnConnected(conn net.Conn) {
	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
}

func (s *serverImpl) OnPacket(conn net.Conn, data []byte) {
	t := conn.(*transport.Conn)
	err := t.Data.(*sessionImpl).Input(conn, data)

	if err != nil {
		_ = conn.Close()
		println("处理rtmp包发生错误:" + err.Error())
	}
}

func (s *serverImpl) OnDisConnected(conn net.Conn, err error) {
	t := conn.(*transport.Conn)
	t.Data.(*sessionImpl).Close()
}
