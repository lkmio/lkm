package rtsp

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/avc"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
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
type TransStream struct {
	stream.BaseTransStream
	addr      net.IPAddr
	addrType  string
	urlFormat string

	sdp             string
	RtspTracks      []*Track
	lastEndSeq      map[utils.AVCodecID]uint16  // 上次结束推流的rtp seq
	segments        []stream.TransStreamSegment // 缓存的切片
	packetAllocator *stream.RtpBuffer           // 分配rtp包
}

func (t *TransStream) OverTCP(data []byte, channel int) {
	data[0] = OverTcpMagic
	data[1] = byte(channel)
	binary.BigEndian.PutUint16(data[2:], uint16(len(data)-4))
}

func (t *TransStream) Input(packet *avformat.AVPacket, trackIndex int) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	var ts uint32
	var result []*collections.ReferenceCounter[[]byte]
	track := t.RtspTracks[trackIndex]

	duration := packet.GetDuration(track.payload.ClockRate)
	//dts := t.Tracks[trackIndex].Dts
	ts = uint32(t.Tracks[trackIndex].Pts)
	t.Tracks[trackIndex].Dts += duration
	t.Tracks[trackIndex].Pts = t.Tracks[trackIndex].Dts + packet.GetPtsDtsDelta(track.payload.ClockRate)

	if utils.AVMediaTypeAudio == packet.MediaType {
		result = t.PackRtpPayload(track, trackIndex, packet.Data, ts, false)
	} else if utils.AVMediaTypeVideo == packet.MediaType {
		annexBData := avformat.AVCCPacket2AnnexB(t.BaseTransStream.Tracks[trackIndex].Stream, packet)
		result = t.PackRtpPayload(track, trackIndex, annexBData, ts, packet.Key)
	}

	return result, int64(ts), utils.AVMediaTypeVideo == packet.MediaType && packet.Key, nil
}

func (t *TransStream) ReadKeyFrameBuffer() ([]stream.TransStreamSegment, error) {
	// 默认不开启rtsp的关键帧缓存, 一次发送rtp包过多, 播放器的jitter buffer可能会溢出丢弃, 造成播放花屏
	//return t.segments, nil
	return nil, nil
}

// PackRtpPayload 打包返回rtp over tcp的数据包
func (t *TransStream) PackRtpPayload(track *Track, trackIndex int, data []byte, timestamp uint32, videoKey bool) []*collections.ReferenceCounter[[]byte] {
	// 分割nalu
	var payloads [][]byte
	if utils.AVCodecIdH264 == track.CodecID || utils.AVCodecIdH265 == track.CodecID {
		avc.SplitNalU(data, func(nalu []byte) {
			payloads = append(payloads, avc.RemoveStartCode(nalu))
		})
	} else {
		payloads = append(payloads, data)
	}

	var result []*collections.ReferenceCounter[[]byte]
	var packet []byte
	var counter *collections.ReferenceCounter[[]byte]

	for _, payload := range payloads {
		// 保存开始序号
		track.StartSeq = track.Muxer.GetHeader().Seq
		track.Muxer.Input(payload, timestamp, func() []byte {
			counter = t.packetAllocator.Get()
			counter.Refer()

			packet = counter.Get()
			// 预留rtp over tcp 4字节头部
			return packet[OverTcpHeaderSize:]
		}, func(bytes []byte) {
			track.EndSeq = track.Muxer.GetHeader().Seq
			// 每个包都存在rtp over tcp 4字节头部
			overTCPPacket := packet[:OverTcpHeaderSize+len(bytes)]
			t.OverTCP(overTCPPacket, trackIndex)

			counter.ResetData(overTCPPacket)
			result = append(result, counter)
		})
	}

	// 引用计数保持为1
	for _, pkt := range result {
		pkt.Release()
	}

	if t.HasVideo() && stream.AppConfig.GOPCache {
		// 遇到视频关键帧, 丢弃前一帧缓存
		if videoKey {
			for _, segment := range t.segments {
				for _, pkt := range segment.Data {
					pkt.Release()
				}
			}

			t.segments = t.segments[:0]
		}

		// 计数+1
		for _, pkt := range result {
			pkt.Refer()
		}

		// 放在缓存末尾
		t.segments = append(t.segments, stream.TransStreamSegment{
			Data:  result,
			TS:    int64(timestamp),
			Key:   videoKey,
			Index: trackIndex,
		})
	}

	return result
}

