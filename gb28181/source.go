package gb28181

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/mpeg"
	"github.com/lkmio/transport"
	"github.com/pion/rtp"
	"math"
	"strings"
)

type SetupType int

const (
	SetupUDP     = SetupType(0)
	SetupPassive = SetupType(1)
	SetupActive  = SetupType(2)

	PsProbeBufferSize = 1024 * 1024 * 2
)

func (s SetupType) TransportType() stream.TransportType {
	switch s {
	case SetupUDP:
		return stream.TransportTypeUDP
	case SetupPassive:
		return stream.TransportTypeTCPServer
	case SetupActive:
		return stream.TransportTypeTCPClient
	default:
		panic(fmt.Errorf("invalid setup type: %d", s))
	}
}

func (s SetupType) String() string {
	switch s {
	case SetupUDP:
		return "udp"
	case SetupPassive:
		return "passive"
	case SetupActive:
		return "active"
	default:
		panic(fmt.Errorf("invalid setup type: %d", s))
	}
}

func SetupTypeFromString(setupType string) SetupType {
	switch setupType {
	case "passive":
		return SetupPassive
	case "active":
		return SetupActive
	default:
		return SetupUDP
	}
}

var (
	TransportManger transport.Manager
)

// GBSource GB28181推流Source, 统一解析PS流、级联转发.
type GBSource interface {
	stream.Source

	SetupType() SetupType

	SetSSRC(ssrc uint32)

	SSRC() uint32

	ProcessPacket(data []byte) error

	SetTransport(transport transport.Transport)
}

type BaseGBSource struct {
	stream.PublishSource

	probeBuffer *mpeg.PSProbeBuffer
	transport   transport.Transport
	ssrc        uint32

	audioTimestamp         int64
	videoTimestamp         int64
	audioPacketCreatedTime int64
	videoPacketCreatedTime int64
	isSystemClock          bool // 推流时间戳不正确, 是否使用系统时间.
	lastRtpTimestamp       int64
	sameTimePackets        [][]byte
}

// ProcessPacket 输入rtp包, 处理PS流, 负责解析->封装->推流
func (source *BaseGBSource) ProcessPacket(data []byte) error {
	packet := rtp.Packet{}
	_ = packet.Unmarshal(data)

	// 收到第一包, 初始化
	if source.probeBuffer == nil {
		source.InitializePublish(packet.SSRC)
	}

	// 国标级联转发
	if source.GetTransStreamPublisher().GetForwardTransStream() != nil {
		if source.lastRtpTimestamp == -1 {
			source.lastRtpTimestamp = int64(packet.Timestamp)
		}

		// 相同时间戳的RTP包, 积攒一起发送, 降低管道压力
		length := len(data)
		if int64(packet.Timestamp) != source.lastRtpTimestamp {
			source.lastRtpTimestamp = int64(packet.Timestamp)
			if len(source.sameTimePackets) > 0 {
				source.GetTransStreamPublisher().Post(&stream.StreamEvent{Type: stream.StreamEventTypeRawPacket, Data: source.sameTimePackets})
				source.sameTimePackets = nil
			}
		}

		if stream.UDPReceiveBufferSize-2 < length {
			log.Sugar.Errorf("rtp包过大, 不转发. source: %s ssrc: %x size: %d", source.ID, source.ssrc, len(data))
		} else {
			bytes := stream.UDPReceiveBufferPool.Get().([]byte)
			copy(bytes[2:], data)
			binary.BigEndian.PutUint16(bytes[:2], uint16(length))
			source.sameTimePackets = append(source.sameTimePackets, bytes[:2+length])
		}
	}

	var bytes []byte
	var n int
	var err error
	bytes, err = source.probeBuffer.Input(packet.Payload)
	if err == nil {
		n, err = source.PublishSource.Input(bytes)
	}

	// 非解析缓冲区满的错误, 继续解析
	if err != nil {
		if strings.HasPrefix(err.Error(), "probe") {
			return err
		}

		log.Sugar.Errorf("解析ps流发生err: %s source: %s", err.Error(), source.GetID())
	}

	source.probeBuffer.Reset(n)
	return nil
}

