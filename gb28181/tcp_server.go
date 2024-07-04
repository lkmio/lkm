package gb28181

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

// TCPServer GB28181TCP被动收流
type TCPServer struct {
	stream.StreamServer[*TCPSession]

	tcp    *transport.TCPServer
	filter Filter
}

func (T *TCPServer) OnNewSession(conn net.Conn) *TCPSession {
	return NewTCPSession(conn, T.filter)
}

func (T *TCPServer) OnCloseSession(session *TCPSession) {
	session.Close()

	if session.source != nil {
		T.filter.RemoveSource(session.source.SSRC())
	}

	if stream.AppConfig.GB28181.IsMultiPort() {
		T.tcp.Close()
		T.Handler = nil
	}
}

func (T *TCPServer) OnConnected(conn net.Conn) []byte {
	T.StreamServer.OnConnected(conn)

	//TCP使用ReceiveBuffer区别在于,多端口模式从第一包就使用ReceiveBuffer, 单端口模式先解析出ssrc, 找到source. 后续再使用ReceiveBuffer.
	if conn.(*transport.Conn).Data.(*TCPSession).source != nil {
		return conn.(*transport.Conn).Data.(*TCPSession).receiveBuffer.GetBlock()
	}

	return nil
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) []byte {
	T.StreamServer.OnPacket(conn, data)
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

func NewTCPServer(addr net.Addr, filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	server.StreamServer = stream.StreamServer[*TCPSession]{
		SourceType: stream.SourceType28181,
		Handler:    server,
	}

	tcp := &transport.TCPServer{}
	tcp.SetHandler(server)
	if err := tcp.Bind(addr); err != nil {
		return server, err
	}

	server.tcp = tcp
	return server, nil
}
