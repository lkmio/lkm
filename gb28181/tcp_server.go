package gb28181

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/log"
	"net"
)

type TCPServer struct {
	tcp    *transport.TCPServer
	filter Filter
}

func (T *TCPServer) OnConnected(conn net.Conn) []byte {
	log.Sugar.Infof("GB28181连接 conn:%s", conn.RemoteAddr().String())

	con := conn.(*transport.Conn)
	session := NewTCPSession(conn, T.filter)
	con.Data = session

	//TCP使用ReceiveBuffer区别在于,多端口模式从第一包就使用ReceiveBuffer, 单端口模式先解析出ssrc, 找到source. 后续再使用ReceiveBuffer.
	if session.source != nil {
		return session.receiveBuffer.GetBlock()
	}
	return nil
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) []byte {
	session := conn.(*transport.Conn).Data.(*TCPSession)

	//单端口收流
	if session.source == nil {
		//直接传给解码器, 先根据ssrc找到source. 后续还是会直接传给source
		session.Input(data)
	} else {
		session.source.(*PassiveSource).PublishSource.Input(data)
	}

	if session.source != nil {
		return session.receiveBuffer.GetBlock()
	}
	return nil
}

func (T *TCPServer) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Infof("GB28181断开连接 conn:%s", conn.RemoteAddr().String())

	con := conn.(*transport.Conn)
	if con.Data != nil && con.Data.(*TCPSession).source != nil {
		con.Data.(*TCPSession).source.Close()
	}

	con.Data = nil
}

func NewTCPServer(addr net.Addr, filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	tcp := &transport.TCPServer{}
	tcp.SetHandler(server)
	if err := tcp.Bind(addr); err != nil {
		return server, err
	}

	server.tcp = tcp
	return server, nil
}
