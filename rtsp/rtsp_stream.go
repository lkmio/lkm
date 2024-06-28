package rtsp

import (
	"encoding/binary"
	"fmt"
	"github.com/yangjiechina/avformat/libavc"
	"github.com/yangjiechina/avformat/librtp"
	"github.com/yangjiechina/avformat/librtsp/sdp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
	"net"
	"strconv"
)

const (
	OverTcpHeaderSize = 4
	OverTcpMagic      = 0x24
)

// rtsp传输流封装
// 低延迟是rtsp特性, 所以不考虑实现GOP缓存
type tranStream struct {
	stream.BaseTransStream
	addr      net.IPAddr
	addrType  string
	urlFormat string

	rtpTracks []*rtspTrack
	sdp       string
}

// rtpMuxer申请输出流内存的回调
// 无论是tcp/udp拉流, 均使用同一块内存, 并且给tcp预留4字节的包长.
func (t *tranStream) onAllocBuffer(params interface{}) []byte {
	return t.rtpTracks[params.(int)].buffer[OverTcpHeaderSize:]
}

// onRtpPacket 所有封装后的RTP流都将回调于此
func (t *tranStream) onRtpPacket(data []byte, timestamp uint32, params interface{}) {
	//params传递track索引
	index := params.(int)
	track := t.rtpTracks[index]

	//保存带有sps和ssp等编码信息的rtp包, 对所有sink通用
	if track.cache && track.extraDataBuffer == nil {
		bytes := make([]byte, OverTcpHeaderSize+len(data))
		copy(bytes[OverTcpHeaderSize:], data)

		track.tmpExtraDataBuffer = append(track.tmpExtraDataBuffer, bytes)
		t.overTCP(bytes, index)
		return
	}

	//将rtp包发送给各个sink
	for _, value := range t.Sinks {
		sink_ := value.(*sink)
		if !sink_.isConnected(index) {
			continue
		}

		//为刚刚连接上的sink, 发送sps和pps等rtp包
		if sink_.pktCount(index) < 1 && utils.AVMediaTypeVideo == track.mediaType {
			seq := binary.BigEndian.Uint16(data[2:])
			count := len(track.extraDataBuffer)

			for i, rtp := range track.extraDataBuffer {
				//回滚rtp包的序号
				librtp.RollbackSeq(rtp[OverTcpHeaderSize:], int(seq)-(count-i-1))
				if sink_.tcp {
					sink_.input(index, rtp, 0)
				} else {
					sink_.input(index, rtp[OverTcpHeaderSize:], timestamp)
				}
			}
		}

		end := OverTcpHeaderSize + len(data)
		t.overTCP(track.buffer[:end], index)

		//发送rtp包
		if sink_.tcp {
			sink_.input(index, track.buffer[:end], 0)
		} else {
			sink_.input(index, data, timestamp)
		}
	}
}

func (t *tranStream) overTCP(data []byte, channel int) {
	data[0] = OverTcpMagic
	data[1] = byte(channel)
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)-4))
}

func (t *tranStream) Input(packet utils.AVPacket) error {
	stream_ := t.rtpTracks[packet.Index()]
	if utils.AVMediaTypeAudio == packet.MediaType() {
		stream_.muxer.Input(packet.Data(), uint32(packet.ConvertPts(stream_.rate)))
	} else if utils.AVMediaTypeVideo == packet.MediaType() {

		//将sps和pps按照单一模式打包
		if stream_.extraDataBuffer == nil {
			if !packet.KeyFrame() {
				return nil
			}

			stream_.cache = true
			parameters := t.BaseTransStream.Tracks[packet.Index()].CodecParameters()

			if utils.AVCodecIdH265 == packet.CodecId() {
				bytes := parameters.(*utils.HEVCCodecData).VPS()
				stream_.muxer.Input(bytes[0], uint32(packet.ConvertPts(stream_.rate)))
			}

			spsBytes := parameters.SPS()
			ppsBytes := parameters.PPS()

			stream_.muxer.Input(spsBytes[0], uint32(packet.ConvertPts(stream_.rate)))
			stream_.muxer.Input(ppsBytes[0], uint32(packet.ConvertPts(stream_.rate)))
			stream_.extraDataBuffer = stream_.tmpExtraDataBuffer
		}

		data := libavc.RemoveStartCode(packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()]))
		stream_.muxer.Input(data, uint32(packet.ConvertPts(stream_.rate)))
	}

	return nil
}

func (t *tranStream) AddSink(sink_ stream.Sink) error {
	sink_.(*sink).setSenderCount(len(t.BaseTransStream.Tracks))
	if err := sink_.(*sink).SendHeader([]byte(t.sdp)); err != nil {
		return err
	}

	return t.BaseTransStream.AddSink(sink_)
}

