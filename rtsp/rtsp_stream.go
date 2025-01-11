package rtsp

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/libavc"
	"github.com/lkmio/avformat/librtp"
	"github.com/lkmio/avformat/librtsp/sdp"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"net"
	"strconv"
)

const (
	OverTcpHeaderSize = 4
	OverTcpMagic      = 0x24
)

// TransStream rtsp传输流封装
// 低延迟是rtsp特性, 所以不考虑实现GOP缓存
type TransStream struct {
	stream.BaseTransStream
	addr      net.IPAddr
	addrType  string
	urlFormat string

	RtspTracks []*Track
	//oldTracks  []*Track
	oldTracks map[byte]uint16
	sdp       string
	buffer    *stream.ReceiveBuffer // 保存封装后的rtp包
}

func (t *TransStream) OverTCP(data []byte, channel int) {
	data[0] = OverTcpMagic
	data[1] = byte(channel)
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)-4))
}

func (t *TransStream) Input(packet utils.AVPacket) ([][]byte, int64, bool, error) {
	t.ClearOutStreamBuffer()

	var ts uint32
	track := t.RtspTracks[packet.Index()]
	if utils.AVMediaTypeAudio == packet.MediaType() {
		ts = uint32(packet.ConvertPts(track.Rate))
		t.PackRtpPayload(track, packet.Index(), packet.Data(), ts)
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		ts = uint32(packet.ConvertPts(track.Rate))
		data := libavc.RemoveStartCode(packet.AnnexBPacketData(t.BaseTransStream.Tracks[packet.Index()].Stream))
		t.PackRtpPayload(track, packet.Index(), data, ts)
	}

	return t.OutBuffer[:t.OutBufferSize], int64(ts), utils.AVMediaTypeVideo == packet.MediaType() && packet.KeyFrame(), nil
}

func (t *TransStream) ReadExtraData(ts int64) ([][]byte, int64, error) {
	// 返回视频编码数据的rtp包
	for _, track := range t.RtspTracks {
		if utils.AVMediaTypeVideo != track.MediaType {
			continue
		}

		// 回滚序号和时间戳
		index := int(track.StartSeq) - len(track.ExtraDataBuffer)
		for i, bytes := range track.ExtraDataBuffer {
			librtp.RollbackSeq(bytes[OverTcpHeaderSize:], index+i+1)
			binary.BigEndian.PutUint32(bytes[OverTcpHeaderSize+4:], uint32(ts))
		}

		return track.ExtraDataBuffer, ts, nil
	}

	return nil, ts, nil
}

func (t *TransStream) PackRtpPayload(track *Track, channel int, data []byte, timestamp uint32) {
	var index int

	// 保存开始序号
	track.StartSeq = track.Muxer.GetHeader().Seq
	track.Muxer.Input(data, timestamp, func() []byte {
		index = t.buffer.Index()
		block := t.buffer.GetBlock()
		return block[OverTcpHeaderSize:]
	}, func(bytes []byte) {
		track.EndSeq = track.Muxer.GetHeader().Seq

		packet := t.buffer.Get(index)[:OverTcpHeaderSize+len(bytes)]
		t.OverTCP(packet, channel)
		t.AppendOutStreamBuffer(packet)
	})
}

func (t *TransStream) AddTrack(track *stream.Track) error {
	if err := t.BaseTransStream.AddTrack(track); err != nil {
		return err
	}

	payloadType, ok := librtp.CodecIdPayloads[track.Stream.CodecId()]
	if !ok {
		return fmt.Errorf("no payload type was found for codecid: %d", track.Stream.CodecId())
	}

	// 恢复上次拉流的序号
	var startSeq uint16
	if t.oldTracks != nil {
		startSeq, ok = t.oldTracks[byte(payloadType.Pt)]
		utils.Assert(ok)
	}

	// 创建RTP封装器
	var muxer librtp.Muxer
	if utils.AVCodecIdH264 == track.Stream.CodecId() {
		muxer = librtp.NewH264Muxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdH265 == track.Stream.CodecId() {
		muxer = librtp.NewH265Muxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdAAC == track.Stream.CodecId() {
		muxer = librtp.NewAACMuxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdPCMALAW == track.Stream.CodecId() || utils.AVCodecIdPCMMULAW == track.Stream.CodecId() {
		muxer = librtp.NewMuxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	}

	rtspTrack := NewRTSPTrack(muxer, byte(payloadType.Pt), payloadType.ClockRate, track.Stream.Type())
	t.RtspTracks = append(t.RtspTracks, rtspTrack)
	index := len(t.RtspTracks) - 1

	// 将sps和pps按照单一模式打包
	bufferIndex := t.buffer.Index()
	if utils.AVMediaTypeVideo == track.Stream.Type() {
		parameters := track.Stream.CodecParameters()

		if utils.AVCodecIdH265 == track.Stream.CodecId() {
			bytes := parameters.(*utils.HEVCCodecData).VPS()
			t.PackRtpPayload(rtspTrack, index, libavc.RemoveStartCode(bytes[0]), 0)
		}

		spsBytes := parameters.SPS()
		ppsBytes := parameters.PPS()
		t.PackRtpPayload(rtspTrack, index, libavc.RemoveStartCode(spsBytes[0]), 0)
		t.PackRtpPayload(rtspTrack, index, libavc.RemoveStartCode(ppsBytes[0]), 0)

		// 拷贝扩展数据的rtp包
		size := t.buffer.Index() - bufferIndex
		extraRtpBuffer := make([][]byte, size)
		for i := 0; i < size; i++ {
			src := t.buffer.Get(bufferIndex + i)
			dst := make([]byte, len(src))
			copy(dst, src)
			extraRtpBuffer[i] = dst[:OverTcpHeaderSize+binary.BigEndian.Uint16(dst[2:])]
		}

		t.RtspTracks[index].ExtraDataBuffer = extraRtpBuffer
	}

	return nil
}

func (t *TransStream) Close() ([][]byte, int64, error) {
	for _, track := range t.RtspTracks {
		if track != nil {
			track.Close()
		}
	}

	return nil, 0, nil
}

func (t *TransStream) WriteHeader() error {
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
		payloadType, _ := librtp.CodecIdPayloads[track.Stream.CodecId()]
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

		if utils.AVMediaTypeAudio == track.Stream.Type() {
			mediaDescription.MediaName.Media = "audio"

			if utils.AVCodecIdAAC == track.Stream.CodecId() {
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

func NewTransStream(addr net.IPAddr, urlFormat string, oldTracks map[byte]uint16) stream.TransStream {
	t := &TransStream{
		addr:      addr,
		urlFormat: urlFormat,
		// 在将AVPacket打包rtp时, 会使用多个buffer块, 回环覆盖多个rtp块, 如果是TCP拉流并且网络不好, 推流的数据会错乱.
		buffer:    stream.NewReceiveBuffer(1500, 1024),
		oldTracks: oldTracks,
	}

	if addr.IP.To4() != nil {
		t.addrType = "IP4"
	} else {
		t.addrType = "IP6"
	}

	return t
}

func TransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	trackFormat := "?track=%d"
	var oldTracks map[byte]uint16
	if endInfo := source.GetStreamEndInfo(); endInfo != nil {
		oldTracks = endInfo.RtspTracks
	}

	return NewTransStream(net.IPAddr{
		IP:   net.ParseIP(stream.AppConfig.PublicIP),
		Zone: "",
	}, trackFormat, oldTracks), nil
}
