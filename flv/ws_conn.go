package flv

import (
	"github.com/gorilla/websocket"
	"github.com/lkmio/avformat/transport"
	"net"
	"time"
)

type WSConn struct {
	*websocket.Conn
}

func (w WSConn) Read(b []byte) (n int, err error) {
	panic("implement me")
}

func (w WSConn) Write(block []byte) (n int, err error) {
	// ws-flv负载的时flv tag
	return 0, w.WriteMessage(websocket.BinaryMessage, GetFLVTag(block))
}

func (w WSConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func NewWSConn(conn *websocket.Conn) net.Conn {
	return transport.NewConn(&WSConn{conn})
}
