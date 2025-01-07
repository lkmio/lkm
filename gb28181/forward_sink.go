package gb28181

import (
	"github.com/lkmio/avformat/librtp"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
)

const (
	TcpStreamForwardBufferBlockSize = 1024
	RTPOverTCPPacketSize            = 1600
)

type ForwardSink struct {
	stream.BaseSink
	setup  SetupType
	socket transport.ITransport
	ssrc   uint32
}

func (f *ForwardSink) OnConnected(conn net.Conn) []byte {
	log.Sugar.Infof("级联连接 conn: %s", conn.RemoteAddr())

	f.Conn = conn
	f.Conn.(*transport.Conn).EnableAsyncWriteMode(TcpStreamForwardBufferBlockSize - 2)
	return nil
}

func (f *ForwardSink) OnPacket(conn net.Conn, data []byte) []byte {
	return nil
}

func (f *ForwardSink) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Infof("级联断开连接 conn: %s", conn.RemoteAddr())

	f.Close()
}

func (f *ForwardSink) Write(index int, data [][]byte, ts int64) error {
	if SetupUDP != f.setup && f.Conn == nil {
		return nil
	}

	if len(data)+2 > RTPOverTCPPacketSize {
		log.Sugar.Errorf("国标级联转发流失败 rtp包过长, 长度：%d, 最大允许：%d", len(data), RTPOverTCPPacketSize)
		return nil
	}

	// 修改为与上级协商的SSRC
	librtp.ModifySSRC(data[0], f.ssrc)

	if SetupUDP == f.setup {
		f.socket.(*transport.UDPClient).Write(data[0][2:])
	} else {
		if _, err := f.Conn.Write(data[0]); err != nil {
			return err
		}
	}

	return nil
}

// Close 关闭国标转发流
func (f *ForwardSink) Close() {
	f.BaseSink.Close()

	if f.socket != nil {
		f.socket.Close()
	}
}

// NewForwardSink 创建国标级联转发流Sink
// 返回监听的端口和Sink
func NewForwardSink(ssrc uint32, serverAddr string, setup SetupType, sinkId stream.SinkID, sourceId string) (stream.Sink, int, error) {
	sink := &ForwardSink{BaseSink: stream.BaseSink{ID: sinkId, SourceID: sourceId, State: stream.SessionStateCreated, Protocol: stream.TransStreamGBStreamForward}, ssrc: ssrc, setup: setup}

	if SetupUDP == setup {
		remoteAddr, err := net.ResolveUDPAddr("udp", serverAddr)
		if err != nil {
			return nil, 0, err
		}

		client, err := TransportManger.NewUDPClient(stream.AppConfig.ListenIP, remoteAddr)
		if err != nil {
			return nil, 0, err
		}

		sink.socket = client
	} else if SetupActive == setup {
		server, err := TransportManger.NewTCPServer(stream.AppConfig.ListenIP)
		if err != nil {
			return nil, 0, err
		}

		sink.TCPStreaming = true
		sink.socket = server
	} else if SetupPassive == setup {
		client := transport.TCPClient{}
		err := TransportManger.AllocPort(true, func(port uint16) error {
			localAddr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(int(port)))
			if err != nil {
				return err
			}

			remoteAddr, err := net.ResolveTCPAddr("tcp", serverAddr)
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

		sink.TCPStreaming = true
		sink.socket = &client
	} else {
		utils.Assert(false)
	}

	return sink, sink.socket.ListenPort(), nil
}
