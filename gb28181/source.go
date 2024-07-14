package gb28181

import (
	"fmt"
	"github.com/lkmio/avformat/libmpeg"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/rtp"
	"net"
)

type TransportType int

const (
	TransportTypeUDP        = TransportType(0)
	TransportTypeTCPPassive = TransportType(1)
	TransportTypeTCPActive  = TransportType(2)

	PsProbeBufferSize = 1024 * 1024 * 2
	JitterBufferSize  = 1024 * 1024
)

var (
	TransportManger transport.Manager
	SharedUDPServer *UDPServer
	SharedTCPServer *TCPServer
)

// GBSource GB28181推流Source, 接收PS流解析生成AVStream和AVPacket, 后续全权交给父类Source处理.
// udp/passive/active 都继承本接口, filter负责解析rtp包, 根据ssrc匹配对应的Source.
type GBSource interface {
	stream.Source

	InputRtp(pkt *rtp.Packet) error

	TransportType() TransportType

	PreparePublish(conn net.Conn, ssrc uint32, source GBSource)

	SetConn(conn net.Conn)

	SetSSRC(ssrc uint32)

	SSRC() uint32
}

type BaseGBSource struct {
	stream.PublishSource

	deMuxerCtx  *libmpeg.PSDeMuxerContext
	audioStream utils.AVStream
	videoStream utils.AVStream

	ssrc      uint32
	transport transport.ITransport
}

func (source *BaseGBSource) InputRtp(pkt *rtp.Packet) error {
	panic("implement me")
}

func (source *BaseGBSource) Transport() TransportType {
	panic("implement me")
}

func (source *BaseGBSource) Init(inputCB func(data []byte) error, closeCB func(), receiveQueueSize int) {
	source.deMuxerCtx = libmpeg.NewPSDeMuxerContext(make([]byte, PsProbeBufferSize))
	source.deMuxerCtx.SetHandler(source)
	source.SetType(stream.SourceType28181)
	source.PublishSource.Init(inputCB, closeCB, receiveQueueSize)
}

// Input 解析PS流, 确保在loop event协程调用此函数
func (source *BaseGBSource) Input(data []byte) error {
	return source.deMuxerCtx.Input(data)
}

// OnPartPacket 部分es流回调
func (source *BaseGBSource) OnPartPacket(index int, mediaType utils.AVMediaType, codec utils.AVCodecID, data []byte, first bool) {
	buffer := source.FindOrCreatePacketBuffer(index, mediaType)

	//第一个es包, 标记内存起始位置
	if first {
		buffer.Mark()
	}

	buffer.Write(data)
}

// OnLossPacket 非完整es包丢弃回调, 直接释放内存块
func (source *BaseGBSource) OnLossPacket(index int, mediaType utils.AVMediaType, codec utils.AVCodecID) {
	buffer := source.FindOrCreatePacketBuffer(index, mediaType)

	buffer.Fetch()
	buffer.FreeTail()
}

// OnCompletePacket 完整帧回调
func (source *BaseGBSource) OnCompletePacket(index int, mediaType utils.AVMediaType, codec utils.AVCodecID, dts int64, pts int64, key bool) error {
	buffer := source.FindOrCreatePacketBuffer(index, mediaType)
	data := buffer.Fetch()

	var packet utils.AVPacket
	var stream_ utils.AVStream
	var err error

	defer func() {
		if packet == nil {
			buffer.FreeTail()
		}
	}()

	if source.IsCompleted() && source.NotTrackAdded(index) {
		if !source.IsTimeoutTrack(index) {
			source.SetTimeoutTrack(index)
			log.Sugar.Errorf("添加track超时 source:%s", source.Id())
		}

		return nil
	}

	if utils.AVMediaTypeAudio == mediaType {
		stream_, packet, err = stream.ExtractAudioPacket(codec, source.audioStream == nil, data, pts, dts, index, 90000)
		if err != nil {
			return err
		}

		if stream_ != nil {
			source.audioStream = stream_
		}
	} else {
		if source.videoStream == nil && !key {
			log.Sugar.Errorf("skip non keyframes conn:%s", source.Conn.RemoteAddr())
			return nil
		}

		stream_, packet, err = stream.ExtractVideoPacket(codec, key, source.videoStream == nil, data, pts, dts, index, 90000)
		if err != nil {
			return err
		}
		if stream_ != nil {
			source.videoStream = stream_
		}
	}

	if stream_ != nil {
		source.OnDeMuxStream(stream_)
		if len(source.OriginStreams()) >= source.deMuxerCtx.TrackCount() {
			source.OnDeMuxStreamDone()
		}
	}

	source.OnDeMuxPacket(packet)
	return nil
}

