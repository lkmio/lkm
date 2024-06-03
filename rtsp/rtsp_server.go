package rtsp

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/log"
	"net"
)

type IServer interface {
	Start(addr net.Addr) error

	Close()
}

func NewServer(password string) IServer {
	return &serverImpl{
		handler: newHandler(password),
	}
}

type serverImpl struct {
	tcp     *transport.TCPServer
	handler *handler
}

func (s *serverImpl) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	//监听TCP端口
	server := &transport.TCPServer{}
	server.SetHandler(s)
	err := server.Bind(addr)
	if err != nil {
		return err
	}

	s.tcp = server
	return nil
}

func (s *serverImpl) closeSession(conn net.Conn) {
	t := conn.(*transport.Conn)
	if t.Data != nil {
		t.Data.(*session).close()
		t.Data = nil
	}
}

func (s *serverImpl) Close() {

}

func (s *serverImpl) OnConnected(conn net.Conn) {
	log.Sugar.Debugf("rtsp连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
}

func (s *serverImpl) OnPacket(conn net.Conn, data []byte) {
	t := conn.(*transport.Conn)

	method, url, header, err := parseMessage(data)
	if err != nil {
		log.Sugar.Errorf("failed to prase message:%s. err:%s conn:%s", string(data), err.Error(), conn.RemoteAddr().String())
		_ = conn.Close()
		return
	}

	err = s.handler.Process(t.Data.(*session), method, url, header)
	if err != nil {
		log.Sugar.Errorf("failed to process message of RTSP. err:%s conn:%s msg:%s", err.Error(), conn.RemoteAddr().String(), string(data))
		_ = conn.Close()
	}
}

func (s *serverImpl) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("rtsp断开连接 conn:%s", conn.RemoteAddr().String())

	s.closeSession(conn)
}
