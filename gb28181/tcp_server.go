package gb28181

import (
	"encoding/hex"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
	"runtime"
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
	return conn.(*transport.Conn).Data.(*TCPSession).receiveBuffer
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) []byte {
	T.StreamServer.OnPacket(conn, data)
	session := conn.(*transport.Conn).Data.(*TCPSession)

	err := session.DecodeGBRTPOverTCPPacket(data, T.filter, conn)
	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err: %s conn: %s data: %s", err.Error(), conn.RemoteAddr().String(), hex.EncodeToString(data))
		_ = conn.Close()
		return nil
	}

	return session.receiveBuffer
}

func NewTCPServer(filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	var tcp *transport.TCPServer
	var err error
	if stream.AppConfig.GB28181.IsMultiPort() {
		tcp = &transport.TCPServer{}
		tcp, err = TransportManger.NewTCPServer()
		if err != nil {
			return nil, err
		}

	} else {
		tcp = &transport.TCPServer{
			ReuseServer: transport.ReuseServer{
				EnableReuse:      true,
				ConcurrentNumber: runtime.NumCPU(),
			},
		}

		var gbAddr *net.TCPAddr
		gbAddr, err = net.ResolveTCPAddr("tcp", stream.ListenAddr(stream.AppConfig.GB28181.Port[0]))
		if err != nil {
			return nil, err
		}

		if err = tcp.Bind(gbAddr); err != nil {
			return server, err
		}
	}

	tcp.SetHandler(server)
	tcp.Accept()
	server.tcp = tcp
	server.StreamServer = stream.StreamServer[*TCPSession]{
		SourceType: stream.SourceType28181,
		Handler:    server,
	}
	return server, nil
}
