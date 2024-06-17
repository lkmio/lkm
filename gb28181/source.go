package gb28181

import (
	"encoding/hex"
	"fmt"
	"github.com/pion/rtp"
	"github.com/yangjiechina/avformat/libavc"
	"github.com/yangjiechina/avformat/libhevc"
	"github.com/yangjiechina/avformat/libmpeg"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
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
	TransportManger stream.TransportManager
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
	defer func() {
		if packet == nil {
			buffer.FreeTail()
		}
	}()

	if utils.AVCodecIdH264 == codec {
		//从关键帧中解析出sps和pps
		if source.videoStream == nil {
			sps, pps, err := libavc.ParseExtraDataFromKeyNALU(data)
			if err != nil {
				log.Sugar.Errorf("从关键帧中解析sps pps失败 source:%s data:%s", source.Id_, hex.EncodeToString(data))
				return err
			}

			codecData, err := utils.NewAVCCodecData(sps, pps)
			if err != nil {
				log.Sugar.Errorf("解析sps pps失败 source:%s data:%s sps:%s, pps:%s", source.Id_, hex.EncodeToString(data), hex.EncodeToString(sps), hex.EncodeToString(pps))
				return err
			}

			source.videoStream = utils.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.Record(), codecData)
			stream_ = source.videoStream
		}

		packet = utils.NewVideoPacket(data, dts, pts, key, utils.PacketTypeAnnexB, codec, index, 90000)
	} else if utils.AVCodecIdH265 == codec {
		if source.videoStream == nil {
			vps, sps, pps, err := libhevc.ParseExtraDataFromKeyNALU(data)
			if err != nil {
				log.Sugar.Errorf("从关键帧中解析vps sps pps失败 source:%s data:%s", source.Id_, hex.EncodeToString(data))
				return err
			}

			codecData, err := utils.NewHevcCodecData(vps, sps, pps)
			if err != nil {
				log.Sugar.Errorf("解析sps pps失败 source:%s data:%s vps:%s sps:%s, pps:%s", source.Id_, hex.EncodeToString(data), hex.EncodeToString(vps), hex.EncodeToString(sps), hex.EncodeToString(pps))
				return err
			}

			source.videoStream = utils.NewAVStream(utils.AVMediaTypeVideo, 0, codec, codecData.Record(), codecData)
			stream_ = source.videoStream
		}

		packet = utils.NewVideoPacket(data, dts, pts, key, utils.PacketTypeAnnexB, codec, index, 90000)
	} else if utils.AVCodecIdAAC == codec {
		//必须包含ADTSHeader
		if len(data) < 7 {
			log.Sugar.Warnf("need more data...")
			return nil
		}

		var skip int
		header, err := utils.ReadADtsFixedHeader(data)
		if err != nil {
			log.Sugar.Errorf("读取ADTSHeader失败 suorce:%s data:%s", source.Id_, hex.EncodeToString(data[:7]))
			return nil
		} else {
			skip = 7
			//跳过ADtsHeader长度
			if header.ProtectionAbsent() == 0 {
				skip += 2
			}
		}

		if source.audioStream == nil {
			if source.IsCompleted() {
				return nil
			}

			configData, err := utils.ADtsHeader2MpegAudioConfigData(header)
			config, err := utils.ParseMpeg4AudioConfig(configData)
			println(config)
			if err != nil {
				log.Sugar.Errorf("adt头转m4ac失败 suorce:%s data:%s", source.Id_, hex.EncodeToString(data[:7]))
				return nil
			}

			source.audioStream = utils.NewAVStream(utils.AVMediaTypeAudio, index, codec, configData, nil)
			stream_ = source.audioStream
		}

		packet = utils.NewAudioPacket(data[skip:], dts, pts, codec, index, 90000)
	} else if utils.AVCodecIdPCMALAW == codec || utils.AVCodecIdPCMMULAW == codec {
		if source.audioStream == nil {
			source.audioStream = utils.NewAVStream(utils.AVMediaTypeAudio, index, codec, nil, nil)
			stream_ = source.audioStream
		}

		packet = utils.NewAudioPacket(data, dts, pts, codec, index, 90000)
	} else {
		log.Sugar.Errorf("the codec %d is not implemented.", codec)
		return nil
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

func (source *BaseGBSource) PreparePublish(conn net.Conn, ssrc uint32, source_ GBSource) {
	source.SetConn(conn)
	source.SetSSRC(ssrc)
	source.SetState(stream.SessionStateTransferring)

	if stream.AppConfig.Hook.EnablePublishEvent() {
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
func NewGBSource(id string, ssrc uint32, tcp bool, active bool) (GBSource, uint16, error) {
	if tcp {
		utils.Assert(stream.AppConfig.GB28181.EnableTCP())
	} else {
		utils.Assert(stream.AppConfig.GB28181.EnableUDP())
	}

	if active {
		utils.Assert(tcp && stream.AppConfig.GB28181.EnableTCP() && stream.AppConfig.GB28181.IsMultiPort())
	}

	var source GBSource
	var port uint16
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
			err := TransportManger.AllocTransport(true, func(port_ uint16) error {

				addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", stream.AppConfig.GB28181.Addr, port_))
				server, err := NewTCPServer(addr, NewSingleFilter(source))
				if err != nil {

					return err
				}

				source.(*PassiveSource).transport = server.tcp
				port = port_
				return nil
			})

			if err != nil {
				return nil, 0, err
			}
		} else {
			err := TransportManger.AllocTransport(false, func(port_ uint16) error {

				addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", stream.AppConfig.GB28181.Addr, port_))
				server, err := NewUDPServer(addr, NewSingleFilter(source))
				if err != nil {
					return err
				}

				source.(*UDPSource).transport = server.udp
				port = port_
				return nil
			})

			if err != nil {
				return nil, 0, err
			}
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
