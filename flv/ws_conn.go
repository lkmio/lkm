package flv

import (
	"github.com/gorilla/websocket"
	"net"
	"time"
)

type WSConn struct {
	*websocket.Conn
}

func (w WSConn) Read(b []byte) (n int, err error) {
	panic("implement me")
}

func (w WSConn) Write(b []byte) (n int, err error) {
	var offset int
	for i := 2; i < len(b); i++ {
		if b[i-2] == 0x0D && b[i-1] == 0x0A {
			offset = i
			break
		}
	}

	return 0, w.WriteMessage(websocket.BinaryMessage, b[offset:len(b)-2])
}

func (w WSConn) SetDeadline(t time.Time) error {
	panic("implement me")
}

func NewWSConn(conn *websocket.Conn) net.Conn {
	return &WSConn{conn}
}