func (t *tranStream) AddTrack(stream utils.AVStream) error {
	if err := t.BaseTransStream.AddTrack(stream); err != nil {
		return err
	}

	payloadType, ok := librtp.CodecIdPayloads[stream.CodecId()]
	if !ok {
		return fmt.Errorf("no payload type was found for codecid:%d", stream.CodecId())
	}

	//创建RTP封装
	var muxer librtp.Muxer
	if utils.AVCodecIdH264 == stream.CodecId() {
		muxer = librtp.NewH264Muxer(payloadType.Pt, 0, 0xFFFFFFFF)
	} else if utils.AVCodecIdH265 == stream.CodecId() {
		muxer = librtp.NewH265Muxer(payloadType.Pt, 0, 0xFFFFFFFF)
	} else if utils.AVCodecIdAAC == stream.CodecId() {
		muxer = librtp.NewAACMuxer(payloadType.Pt, 0, 0xFFFFFFFF)
	} else if utils.AVCodecIdPCMALAW == stream.CodecId() || utils.AVCodecIdPCMMULAW == stream.CodecId() {
		muxer = librtp.NewMuxer(payloadType.Pt, 0, 0xFFFFFFFF)
	}

	muxer.SetAllocHandler(t.onAllocBuffer)
	muxer.SetWriteHandler(t.onRtpPacket)

	t.rtpTracks = append(t.rtpTracks, NewRTSPTrack(muxer, byte(payloadType.Pt), payloadType.ClockRate, stream.Type()))
	muxer.SetParams(len(t.rtpTracks) - 1)
	return nil
}

func (t *tranStream) WriteHeader() error {
	description := sdp.SessionDescription{
		Version: 0,
		Origin: sdp.Origin{
			Username:       "-",
			SessionID:      0,
			SessionVersion: 0,
			NetworkType:    "IN",
			AddressType:    t.addrType,
			UnicastAddress: t.addr.IP.String(),
		},

		SessionName: "Stream",
		TimeDescriptions: []sdp.TimeDescription{{
			Timing: sdp.Timing{
				StartTime: 0,
				StopTime:  0,
			},
			RepeatTimes: nil,
		},
		},
	}

	for i, track := range t.Tracks {
		payloadType, _ := librtp.CodecIdPayloads[track.CodecId()]
		mediaDescription := sdp.MediaDescription{
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: t.addrType,
				Address:     &sdp.Address{Address: t.addr.IP.String()},
			},

			Attributes: []sdp.Attribute{
				sdp.NewAttribute("recvonly", ""),
				sdp.NewAttribute("control:"+fmt.Sprintf(t.urlFormat, i), ""),
				sdp.NewAttribute(fmt.Sprintf("rtpmap:%d %s/%d", payloadType.Pt, payloadType.Encoding, payloadType.ClockRate), ""),
			},
		}

		mediaDescription.MediaName.Protos = []string{"RTP", "AVP"}
		mediaDescription.MediaName.Formats = []string{strconv.Itoa(payloadType.Pt)}

		if utils.AVMediaTypeAudio == track.Type() {
			mediaDescription.MediaName.Media = "audio"

			if utils.AVCodecIdAAC == track.CodecId() {
				//[14496-3], [RFC6416] profile-level-id:
				//1 : Main Audio Profile Level 1
				//9 : Speech Audio Profile Level 1
				//15: High Quality Audio Profile Level 2
				//30: Natural Audio Profile Level 1
				//44: High Efficiency AAC Profile Level 2
				//48: High Efficiency AAC v2 Profile Level 2
				//55: Baseline MPEG Surround Profile (see ISO/IEC 23003-1) Level 3

				//[RFC5619]
				//a=fmtp:96 streamType=5; profile-level-id=44; mode=AAC-hbr; config=131
				//     056E598; sizeLength=13; indexLength=3; indexDeltaLength=3; constant
				//     Duration=2048; MPS-profile-level-id=55; MPS-config=F1B4CF920442029B
				//     501185B6DA00;
				//低比特率用sizelength=6;indexlength=2;indexdeltalength=2

				//[RFC3640]
				//mode=AAC-hbr
				fmtp := sdp.NewAttribute("fmtp:97 profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;", "")
				mediaDescription.Attributes = append(mediaDescription.Attributes, fmtp)
			}

		} else {
			mediaDescription.MediaName.Media = "video"
		}

		description.MediaDescriptions = append(description.MediaDescriptions, &mediaDescription)
	}

	marshal, err := description.Marshal()
	if err != nil {
		return err
	}

	t.sdp = string(marshal)
	return nil
}

func NewTransStream(addr net.IPAddr, urlFormat string) stream.TransStream {
	t := &tranStream{
		addr:      addr,
		urlFormat: urlFormat,
	}

	if addr.IP.To4() != nil {
		t.addrType = "IP4"
	} else {
		t.addrType = "IP6"
	}

	return t
}

func TransStreamFactory(source stream.Source, protocol stream.Protocol, streams []utils.AVStream) (stream.TransStream, error) {
	trackFormat := "?track=%d"
	return NewTransStream(net.IPAddr{
		IP:   net.ParseIP(stream.AppConfig.PublicIP),
		Zone: "",
	}, trackFormat), nil
}
