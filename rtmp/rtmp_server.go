package rtmp

import (
	"github.com/yangjiechina/live-server/log"
	"net"

	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
)

type IServer interface {
	Start(addr net.Addr) error

	Close()
}

func NewServer() IServer {
	return &serverImpl{}
}

type serverImpl struct {
	tcp *transport.TCPServer
}

func (s *serverImpl) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	server := &transport.TCPServer{}
	server.SetHandler(s)
	err := server.Bind(addr)

	if err != nil {
		return err
	}

	s.tcp = server
	return nil
}

func (s *serverImpl) Close() {
	panic("implement me")
}

func (s *serverImpl) OnConnected(conn net.Conn) {
	log.Sugar.Debugf("rtmp连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
}

func (s *serverImpl) OnPacket(conn net.Conn, data []byte) {
	t := conn.(*transport.Conn)
	err := t.Data.(*sessionImpl).Input(conn, data)

	if err != nil {
		log.Sugar.Errorf("处理rtmp包失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())

		_ = conn.Close()
		t.Data.(*sessionImpl).Close()
		t.Data = nil
	}
}

func (s *serverImpl) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("rtmp断开连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data.(*sessionImpl).Close()
	t.Data = nil
}
