package gb28181

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
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

	//TCP单端口收流, Session已经绑定Source, 使用ReceiveBuffer读取网络包
	if conn.(*transport.Conn).Data.(*TCPSession).source != nil {
		return conn.(*transport.Conn).Data.(*TCPSession).receiveBuffer.GetBlock()
	}

	return nil
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) []byte {
	T.StreamServer.OnPacket(conn, data)
	session := conn.(*transport.Conn).Data.(*TCPSession)

	// 单端口推流时, 先解析出SSRC找到GBSource. 后序将推流数据交给stream.Source处理
	if session.source == nil {
		if err := session.decoder.Input(data); err != nil {
			log.Sugar.Errorf("解析粘包数据失败 err: %s", err.Error())
			conn.Close()
		}

	} else {
		// 将流交给Source的主协程处理，主协程最终会调用PassiveSource的Input函数处理
		if session.source.SetupType() == SetupPassive {
			session.source.(*PassiveSource).PublishSource.Input(data)
		} else {
			session.source.(*ActiveSource).PublishSource.Input(data)
		}
	}

	// 绑定Source后, 使用ReceiveBuffer读取网络包, 减少拷贝
	if session.source != nil {
		return session.receiveBuffer.GetBlock()
	}

	return nil
}

func NewTCPServer(filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	var tcp *transport.TCPServer
	var err error
	if stream.AppConfig.GB28181.IsMultiPort() {
		tcp = &transport.TCPServer{}
		tcp, err = TransportManger.NewTCPServer(stream.AppConfig.ListenIP)
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
