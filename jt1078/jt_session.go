package jt1078

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/collections"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
)

const (
	VideoIFrameMark      = 0b000
	VideoPFrameMark      = 0b001
	VideoBFrameMark      = 0b010
	AudioFrameMark       = 0b011
	TransmissionDataMark = 0b1000

	PTVideoH264 = 98
	PTVideoH265 = 99
	PTVideoAVS  = 100
	PTVideoSVAC = 101

	PTAudioG711A  = 6
	PTAudioG711U  = 7
	PTAudioG726   = 8
	PTAudioG729A  = 9
	PTAudioAAC    = 19
	PTAudioMP3    = 25
	PTAudioADPCMA = 26
)

type Session struct {
	stream.PublishSource

	phone   string
	decoder *transport.DelimiterFrameDecoder

	audioIndex    int
	videoIndex    int
	audioStream   utils.AVStream
	videoStream   utils.AVStream
	audioBuffer   collections.MemoryPool
	videoBuffer   collections.MemoryPool
	rtpPacket     *RtpPacket
	receiveBuffer *stream.ReceiveBuffer
}

type RtpPacket struct {
	pt         byte
	packetType byte
	ts         uint64
	subMark    byte
	simNumber  string

	payload []byte
}

func (s *Session) OnJtPTPPacket(data []byte) {
	packet, err := read1078RTPPacket(data)
	if err != nil {
		return
	}

	//过滤空数据
	if len(packet.payload) == 0 {
		return
	}

	//首包处理, hook通知
	if s.rtpPacket == nil {
		s.Id_ = packet.simNumber
		s.rtpPacket = &RtpPacket{}
		*s.rtpPacket = packet

		go func() {
			_, state := stream.PreparePublishSource(s, true)
			if utils.HookStateOK != state {
				log.Sugar.Errorf("1078推流失败 source:%s", s.phone)

				if s.Conn != nil {
					s.Conn.Close()
				}
			}
		}()
	}

	//完整包/最后一个分包, 创建AVPacket
	//参考时间戳, 遇到不同的时间戳, 处理前一包. 分包标记可能不靠谱
	if s.rtpPacket.ts != packet.ts || s.rtpPacket.pt != packet.pt {
		if s.rtpPacket.packetType == AudioFrameMark && s.audioBuffer != nil {
			if err := s.processAudioPacket(s.rtpPacket.pt, s.rtpPacket.packetType, s.rtpPacket.ts, s.audioBuffer.Fetch(), s.audioIndex); err != nil {
				log.Sugar.Errorf("处理音频包失败 phone:%s err:%s", s.phone, err.Error())
				s.audioBuffer.FreeTail()
			}

			*s.rtpPacket = packet
		} else if s.rtpPacket.packetType < AudioFrameMark && s.videoBuffer != nil {
			if err := s.processVideoPacket(s.rtpPacket.pt, s.rtpPacket.packetType, s.rtpPacket.ts, s.videoBuffer.Fetch(), s.videoIndex); err != nil {
				log.Sugar.Errorf("处理视频包失败 phone:%s err:%s", s.phone, err.Error())
				s.videoBuffer.FreeTail()
			}

			*s.rtpPacket = packet
		}
	}

	if packet.packetType == AudioFrameMark {
		if s.audioBuffer == nil {
			if s.videoIndex == 0 && s.audioIndex == 0 {
				s.videoIndex = 1
			}

			if s.IsCompleted() {
				if !s.IsTimeoutTrack(s.audioIndex) {
					s.SetTimeoutTrack(s.audioIndex)
					log.Sugar.Errorf("添加audiotrack超时")
				}
				return
			}

			s.audioBuffer = s.FindOrCreatePacketBuffer(s.audioIndex, utils.AVMediaTypeAudio)
		}

		s.audioBuffer.TryMark()
		s.audioBuffer.Write(packet.payload)
	} else {
		if s.videoBuffer == nil {
			if s.videoIndex == 0 && s.audioIndex == 0 {
				s.audioIndex = 1
			}

			if s.IsCompleted() {
				if !s.IsTimeoutTrack(s.videoIndex) {
					s.SetTimeoutTrack(s.videoIndex)
					log.Sugar.Errorf("添加videotrack超时")
				}
				return
			}

			s.videoBuffer = s.FindOrCreatePacketBuffer(s.videoIndex, utils.AVMediaTypeVideo)
		}

		s.videoBuffer.TryMark()
		s.videoBuffer.Write(packet.payload)
	}
}

func (s *Session) Input(data []byte) error {
	return s.decoder.Input(data)
}

func (s *Session) Close() {
	log.Sugar.Infof("1078推流结束 phone number:%s %s", s.phone, s.PublishSource.PrintInfo())

	if s.audioBuffer != nil {
		s.audioBuffer.Clear()
	}

	if s.videoBuffer != nil {
		s.videoBuffer.Clear()
	}

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	if s.decoder != nil {
		s.decoder.Close()
		s.decoder = nil
	}

	s.PublishSource.Close()
}