// 纠正国标推流的时间戳
func (source *BaseGBSource) correctTimestamp(packet *avformat.AVPacket, dts, pts int64) {
	// dts和pts保持一致
	pts = int64(math.Max(float64(dts), float64(pts)))
	dts = pts
	packet.Pts = pts
	packet.Dts = dts

	var lastTimestamp int64
	var lastCreatedTime int64
	if utils.AVMediaTypeAudio == packet.MediaType {
		lastTimestamp = source.audioTimestamp
		lastCreatedTime = source.audioPacketCreatedTime
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		lastTimestamp = source.videoTimestamp
		lastCreatedTime = source.videoPacketCreatedTime
	}

	// 计算duration
	var duration int64
	if !source.isSystemClock && lastTimestamp != -1 {
		if pts < lastTimestamp {
			duration = 0x1FFFFFFFF - lastTimestamp + pts
			if duration < 90000 {
				// 处理正常溢出
				packet.Duration = duration
			} else {
				// 时间戳不正确
				log.Sugar.Errorf("推流时间戳不正确, 使用系统时钟. source: %s ssrc: %x duration: %d", source.ID, source.ssrc, duration)
				source.isSystemClock = true
			}
		} else {
			duration = pts - lastTimestamp
		}

		packet.Duration = duration
		duration = packet.GetDuration(90000)
		if duration < 0 || duration < 750 {
			log.Sugar.Errorf("推流时间戳不正确, 使用系统时钟. ts: %d duration: %d source: %s ssrc: %x", pts, duration, source.ID, source.ssrc)
			source.isSystemClock = true
		}
	}

	// 纠正时间戳
	if source.isSystemClock && lastTimestamp != -1 {
		duration = (packet.CreatedTime - lastCreatedTime) * 90
		packet.Dts = lastTimestamp + duration
		packet.Pts = lastTimestamp + duration
		packet.Duration = duration
	}

	if utils.AVMediaTypeAudio == packet.MediaType {
		source.audioTimestamp = packet.Pts
		source.audioPacketCreatedTime = packet.CreatedTime
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		source.videoTimestamp = packet.Pts
		source.videoPacketCreatedTime = packet.CreatedTime
	}
}

func (source *BaseGBSource) Close() {
	log.Sugar.Infof("GB28181推流结束 ssrc: %d %s", source.ssrc, source.PublishSource.String())

	source.PublishSource.Close()

	// 加锁执行, 保证并发安全
	source.ExecuteWithDeleteLock(func() {
		if source.transport != nil {
			source.transport.Close()
			source.transport = nil
		}
	})
}

func (source *BaseGBSource) SetSSRC(ssrc uint32) {
	source.ssrc = ssrc
}

func (source *BaseGBSource) SSRC() uint32 {
	return source.ssrc
}

func (source *BaseGBSource) InitializePublish(ssrc uint32) {
	if source.ssrc != ssrc {
		log.Sugar.Warnf("创建source的ssrc与实际推流的ssrc不一致, 创建的ssrc: %x 实际推流的ssrc: %x source: %s", source.ssrc, ssrc, source.GetID())
	}

	// 初始化ps解复用器
	source.TransDemuxer.SetOnPreprocessPacketHandler(func(packet *avformat.AVPacket) {
		source.correctTimestamp(packet, packet.Dts, packet.Pts)
	})
	source.probeBuffer = mpeg.NewProbeBuffer(PsProbeBufferSize)
	source.lastRtpTimestamp = -1

	source.ssrc = ssrc
	source.audioTimestamp = -1
	source.videoTimestamp = -1
	source.audioPacketCreatedTime = -1
	source.videoPacketCreatedTime = -1

	p := stream.SourceManager.Find(source.GetID())
	if p == nil {
		log.Sugar.Errorf("GB28181推流失败, 未找到source: %s", source.GetID())
		source.Close()
		return
	}

	stream.PreparePublishSourceWithAsync(p, false)
}

func (source *BaseGBSource) Init() {
	// 创建ps解复用器
	source.TransDemuxer = mpeg.NewPSDemuxer(false)
	source.TransDemuxer.SetHandler(source)
	source.PublishSource.Init()
}

func (source *BaseGBSource) SetTransport(transport transport.Transport) {
	source.transport = transport
}

// NewGBSource 创建国标推流源, 返回监听的收流端口
func NewGBSource(id string, ssrc uint32, tcp bool, active bool) (GBSource, int, error) {
	var transportServer transport.Transport
	var source GBSource
	var port int
	var err error

	if active {
		source, port, err = NewActiveSource()
	} else if tcp {
		transportServer, err = TransportManger.NewTCPServer()
		if err != nil {
			return nil, 0, err
		}

		source = NewPassiveSource()
		transportServer.(*transport.TCPServer).SetHandler(source.(*PassiveSource))
		transportServer.(*transport.TCPServer).Accept()
		port = transportServer.ListenPort()
	} else {
		transportServer, err = TransportManger.NewUDPServer()
		if err != nil {
			return nil, 0, err
		}

		source = NewUDPSource()
		transportServer.(*transport.UDPServer).SetHandler(source.(*UDPSource))
		transportServer.(*transport.UDPServer).Receive()
		port = transportServer.ListenPort()
	}

	source.SetType(stream.SourceType28181)
	source.SetID(id)
	source.SetSSRC(ssrc)
	// 加锁保护一下, 防止初始化阶段, 调用关闭source接口, 发生并发安全问题
	source.ExecuteWithDeleteLock(func() {
		if err = stream.AddSource(source); err != nil {
			return
		}

		source.SetTransport(transportServer)
		source.Init()
	})

	// id冲突
	if err != nil {
		if transportServer != nil {
			transportServer.Close()
		}
		return nil, 0, err
	}

	stream.LoopEvent(source)
	return source, port, err
}
