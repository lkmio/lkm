package rtsp

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"net"
	"runtime"
)

type Server interface {
	Start(addr net.Addr) error

	Close()
}

func NewServer(password string) Server {
	return &server{
		handler: newHandler(password),
	}
}

type server struct {
	tcp     *transport.TCPServer
	handler *handler
}

func (s *server) Start(addr net.Addr) error {
	utils.Assert(s.tcp == nil)

	//监听TCP端口
	tcp := &transport.TCPServer{
		ReuseServer: transport.ReuseServer{
			EnableReuse:      true,
			ConcurrentNumber: runtime.NumCPU(),
		},
	}

	if err := tcp.Bind(addr); err != nil {
		return err
	}

	tcp.SetHandler(s)
	tcp.Accept()
	s.tcp = tcp
	return nil
}

func (s *server) closeSession(conn net.Conn) {
	t := conn.(*transport.Conn)
	if t.Data != nil {
		t.Data.(*session).close()
		t.Data = nil
	}
}

func (s *server) Close() {

}

func (s *server) OnConnected(conn net.Conn) []byte {
	log.Sugar.Debugf("rtsp连接 conn:%s", conn.RemoteAddr().String())

	t := conn.(*transport.Conn)
	t.Data = NewSession(conn)
	return nil
}

func (s *server) OnPacket(conn net.Conn, data []byte) []byte {
	t := conn.(*transport.Conn)

	method, url, header, err := parseMessage(data)
	if err != nil {
		log.Sugar.Errorf("failed to prase message:%s. err:%s conn:%s", string(data), err.Error(), conn.RemoteAddr().String())
		_ = conn.Close()
		return nil
	}

	err = s.handler.Process(t.Data.(*session), method, url, header)
	if err != nil {
		log.Sugar.Errorf("failed to process message of RTSP. err:%s conn:%s msg:%s", err.Error(), conn.RemoteAddr().String(), string(data))
		_ = conn.Close()
	}

	//后续实现rtsp推流, 需要返回收流buffer
	return nil
}

func (s *server) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("rtsp断开连接 conn:%s", conn.RemoteAddr().String())

	s.closeSession(conn)
}
