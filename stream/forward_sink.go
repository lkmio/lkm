package stream

import (
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/transport"
	"net"
	"time"
)

type TransportType int

const (
	TransportTypeUDP TransportType = iota
	TransportTypeTCPClient
	TransportTypeTCPServer
)

func (t TransportType) String() string {
	switch t {
	case TransportTypeUDP:
		return "udp"
	case TransportTypeTCPClient:
		return "tcp_client"
	case TransportTypeTCPServer:
		return "tcp_server"
	default:
		panic("invalid transport type")
	}
}

type ForwardSink struct {
	BaseSink
	socket        transport.Transport
	transportType TransportType
	receiveTimer  *time.Timer
}

func (f *ForwardSink) OnConnected(conn net.Conn) []byte {
	log.Sugar.Infof("%s 连接 conn: %s", f.Protocol, conn.RemoteAddr())

	f.receiveTimer.Stop()

	// 如果f.Conn赋值后, 发送数据先于EnableAsyncWriteMode执行, 可能会panic
	// 所以保险一点, 放在主协程执行
	ExecuteSyncEventOnTransStreamPublisher(f.SourceID, func() {
		f.Conn = conn
		f.BaseSink.EnableAsyncWriteMode(512)
	})

	return nil
}

func (f *ForwardSink) OnPacket(conn net.Conn, data []byte) []byte {
	return nil
}

func (f *ForwardSink) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Infof("%s 断开连接 conn: %s", f.Protocol, conn.RemoteAddr())

	f.Close()
}

func (f *ForwardSink) Write(index int, data []*collections.ReferenceCounter[[]byte], ts int64, keyVideo bool) error {
	// TCP等待连接后再转发数据
	if TransportTypeUDP != f.transportType && f.Conn == nil {
		return nil
	}

	if TransportTypeUDP == f.transportType {
		for _, datum := range data {
			f.socket.(*transport.UDPClient).Write(datum.Get()[2:])
		}
	} else {
		return f.BaseSink.Write(index, data, ts, keyVideo)
	}

	return nil
}

// Close 关闭国标转发流
func (f *ForwardSink) Close() {
	f.BaseSink.Close()

	if f.socket != nil {
		f.socket.Close()
	}

	if f.receiveTimer != nil {
		f.receiveTimer.Stop()
	}
}

// StartReceiveTimer 启动tcp sever计时器, 如果计时器触发, 没有连接, 则关闭流
func (f *ForwardSink) StartReceiveTimer() {
	f.receiveTimer = time.AfterFunc(ForwardSinkWaitTimeout*time.Second, func() {
		if f.Conn == nil {
			log.Sugar.Infof("%s 等待连接超时, 关闭sink", f.Protocol)
			f.Close()
		}
	})
}

func NewForwardSink(transportType TransportType, protocol TransStreamProtocol, sinkId SinkID, sourceId string, addr string, manager transport.Manager) (*ForwardSink, int, error) {
	sink := &ForwardSink{
		BaseSink:      BaseSink{ID: sinkId, SourceID: sourceId, State: SessionStateCreated, Protocol: protocol},
		transportType: transportType,
	}

	if transportType == TransportTypeUDP {
		remoteAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, 0, err
		}

		client, err := manager.NewUDPClient(remoteAddr)
		if err != nil {
			return nil, 0, err
		}

		sink.socket = client
	} else if transportType == TransportTypeTCPClient {
		client := transport.TCPClient{}
		err := manager.AllocPort(true, func(port uint16) error {
			localAddr, err := net.ResolveTCPAddr("tcp", ListenAddr(int(port)))
			if err != nil {
				return err
			}

			remoteAddr, err := net.ResolveTCPAddr("tcp", addr)
			if err != nil {
				return err
			}

			client.SetHandler(sink)
			conn, err := client.Connect(localAddr, remoteAddr)
			if err != nil {
				return err
			}

			sink.Conn = conn
			return nil
		})

		if err != nil {
			return nil, 0, err
		}

		sink.socket = &client
	} else if transportType == TransportTypeTCPServer {
		tcpServer, err := manager.NewTCPServer()
		if err != nil {
			return nil, 0, err
		}

		tcpServer.SetHandler(sink)
		tcpServer.Accept()
		sink.socket = tcpServer
		sink.StartReceiveTimer()
	}

	return sink, sink.socket.ListenPort(), nil
}