func (t *TransStream) AddTrack(track *stream.Track) (int, error) {
	// 恢复上次拉流的序号
	var startSeq uint16
	if t.lastEndSeq != nil {
		var ok bool
		startSeq, ok = t.lastEndSeq[track.Stream.CodecID]
		utils.Assert(ok)
	}

	// 查找RTP封装器和PayloadType
	newMuxerFunc := rtp.SupportedCodecs[track.Stream.CodecID]
	utils.Assert(newMuxerFunc != nil)

	// 创建RTP封装器
	muxer, payload := newMuxerFunc.(func(seq int, ssrc uint32) (rtp.Muxer, rtp.PayloadType))(int(startSeq), 0xFFFFFFFF)

	// 创建track
	rtspTrack := NewRTSPTrack(muxer, payload, track.Stream.MediaType, track.Stream.CodecID)
	t.RtspTracks = append(t.RtspTracks, rtspTrack)
	trackIndex := len(t.RtspTracks) - 1

	// 将sps和pps按照单一模式打包
	packAndAdd := func(data []byte) {
		packets := t.PackRtpPayload(rtspTrack, trackIndex, data, 0, true)
		utils.Assert(len(packets) == 1)
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
	}

	return trackIndex, nil
}

func (t *TransStream) Close() ([]stream.TransStreamSegment, error) {
	for _, track := range t.RtspTracks {
		if track != nil {
			track.Close()
		}
	}

	return nil, nil
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

	for i, track := range t.RtspTracks {
		mediaDescription := sdp.MediaDescription{
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: t.addrType,
				Address:     &sdp.Address{Address: t.addr.IP.String()},
			},

			Attributes: []sdp.Attribute{
				sdp.NewAttribute("recvonly", ""),
				sdp.NewAttribute("control:"+fmt.Sprintf(t.urlFormat, i), ""),
				sdp.NewAttribute(fmt.Sprintf("rtpmap:%d %s/%d", track.payload.Pt, track.payload.Encoding, track.payload.ClockRate), ""),
			},
		}

		mediaDescription.MediaName.Protos = []string{"RTP", "AVP"}
		mediaDescription.MediaName.Formats = []string{strconv.Itoa(track.payload.Pt)}

		if utils.AVMediaTypeAudio == track.MediaType {
			mediaDescription.MediaName.Media = "audio"

			if utils.AVCodecIdAAC == track.CodecID {
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

		} else if utils.AVMediaTypeVideo == track.MediaType {
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

func NewTransStream(addr net.IPAddr, urlFormat string, oldTracks map[utils.AVCodecID]uint16) stream.TransStream {
	t := &TransStream{
		addr:            addr,
		urlFormat:       urlFormat,
		lastEndSeq:      oldTracks,
		packetAllocator: stream.NewRtpBuffer(512),
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
	var oldTracks map[utils.AVCodecID]uint16
	if endInfo := source.GetTransStreamPublisher().GetStreamEndInfo(); endInfo != nil {
		oldTracks = endInfo.RtspTracks
		if oldTracks != nil {
			for codecID, seq := range oldTracks {
				log.Sugar.Infof("track codecID: %s, seq: %d", codecID, seq)
			}
		}
	}

	return NewTransStream(net.IPAddr{
		IP:   net.ParseIP(stream.AppConfig.PublicIP),
		Zone: "",
	}, trackFormat, oldTracks), nil
}
