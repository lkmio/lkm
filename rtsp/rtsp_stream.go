package rtsp

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/avc"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/rtp"
	"github.com/pion/sdp/v3"
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

	rtpBuffer *stream.RtpBuffer
}

func (t *TransStream) OverTCP(data []byte, channel int) {
	data[0] = OverTcpMagic
	data[1] = byte(channel)
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)-4))
}

func (t *TransStream) Input(packet *avformat.AVPacket) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	var ts uint32
	var result []*collections.ReferenceCounter[[]byte]
	track := t.RtspTracks[packet.Index]
	if utils.AVMediaTypeAudio == packet.MediaType {
		ts = uint32(packet.ConvertPts(track.Rate))
		result = t.PackRtpPayload(track, packet.Index, packet.Data, ts)
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		ts = uint32(packet.ConvertPts(track.Rate))
		annexBData := avformat.AVCCPacket2AnnexB(t.BaseTransStream.Tracks[packet.Index].Stream, packet)
		data := avc.RemoveStartCode(annexBData)
		result = t.PackRtpPayload(track, packet.Index, data, ts)
	}

	return result, int64(ts), utils.AVMediaTypeVideo == packet.MediaType && packet.Key, nil
}

func (t *TransStream) ReadExtraData(ts int64) ([]*collections.ReferenceCounter[[]byte], int64, error) {
	// 返回视频编码数据的rtp包
	for _, track := range t.RtspTracks {
		if utils.AVMediaTypeVideo != track.MediaType {
			continue
		}

		// 回滚序号和时间戳
		index := int(track.StartSeq) - len(track.ExtraDataBuffer)
		for i, packet := range track.ExtraDataBuffer {
			rtp.RollbackSeq(packet.Get()[OverTcpHeaderSize:], index+i+1)
			binary.BigEndian.PutUint32(packet.Get()[OverTcpHeaderSize+4:], uint32(ts))
		}

		// 目前只有视频需要发送扩展数据的rtp包, 所以直接返回
		return track.ExtraDataBuffer, ts, nil
	}

	return nil, ts, nil
}

// PackRtpPayload 打包返回rtp over tcp的数据包
func (t *TransStream) PackRtpPayload(track *Track, channel int, data []byte, timestamp uint32) []*collections.ReferenceCounter[[]byte] {
	var result []*collections.ReferenceCounter[[]byte]
	var packet []byte
	var counter *collections.ReferenceCounter[[]byte]

	// 保存开始序号
	track.StartSeq = track.Muxer.GetHeader().Seq
	track.Muxer.Input(data, timestamp, func() []byte {
		counter = t.rtpBuffer.Get()
		counter.Refer()

		packet = counter.Get()
		return packet[OverTcpHeaderSize:]
	}, func(bytes []byte) {
		track.EndSeq = track.Muxer.GetHeader().Seq
		overTCPPacket := packet[:OverTcpHeaderSize+len(bytes)]
		t.OverTCP(overTCPPacket, channel)

		counter.ResetData(overTCPPacket)
		result = append(result, counter)
	})

	// 引用计数保持为1
	for _, pkt := range result {
		pkt.Release()
	}

	return result
}

func (t *TransStream) AddTrack(track *stream.Track) error {
	if err := t.BaseTransStream.AddTrack(track); err != nil {
		return err
	}

	payloadType, ok := rtp.CodecIdPayloads[track.Stream.CodecID]
	if !ok {
		return fmt.Errorf("no payload type was found for codecid: %d", track.Stream.CodecID)
	}

	// 恢复上次拉流的序号
	var startSeq uint16
	if t.oldTracks != nil {
		startSeq, ok = t.oldTracks[byte(payloadType.Pt)]
		utils.Assert(ok)
	}

	// 创建RTP封装器
	var muxer rtp.Muxer
	if utils.AVCodecIdH264 == track.Stream.CodecID {
		muxer = rtp.NewH264Muxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdH265 == track.Stream.CodecID {
		muxer = rtp.NewH265Muxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdAAC == track.Stream.CodecID {
		muxer = rtp.NewAACMuxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	} else if utils.AVCodecIdPCMALAW == track.Stream.CodecID || utils.AVCodecIdPCMMULAW == track.Stream.CodecID {
		muxer = rtp.NewMuxer(payloadType.Pt, int(startSeq), 0xFFFFFFFF)
	}

	rtspTrack := NewRTSPTrack(muxer, byte(payloadType.Pt), payloadType.ClockRate, track.Stream.MediaType)
	t.RtspTracks = append(t.RtspTracks, rtspTrack)
	trackIndex := len(t.RtspTracks) - 1

	// 将sps和pps按照单一模式打包
	var extraDataPackets []*collections.ReferenceCounter[[]byte]
	packAndAdd := func(data []byte) {
		packets := t.PackRtpPayload(rtspTrack, trackIndex, data, 0)
		for _, packet := range packets {
			extra := packet.Get()
			bytes := make([]byte, len(extra))
			copy(bytes, extra)
			extraDataPackets = append(extraDataPackets, collections.NewReferenceCounter(bytes))
		}
	}

	if utils.AVMediaTypeVideo == track.Stream.MediaType {
		parameters := track.Stream.CodecParameters
		if utils.AVCodecIdH265 == track.Stream.CodecID {
			bytes := parameters.(*avformat.HEVCCodecData).VPS()
			packAndAdd(avc.RemoveStartCode(bytes[0]))
		}

		spsBytes := parameters.SPS()
		ppsBytes := parameters.PPS()
		packAndAdd(avc.RemoveStartCode(spsBytes[0]))
		packAndAdd(avc.RemoveStartCode(ppsBytes[0]))

		t.RtspTracks[trackIndex].ExtraDataBuffer = extraDataPackets
	}

	return nil
}

func (t *TransStream) Close() ([]*collections.ReferenceCounter[[]byte], int64, error) {
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
		payloadType, _ := rtp.CodecIdPayloads[track.Stream.CodecID]
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

		if utils.AVMediaTypeAudio == track.Stream.MediaType {
			mediaDescription.MediaName.Media = "audio"

			if utils.AVCodecIdAAC == track.Stream.CodecID {
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
		oldTracks: oldTracks,
		rtpBuffer: stream.NewRtpBuffer(512),
	}

	if addr.IP.To4() != nil {
		t.addrType = "IP4"
	} else {
		t.addrType = "IP6"
	}

	return t
}

func TransStreamFactory(source stream.Source, _ stream.TransStreamProtocol, _ []*stream.Track, _ stream.Sink) (stream.TransStream, error) {
	trackFormat := "?track=%d"
	var oldTracks map[byte]uint16
	if endInfo := source.GetTransStreamPublisher().GetStreamEndInfo(); endInfo != nil {
		oldTracks = endInfo.RtspTracks
	}

	return NewTransStream(net.IPAddr{
		IP:   net.ParseIP(stream.AppConfig.PublicIP),
		Zone: "",
	}, trackFormat, oldTracks), nil
}
