package rtmp

import (
	"github.com/yangjiechina/lkm/log"
	"net"

	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
)

type Server interface {
	Start(addr net.Addr) error

	Close()
}

func NewServer() Server {
	return &server{}
}

type server struct {
	tcp *transport.TCPServer
}

func (s *server) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	tcp := &transport.TCPServer{}
	tcp.SetHandler(s)
	err := tcp.Bind(addr)

	if err != nil {
		return err
	}

	s.tcp = tcp
	return nil
}

func (s *server) Close() {
	panic("implement me")
}

func (s *server) OnConnected(conn net.Conn) []byte {
	log.Sugar.Debugf("rtmp连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
	return nil
}

func (s *server) OnPacket(conn net.Conn, data []byte) []byte {
	t := conn.(*transport.Conn)
	session := t.Data.(*Session)
	err := session.Input(conn, data)

	if err != nil {
		log.Sugar.Errorf("处理rtmp包失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())

		_ = conn.Close()
		t.Data.(*Session).Close()
		t.Data = nil
	}

	if session.isPublisher {
		return session.receiveBuffer.GetBlock()
	}

	return nil
}

func (s *server) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("rtmp断开连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	if t.Data != nil {
		t.Data.(*Session).Close()
		t.Data = nil
	}
}
