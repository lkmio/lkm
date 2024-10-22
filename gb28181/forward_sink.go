package gb28181

import (
	"encoding/binary"
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
	buffer *stream.ReceiveBuffer //发送缓冲区
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

func (f *ForwardSink) Input(data []byte) error {
	if SetupUDP != f.setup && f.Conn == nil {
		return nil
	}

	if len(data)+2 > RTPOverTCPPacketSize {
		log.Sugar.Errorf("国标级联转发流失败 rtp包过长, 长度：%d, 最大允许：%d", len(data), RTPOverTCPPacketSize)
		return nil
	}

	// 修改为与上级协商的SSRC
	librtp.ModifySSRC(data, f.ssrc)

	if SetupUDP == f.setup {
		// UDP转发, 不拷贝直接发送
		f.socket.(*transport.UDPClient).Write(data)
	} else {
		// TCP转发, 拷贝一次再发送
		block := f.buffer.GetBlock()
		copy(block[2:], data)
		binary.BigEndian.PutUint16(block, uint16(len(data)))

		if _, err := f.Conn.Write(block[:2+len(data)]); err == nil {
			return nil
		} else if _, ok := err.(*transport.ZeroWindowSizeError); ok {
			log.Sugar.Errorf("发送缓冲区阻塞")
			f.Conn.Close()
			f.Conn = nil
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
	sink := &ForwardSink{BaseSink: stream.BaseSink{ID: sinkId, SourceID: sourceId, Protocol: stream.TransStreamGBStreamForward}, ssrc: ssrc, setup: setup}

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

		sink.socket = &client
	} else {
		utils.Assert(false)
	}

	if SetupUDP != setup {
		sink.buffer = stream.NewReceiveBuffer(RTPOverTCPPacketSize, TcpStreamForwardBufferBlockSize)
	}

	return sink, sink.socket.ListenPort(), nil
}
