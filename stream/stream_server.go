package stream

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/log"
	"net"
)

type SessionHandler[T any] interface {
	OnNewSession(conn net.Conn) T

	OnCloseSession(session T)
}

type StreamServer[T any] struct {
	SourceType SourceType
	Handler    SessionHandler[T]
}

func (s *StreamServer[T]) OnConnected(conn net.Conn) []byte {
	log.Sugar.Debugf("%s连接 conn:%s", s.SourceType.ToString(), conn.RemoteAddr().String())
	conn.(*transport.Conn).Data = s.Handler.OnNewSession(conn)
	return nil
}

func (s *StreamServer[T]) OnPacket(conn net.Conn, data []byte) []byte {
	if AppConfig.Debug {
		DumpStream2File(s.SourceType, conn, data)
	}

	return nil
}

func (s *StreamServer[T]) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("%s断开连接 conn:%s", s.SourceType.ToString(), conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	if t.Data != nil {
		s.Handler.OnCloseSession(t.Data.(T))
		t.Data = nil
	}
}
