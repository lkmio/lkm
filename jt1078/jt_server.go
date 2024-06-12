package jt1078

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"net"
)

type Server interface {
	Start(addr net.Addr) error

	Close()
}

type jtServer struct {
	tcp *transport.TCPServer
}

func NewServer() Server {
	return &jtServer{}
}

func (s jtServer) OnConnected(conn net.Conn) {
	log.Sugar.Debugf("jtserver连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
}

func (s jtServer) OnPacket(conn net.Conn, data []byte) {
	conn.(*transport.Conn).Data.(*Session).Input(data)
}

func (s jtServer) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("jtserver断开连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	utils.Assert(t.Data != nil)
	t.Data.(*Session).Close()
	t.Data = nil
}

func (s jtServer) Start(addr net.Addr) error {
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

func (s jtServer) Close() {
	panic("implement me")
}
