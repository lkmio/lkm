package stream

import (
	"encoding/binary"
	"fmt"
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

// ForwardSink 转发流Sink, 级联/对讲广播/JT1078转GB28181均使用
type ForwardSink struct {
	BaseSink
	socket           transport.Transport
	spareSocket      transport.Transport // 对讲备选udp发送
	transportType    TransportType
	receiveTimer     *time.Timer
	ssrc             uint32
	requireSSRCMatch bool // 如果ssrc要求一致, 发包时要检查ssrc是否一致, 不一致则重新拷贝一份
	rtpBuffer        *RtpBuffer
}

func (f *ForwardSink) OnConnected(conn net.Conn) []byte {
	log.Sugar.Infof("%s 连接 conn: %s", f.Protocol, conn.RemoteAddr())

	if f.receiveTimer != nil {
		f.receiveTimer.Stop()
	}

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
	if TransportTypeUDP != f.transportType && f.Conn == nil && f.spareSocket == nil {
		return nil
	}

	var processedData []*collections.ReferenceCounter[[]byte]

	// ssrc不一致, 重新拷贝一份, 修改为指定的ssrc
	if f.requireSSRCMatch && f.ssrc != binary.BigEndian.Uint32(data[0].Get()[2+8:]) {
		if TransportTypeUDP != f.transportType {
			if f.rtpBuffer == nil {
				f.rtpBuffer = NewRtpBuffer(1024)
			}

		} else if f.rtpBuffer == nil {
			f.rtpBuffer = NewRtpBuffer(1)
		}

		for _, datum := range data {
			src := datum.Get()
			counter := f.rtpBuffer.Get()
			bytes := counter.Get()

			length := len(src)
			copy(bytes, src[:length])

			// 修改ssrc
			binary.BigEndian.PutUint32(bytes[2+8:], f.ssrc)

			// UDP直接发送
			if TransportTypeUDP == f.transportType {
				_ = f.socket.(*transport.UDPClient).Write(bytes[2:length])
			} else {
				counter.ResetData(bytes[:length])
				counter.Refer()
				processedData = append(processedData, counter)
			}
		}

		// UDP已经发送, 直接返回
		if processedData == nil {
			return nil
		} else {
			// 引用计数保持为1
			for _, pkt := range processedData {
				pkt.Release()
			}
		}
	}

	if processedData == nil {
		processedData = data
	}

	spare := f.Conn == nil && f.spareSocket != nil
	if TransportTypeUDP == f.transportType || spare {
		var sender = f.socket
		if spare {
			sender = f.spareSocket
		}

		for _, datum := range processedData {
			sender.(*transport.UDPClient).Write(datum.Get()[2:])
		}
	} else {
		return f.BaseSink.Write(index, processedData, ts, keyVideo)
	}

	return nil
}

// Close 关闭转发流
func (f *ForwardSink) Close() {
	f.BaseSink.Close()

	if f.socket != nil {
		f.socket.Close()
	}

	if f.spareSocket != nil {
		f.spareSocket.Close()
	}

	if f.receiveTimer != nil {
		f.receiveTimer.Stop()
	}

	if f.rtpBuffer != nil {
		f.rtpBuffer.Clear()
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

func (f *ForwardSink) GetSSRC() uint32 {
	return f.ssrc
}

func NewForwardSink(transportType TransportType, protocol TransStreamProtocol, sinkId SinkID, sourceId string, addr string, manager transport.Manager, ssrc uint32) (*ForwardSink, int, error) {
	sink := &ForwardSink{
		BaseSink:         BaseSink{ID: sinkId, SourceID: sourceId, State: SessionStateCreated, Protocol: protocol},
		transportType:    transportType,
		ssrc:             ssrc,
		requireSSRCMatch: false, // 默认允许ssrc可以不一致
	}

	if transportType == TransportTypeUDP {
		remoteAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, 0, err
		} else if client, err := manager.NewUDPClient(remoteAddr); err != nil {
			return nil, 0, err
		} else {
			sink.socket = client
		}

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

		// 同时创建udp发送器, 兼容不支持tcp对讲的设备
		if TransStreamGBTalk == protocol {
			localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", AppConfig.ListenIP, tcpServer.ListenPort()))
			remoteAddr, err := net.ResolveUDPAddr("udp", addr)

			udp := &transport.UDPClient{}
			err = udp.Connect(localAddr, remoteAddr)
			if err == nil {
				sink.spareSocket = udp
			}
		} else {
			sink.StartReceiveTimer()
		}
	}

	return sink, sink.socket.ListenPort(), nil
}