func (source *BaseGBSource) Close() {
	log.Sugar.Infof("GB28181推流结束 ssrc:%d %s", source.ssrc, source.PublishSource.PrintInfo())

	//释放收流端口
	if source.transport != nil {
		source.transport.Close()
		source.transport = nil
	}

	//删除ssrc关联
	if !stream.AppConfig.GB28181.IsMultiPort() {
		if SharedTCPServer != nil {
			SharedTCPServer.filter.RemoveSource(source.ssrc)
		}

		if SharedUDPServer != nil {
			SharedUDPServer.filter.RemoveSource(source.ssrc)
		}
	}

	source.PublishSource.Close()
}

func (source *BaseGBSource) SetConn(conn net.Conn) {
	source.Conn = conn
}

func (source *BaseGBSource) SetSSRC(ssrc uint32) {
	source.ssrc = ssrc
}

func (source *BaseGBSource) SSRC() uint32 {
	return source.ssrc
}

func (source *BaseGBSource) PreparePublish(conn net.Conn, ssrc uint32, source_ GBSource) {
	source.SetConn(conn)
	source.SetSSRC(ssrc)
	source.SetState(stream.SessionStateTransferring)

	if stream.AppConfig.Hook.IsEnablePublishEvent() {
		go func() {
			_, state := stream.HookPublishEvent(source_)
			if utils.HookStateOK != state {
				log.Sugar.Errorf("GB28181 推流失败 source:%s", source.Id())

				if conn != nil {
					conn.Close()
				}
			}
		}()
	}
}

// NewGBSource 创建gb源,返回监听的收流端口
func NewGBSource(id string, ssrc uint32, tcp bool, active bool) (GBSource, int, error) {
	if tcp {
		utils.Assert(stream.AppConfig.GB28181.IsEnableTCP())
	} else {
		utils.Assert(stream.AppConfig.GB28181.IsEnableUDP())
	}

	if active {
		utils.Assert(tcp && stream.AppConfig.GB28181.IsEnableTCP() && stream.AppConfig.GB28181.IsMultiPort())
	}

	var source GBSource
	var port int
	var err error

	if active {
		source, port, err = NewActiveSource()
	} else if tcp {
		source = NewPassiveSource()
	} else {
		source = NewUDPSource()
	}

	if err != nil {
		return nil, 0, err
	}

	//单端口模式，绑定ssrc
	if !stream.AppConfig.GB28181.IsMultiPort() {
		var success bool
		if tcp {
			success = SharedTCPServer.filter.AddSource(ssrc, source)
		} else {
			success = SharedUDPServer.filter.AddSource(ssrc, source)
		}

		if !success {
			return nil, 0, fmt.Errorf("ssrc conflict")
		}

		port = stream.AppConfig.GB28181.Port[0]
	} else if !active {
		if tcp {
			tcpServer, err := NewTCPServer(NewSingleFilter(source))
			if err != nil {
				return nil, 0, err
			}

			port = tcpServer.tcp.ListenPort()
			source.(*PassiveSource).transport = tcpServer.tcp
		} else {
			server, err := NewUDPServer(NewSingleFilter(source))
			if err != nil {
				return nil, 0, err
			}

			port = server.udp.ListenPort()
			source.(*UDPSource).transport = server.udp
		}
	}

	var bufferBlockCount int
	if active || tcp {
		bufferBlockCount = stream.ReceiveBufferTCPBlockCount
	} else {
		bufferBlockCount = stream.ReceiveBufferUdpBlockCount
	}

	source.SetId(id)
	source.SetSSRC(ssrc)
	source.Init(source.Input, source.Close, bufferBlockCount)
	if _, state := stream.PreparePublishSource(source, false); utils.HookStateOK != state {
		return nil, 0, fmt.Errorf("error code %d", state)
	}

	go source.LoopEvent()
	return source, port, err
}
