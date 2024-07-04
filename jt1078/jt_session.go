package jt1078

import (
	"encoding/binary"
	"fmt"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
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
	audioBuffer   stream.MemoryPool
	videoBuffer   stream.MemoryPool
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

func (s *Session) processVideoPacket(pt byte, pktType byte, ts uint64, data []byte, index int) error {
	var packet utils.AVPacket
	var stream_ utils.AVStream

	if PTVideoH264 == pt {
		if s.videoStream == nil {
			if VideoIFrameMark != pktType {
				return fmt.Errorf("skip non keyframes")
			}

			videoStream, err := utils.CreateAVCStreamFromKeyFrame(data, 1)
			if err != nil {
				return err
			}

			stream_ = videoStream
		}

		packet = utils.NewVideoPacket(data, int64(ts), int64(ts), VideoIFrameMark == pktType, utils.PacketTypeAnnexB, utils.AVCodecIdH265, index, 1000)
	} else if PTVideoH265 == pt {
		if s.videoStream == nil {
			if VideoIFrameMark != pktType {
				return fmt.Errorf("skip non keyframes")
			}

			videoStream, err := utils.CreateHevcStreamFromKeyFrame(data, 1)
			if err != nil {
				return err
			}

			stream_ = videoStream
		}

		packet = utils.NewVideoPacket(data, int64(ts), int64(ts), VideoIFrameMark == pktType, utils.PacketTypeAnnexB, utils.AVCodecIdH265, index, 1000)
	} else {
		return fmt.Errorf("the codec %d is not implemented", pt)
	}

	if stream_ != nil {
		s.videoStream = stream_
		s.OnDeMuxStream(stream_)
		if s.videoStream != nil && s.audioStream != nil {
			s.OnDeMuxStreamDone()
		}
	}

	s.OnDeMuxPacket(packet)
	return nil
}

func (s *Session) processAudioPacket(pt byte, pktType byte, ts uint64, data []byte, index int) error {
	var packet utils.AVPacket
	var stream_ utils.AVStream

	if PTAudioG711A == pt {
		if s.audioStream == nil {
			stream_ = utils.NewAVStream(utils.AVMediaTypeAudio, 0, utils.AVCodecIdPCMALAW, nil, nil)
		}

		packet = utils.NewAudioPacket(data, int64(ts), int64(ts), utils.AVCodecIdPCMALAW, index, 1000)
	} else if PTAudioG711U == pt {
		if s.audioStream == nil {
			stream_ = utils.NewAVStream(utils.AVMediaTypeAudio, 0, utils.AVCodecIdPCMMULAW, nil, nil)
		}

		packet = utils.NewAudioPacket(data, int64(ts), int64(ts), utils.AVCodecIdPCMMULAW, index, 1000)
	} else if PTAudioAAC == pt {

	} else {
		return fmt.Errorf("the codec %d is not implemented", pt)
	}

	if stream_ != nil {
		s.audioStream = stream_
		s.OnDeMuxStream(stream_)
		if s.videoStream != nil && s.audioStream != nil {
			s.OnDeMuxStreamDone()
		}
	}

	s.OnDeMuxPacket(packet)
	return nil
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
	//或者参考时间戳, 推流的分包标记为可能不靠谱
	if s.rtpPacket.ts != packet.ts || s.rtpPacket.pt != packet.pt {
		if s.rtpPacket.packetType == AudioFrameMark {
			if err := s.processAudioPacket(s.rtpPacket.pt, s.rtpPacket.packetType, s.rtpPacket.ts, s.audioBuffer.Fetch(), s.audioIndex); err != nil {
				log.Sugar.Errorf("处理音频包失败 phone:%s err:%s", s.phone, err.Error())
				s.audioBuffer.FreeTail()
			}

			*s.rtpPacket = packet
			s.audioBuffer.Mark()
		} else {
			if err := s.processVideoPacket(s.rtpPacket.pt, s.rtpPacket.packetType, s.rtpPacket.ts, s.videoBuffer.Fetch(), s.videoIndex); err != nil {
				log.Sugar.Errorf("处理视频包失败 phone:%s err:%s", s.phone, err.Error())
				s.videoBuffer.FreeTail()
			}

			*s.rtpPacket = packet
			s.videoBuffer.Mark()
		}
	}

	if packet.packetType == AudioFrameMark {
		if s.audioBuffer == nil {
			if s.videoIndex == 0 && s.audioIndex == 0 {
				s.videoIndex = 1
			}

			s.audioBuffer = s.FindOrCreatePacketBuffer(s.audioIndex, utils.AVMediaTypeAudio)
			s.audioBuffer.Mark()
		}

		s.audioBuffer.Write(packet.payload)
	} else {
		if s.videoBuffer == nil {
			if s.videoIndex == 0 && s.audioIndex == 0 {
				s.audioIndex = 1
			}

			s.videoBuffer = s.FindOrCreatePacketBuffer(s.videoIndex, utils.AVMediaTypeVideo)
			s.videoBuffer.Mark()
		}

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