func (s *Session) processVideoPacket(pt byte, pktType byte, ts uint64, data []byte, index int) error {
	var codecId utils.AVCodecID

	if PTVideoH264 == pt {
		if s.videoStream == nil && VideoIFrameMark != pktType {
			log.Sugar.Errorf("skip non keyframes conn:%s", s.Conn.RemoteAddr())
			return nil
		}
		codecId = utils.AVCodecIdH264
	} else if PTVideoH265 == pt {
		if s.videoStream == nil && VideoIFrameMark != pktType {
			log.Sugar.Errorf("skip non keyframes conn:%s", s.Conn.RemoteAddr())
			return nil
		}
		codecId = utils.AVCodecIdH265
	} else {
		return fmt.Errorf("the codec %d is not implemented", pt)
	}

	videoStream, videoPacket, err := stream.ExtractVideoPacket(codecId, VideoIFrameMark == pktType, s.videoStream == nil, data, int64(ts), int64(ts), index, 1000)
	if err != nil {
		return err
	}

	if videoStream != nil {
		s.videoStream = videoStream
		s.OnDeMuxStream(videoStream)
		if s.videoStream != nil && s.audioStream != nil {
			s.OnDeMuxStreamDone()
		}
	}

	s.OnDeMuxPacket(videoPacket)
	return nil
}

func (s *Session) processAudioPacket(pt byte, pktType byte, ts uint64, data []byte, index int) error {
	var codecId utils.AVCodecID

	if PTAudioG711A == pt {
		codecId = utils.AVCodecIdPCMALAW
	} else if PTAudioG711U == pt {
		codecId = utils.AVCodecIdPCMMULAW
	} else if PTAudioAAC == pt {
		codecId = utils.AVCodecIdAAC
	} else {
		return fmt.Errorf("the codec %d is not implemented", pt)
	}

	audioStream, audioPacket, err := stream.ExtractAudioPacket(codecId, s.audioStream == nil, data, int64(ts), int64(ts), index, 1000)
	if err != nil {
		return err
	}

	if audioStream != nil {
		s.audioStream = audioStream
		s.OnDeMuxStream(audioStream)
		if s.videoStream != nil && s.audioStream != nil {
			s.OnDeMuxStreamDone()
		}
	}

	s.OnDeMuxPacket(audioPacket)
	return nil
}

// 读取1078的rtp包, 返回数据类型, 负载类型、时间戳、负载数据
func read1078RTPPacket(data []byte) (RtpPacket, error) {
	if len(data) < 12 {
		return RtpPacket{}, fmt.Errorf("invaild data")
	}

	packetType := data[11] >> 4 & 0x0F
	//忽略透传数据
	if TransmissionDataMark == packetType {
		return RtpPacket{}, fmt.Errorf("invaild data")
	}

	//忽略低于最低长度的数据包
	if (AudioFrameMark == packetType && len(data) < 26) || (AudioFrameMark == packetType && len(data) < 22) {
		return RtpPacket{}, fmt.Errorf("invaild data")
	}

	//x扩展位,固定为0
	_ = data[0] >> 4 & 0x1
	pt := data[1] & 0x7F
	//seq
	_ = binary.BigEndian.Uint16(data[2:])

	var simNumber string
	for i := 4; i < 10; i++ {
		simNumber += fmt.Sprintf("%02d", data[i])
	}

	//channel
	_ = data[10]
	//subMark
	subMark := data[11] & 0x0F
	//单位ms
	var ts uint64
	n := 12
	if TransmissionDataMark != packetType {
		ts = binary.BigEndian.Uint64(data[n:])
		n += 8
	}

	if AudioFrameMark > packetType {
		//iFrameInterval
		_ = binary.BigEndian.Uint16(data[n:])
		n += 2
		//lastFrameInterval
		_ = binary.BigEndian.Uint16(data[n:])
		n += 2
	}

	//size
	_ = binary.BigEndian.Uint16(data[n:])
	n += 2

	return RtpPacket{pt: pt, packetType: packetType, ts: ts, simNumber: simNumber, subMark: subMark, payload: data[n:]}, nil
}

func NewSession(conn net.Conn) *Session {
	session := Session{
		PublishSource: stream.PublishSource{
			Conn:  conn,
			Type_: stream.SourceType1078,
		},
	}
	delimiter := [4]byte{0x30, 0x31, 0x63, 0x64}
	session.decoder = transport.NewDelimiterFrameDecoder(1024*1024*2, delimiter[:], session.OnJtPTPPacket)
	session.receiveBuffer = stream.NewTCPReceiveBuffer()

	session.Init(session.Input, session.Close, stream.ReceiveBufferTCPBlockCount)
	go session.LoopEvent()
	return &session
}
