package rtsp

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/log"
	"net"
	"net/textproto"
)

type IServer interface {
	Start(addr net.Addr) error

	Close()
}

func NewServer() IServer {
	return &serverImpl{
		publicHeader: "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN, PAUSE, GET_PARAMETER, SET_PARAMETER, REDIRECT, RECORD",
	}
}

type serverImpl struct {
	tcp *transport.TCPServer

	handlers     map[string]func(source string, headers textproto.MIMEHeader)
	publicHeader string
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
	for key, _ := range s.handlers {
		s.publicHeader += key + ", "
	}

	s.publicHeader = s.publicHeader[:len(s.publicHeader)-2]
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

	message, url, header, err := parseMessage(data)
	if err != nil {
		log.Sugar.Errorf("failed to prase message:%s. err:%s peer:%s", string(data), err.Error(), conn.RemoteAddr().String())
		_ = conn.Close()
		s.closeSession(conn)
		return
	}

	err = t.Data.(*session).Input(message, url, header)
	if err != nil {
		log.Sugar.Errorf("failed to process message of RTSP. err:%s peer:%s msg:%s", err.Error(), conn.RemoteAddr().String(), string(data))

		_ = conn.Close()
	}
}

func (s *serverImpl) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Debugf("rtsp断开连接 conn:%s", conn.RemoteAddr().String())

	s.closeSession(conn)
}
