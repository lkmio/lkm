package jt1078

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"net"
	"runtime"
)

type Server interface {
	Start(addr net.Addr) error

	Close()
}

type jtServer struct {
	stream.StreamServer[*Session]
	tcp *transport.TCPServer
}

func (s *jtServer) OnNewSession(conn net.Conn) *Session {
	return NewSession(conn)
}

func (s *jtServer) OnCloseSession(session *Session) {
	session.Close()
}

func (s *jtServer) OnPacket(conn net.Conn, data []byte) []byte {
	s.StreamServer.OnPacket(conn, data)
	session := conn.(*transport.Conn).Data.(*Session)
	session.PublishSource.Input(data)
	return session.receiveBuffer.GetBlock()
}

func (s *jtServer) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	server := &transport.TCPServer{
		ReuseServer: transport.ReuseServer{
			EnableReuse:      true,
			ConcurrentNumber: runtime.NumCPU(),
		},
	}
	if err := server.Bind(addr); err != nil {
		return err
	}

	server.SetHandler(s)
	server.Accept()
	s.tcp = server
	return nil
}

func (s *jtServer) Close() {
	panic("implement me")
}

func NewServer() Server {
	j := &jtServer{}
	j.StreamServer = stream.StreamServer[*Session]{
		SourceType: stream.SourceType1078,
		Handler:    j,
	}

	return j
}
