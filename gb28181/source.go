package gb28181

import (
	"fmt"
	"github.com/lkmio/avformat/libmpeg"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/rtp"
	"math"
	"net"
)

type SetupType int

const (
	SetupUDP     = SetupType(0)
	SetupPassive = SetupType(1)
	SetupActive  = SetupType(2)

	PsProbeBufferSize = 1024 * 1024 * 2
	JitterBufferSize  = 1024 * 1024
)

var (
	TransportManger transport.Manager
	SharedUDPServer *UDPServer
	SharedTCPServer *TCPServer
)

// GBSource GB28181推流Source, 统一解析PS流、级联转发.
type GBSource interface {
	stream.Source

	SetupType() SetupType

	// PreparePublish 收到流时, 做一些初始化工作.
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

	audioTimestamp         int64
	videoTimestamp         int64
	audioPacketCreatedTime int64
	videoPacketCreatedTime int64
	isSystemClock          bool // 推流时间戳不正确, 是否使用系统时间.
}

func (source *BaseGBSource) Init(receiveQueueSize int) {
	source.deMuxerCtx = libmpeg.NewPSDeMuxerContext(make([]byte, PsProbeBufferSize))
	source.deMuxerCtx.SetHandler(source)
	source.SetType(stream.SourceType28181)
	source.PublishSource.Init(receiveQueueSize)
}

// Input 输入rtp包, 处理PS流, 负责解析->封装->推流. 所有GBSource, 均到此处处理, 在event协程调用此函数
func (source *BaseGBSource) Input(data []byte) error {
	// 国标级联转发
	for _, transStream := range source.TransStreams {
		if transStream.Protocol() != stream.TransStreamGBStreamForward {
			continue
		}

		transStream.(*ForwardStream).SendPacket(data)
	}

	packet := rtp.Packet{}
	_ = packet.Unmarshal(data)
	return source.deMuxerCtx.Input(packet.Payload)
}

// OnPartPacket 部分es流回调
func (source *BaseGBSource) OnPartPacket(index int, mediaType utils.AVMediaType, codec utils.AVCodecID, data []byte, first bool) {
	buffer := source.FindOrCreatePacketBuffer(index, mediaType)

	// 第一个es包, 标记内存起始位置
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
			log.Sugar.Errorf("添加track超时 source:%s", source.GetID())
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

	source.correctTimestamp(packet, dts, pts)
	source.OnDeMuxPacket(packet)
	return nil
}

// 纠正国标推流的时间戳
func (source *BaseGBSource) correctTimestamp(packet utils.AVPacket, dts, pts int64) {
	// dts和pts保持一致
	pts = int64(math.Max(float64(dts), float64(pts)))
	dts = pts
	packet.SetPts(pts)
	packet.SetDts(dts)

	var lastTimestamp int64
	var lastCreatedTime int64
	if utils.AVMediaTypeAudio == packet.MediaType() {
		lastTimestamp = source.audioTimestamp
		lastCreatedTime = source.audioPacketCreatedTime
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		lastTimestamp = source.videoTimestamp
		lastCreatedTime = source.videoPacketCreatedTime
	}

	// 计算duration
	var duration int64
	if !source.isSystemClock && lastTimestamp != -1 {
		if pts < lastTimestamp {
			duration = 0x1FFFFFFFF - lastTimestamp + pts
			if duration < 90000 {
				//处理正常溢出
				packet.SetDuration(duration)
			} else {
				//时间戳不正确
				log.Sugar.Errorf("推流时间戳不正确, 使用系统时钟. ssrc:%d", source.ssrc)
				source.isSystemClock = true
			}
		} else {
			duration = pts - lastTimestamp
		}

		packet.SetDuration(duration)
		duration = packet.Duration(90000)
		if duration < 0 || duration < 750 {
			log.Sugar.Errorf("推流时间戳不正确, 使用系统时钟. ssrc:%d", source.ssrc)
			source.isSystemClock = true
		}
	}

	// 纠正时间戳
	if source.isSystemClock && lastTimestamp != -1 {
		duration = (packet.CreatedTime() - lastCreatedTime) * 90
		packet.SetDts(lastTimestamp + duration)
		packet.SetPts(lastTimestamp + duration)
		packet.SetDuration(duration)
	}

	if utils.AVMediaTypeAudio == packet.MediaType() {
		source.audioTimestamp = packet.Pts()
		source.audioPacketCreatedTime = packet.CreatedTime()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		source.videoTimestamp = packet.Pts()
		source.videoPacketCreatedTime = packet.CreatedTime()
	}
}

func (source *BaseGBSource) Close() {
	log.Sugar.Infof("GB28181推流结束 ssrc:%d %s", source.ssrc, source.PublishSource.String())

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
	source.audioTimestamp = -1
	source.videoTimestamp = -1
	source.audioPacketCreatedTime = -1
	source.videoPacketCreatedTime = -1

	if stream.AppConfig.Hooks.IsEnablePublishEvent() {
		go func() {
			if _, state := stream.HookPublishEvent(source_); utils.HookStateOK == state {
				return
			}

			log.Sugar.Errorf("GB28181 推流失败 source:%s", source.GetID())
			if conn != nil {
				conn.Close()
			}
		}()
	}
}

// NewGBSource 创建国标推流源, 返回监听的收流端口
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

	// 单端口模式，绑定ssrc
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
		// 多端口模式, 创建收流Server
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

	source.SetID(id)
	source.SetSSRC(ssrc)
	source.Init(bufferBlockCount)
	if _, state := stream.PreparePublishSource(source, false); utils.HookStateOK != state {
		return nil, 0, fmt.Errorf("error code %d", state)
	}

	go stream.LoopEvent(source)
	return source, port, err
}
