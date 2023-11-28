package stream

import (
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/transcode"
	"time"
)

// SourceType Source 推流类型
type SourceType byte

// Protocol 输出协议
type Protocol uint32

const (
	SourceTypeRtmp  = SourceType(1)
	SourceType28181 = SourceType(2)
	SourceType1078  = SourceType(3)

	ProtocolRtmp = Protocol(1)
	ProtocolFlv  = Protocol(2)
	ProtocolRtsp = Protocol(3)
	ProtocolHls  = Protocol(4)
	ProtocolRtc  = Protocol(5)

	ProtocolRtmpStr = "rtmp"
)

// SessionState 推拉流Session状态
// 包含, 握手阶段、Hook授权.
type SessionState uint32

const (
	SessionStateCreate           = SessionState(1)
	SessionStateHandshaking      = SessionState(2)
	SessionStateHandshakeFailure = SessionState(3)
	SessionStateHandshakeDone    = SessionState(4)
	SessionStateTransferring     = SessionState(5)
)

type ISource interface {
	// Id Source的唯一ID/**
	Id() string

	// Input 输入推流数据
	Input(data []byte)

	// CreateTransDeMuxer 创建推流的解服用器
	CreateTransDeMuxer() ITransDeMuxer

	// CreateTranscoder 创建转码器
	CreateTranscoder(src utils.AVStream, dst utils.AVStream) transcode.ITranscoder

	// OriginStreams 返回推流的原始Streams
	OriginStreams() []utils.AVStream

	// TranscodeStreams 返回转码的Streams
	TranscodeStreams() []utils.AVStream

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，将Sink添加到等待队列.
	// 匹配拉流的编码器, 创建TransMuxer或向存在TransMuxer添加Sink
	AddSink(sink ISink) bool

	// RemoveSink 删除Sink/**
	RemoveSink(tid TransStreamId, sinkId string) bool

	// Close 关闭Source
	// 停止一切封装和转发流以及转码工作
	// 将Sink添加到等待队列
	Close()
}

type onSourceHandler interface {
	onDeMuxStream(stream utils.AVStream)
}

type SourceImpl struct {
	Id_   string
	type_ SourceType
	state SessionState

	deMuxer          ITransDeMuxer           //负责从推流协议中解析出AVStream和AVPacket
	recordSink       ISink                   //每个Source唯一的一个录制流
	audioTranscoders []transcode.ITranscoder //音频解码器
	videoTranscoders []transcode.ITranscoder //视频解码器
	originStreams    StreamManager           //推流的音视频Streams
	allStreams       StreamManager           //推流Streams+转码器获得的Streams
	buffers          []StreamBuffer

	completed  bool
	probeTimer *time.Timer

	//所有的输出协议, 持有Sink
	transStreams map[TransStreamId]ITransStream
}

func (s *SourceImpl) Id() string {
	return s.Id_
}

func (s *SourceImpl) Input(data []byte) {
	s.deMuxer.Input(data)
}

func (s *SourceImpl) CreateTransDeMuxer() ITransDeMuxer {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) CreateTranscoder(src utils.AVStream, dst utils.AVStream) transcode.ITranscoder {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) OriginStreams() []utils.AVStream {
	return s.originStreams.All()
}

func (s *SourceImpl) TranscodeStreams() []utils.AVStream {
	return s.allStreams.All()
}

func IsSupportMux(protocol Protocol, audioCodecId, videoCodecId utils.AVCodecID) bool {
	if ProtocolRtmp == protocol || ProtocolFlv == protocol {

	}

	return true
}

func (s *SourceImpl) AddSink(sink ISink) bool {
	// 暂时不考虑多路视频流，意味着只能1路视频流和多路音频流，同理originStreams和allStreams里面的Stream互斥. 同时多路音频流的Codec必须一致
	audioCodecId, videoCodecId := sink.DesiredAudioCodecId(), sink.DesiredVideoCodecId()
	audioStream := s.originStreams.FindStreamWithType(utils.AVMediaTypeAudio)
	videoStream := s.originStreams.FindStreamWithType(utils.AVMediaTypeVideo)

	disableAudio := audioStream == nil
	disableVideo := videoStream == nil || !sink.EnableVideo()
	if disableAudio && disableVideo {
		return false
	}

	//不支持对期望编码的流封装. 降级
	if (utils.AVCodecIdNONE != audioCodecId || utils.AVCodecIdNONE != videoCodecId) && !IsSupportMux(sink.Protocol(), audioCodecId, videoCodecId) {
		audioCodecId = utils.AVCodecIdNONE
		videoCodecId = utils.AVCodecIdNONE
	}

	if !disableAudio && utils.AVCodecIdNONE == audioCodecId {
		audioCodecId = audioStream.CodecId()
	}
	if !disableVideo && utils.AVCodecIdNONE == videoCodecId {
		videoCodecId = videoStream.CodecId()
	}

	//创建音频转码器
	if !disableAudio && audioCodecId != audioStream.CodecId() {
		utils.Assert(false)
	}

	//创建视频转码器
	if !disableVideo && videoCodecId != videoStream.CodecId() {
		utils.Assert(false)
	}

	var streams [5]utils.AVStream
	var index int
	bufferCount := -1

	for _, stream := range s.originStreams.All() {
		if disableVideo && stream.Type() == utils.AVMediaTypeVideo {
			continue
		}

		streams[index] = stream
		index++

		//从缓存的Stream中，挑选出最小的缓存数量，交叉发送.
		count := s.buffers[stream.Index()].Size()
		if bufferCount == -1 {
			bufferCount = count
		} else {
			bufferCount = utils.MinInt(bufferCount, count)
		}
	}

	transStreamId := GenerateTransStreamId(sink.Protocol(), streams[:index]...)
	transStream, ok := s.transStreams[transStreamId]
	if !ok {
		//创建一个新的传输流
		transStream = TransStreamFactory(sink.Protocol(), streams[:index])
		if s.transStreams == nil {
			s.transStreams = make(map[TransStreamId]ITransStream, 10)
		}
		s.transStreams[transStreamId] = transStream

		for i := 0; i < index; i++ {
			transStream.AddTrack(streams[i])
		}

		_ = transStream.WriteHeader()
	}

	transStream.AddSink(sink)

	if AppConfig.GOPCache > 0 && !ok {
		//先交叉发送
		for i := 0; i < bufferCount; i++ {
			for _, stream := range streams[:index] {
				buffer := s.buffers[stream.Index()]
				packet := buffer.Peek(i).(utils.AVPacket)
				transStream.Input(packet)
			}
		}

		//发送超过最低缓存数的缓存包
		for _, stream := range streams[:index] {
			buffer := s.buffers[stream.Index()]

			for i := bufferCount; i > buffer.Size(); i++ {
				packet := buffer.Peek(i).(utils.AVPacket)
				transStream.Input(packet)
			}
		}
	}

	return false
}

func (s *SourceImpl) RemoveSink(tid TransStreamId, sinkId string) bool {
	return true
}

func (s *SourceImpl) Close() {

}

func (s *SourceImpl) OnDeMuxStream(stream utils.AVStream) (bool, StreamBuffer) {
	if s.completed {
		fmt.Printf("添加Stream失败 Source: %s已经WriteHeader", s.Id_)
		return false, nil
	}

	s.originStreams.Add(stream)
	s.allStreams.Add(stream)
	//if len(s.originStreams.All()) == 1 {
	//	s.probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, s.writeHeader)
	//}

	//为每个Stream创建对于的Buffer
	if AppConfig.GOPCache > 0 {
		buffer := NewStreamBuffer(int64(AppConfig.GOPCache * 1000))
		s.buffers = append(s.buffers, buffer)

		return true, buffer
	}

	return true, nil
}

// 从DeMuxer解析完Stream后, 处理等待Sinks
func (s *SourceImpl) writeHeader() {
	utils.Assert(!s.completed)
	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}
	s.completed = true

	sinks := PopWaitingSinks(s.Id_)
	for _, sink := range sinks {
		s.AddSink(sink)
	}
}

func (s *SourceImpl) OnDeMuxStreamDone() {
	s.writeHeader()
}

func (s *SourceImpl) OnDeMuxPacket(index int, packet utils.AVPacket) {
	if AppConfig.GOPCache > 0 {
		buffer := s.buffers[packet.Index()]
		buffer.AddPacket(packet, packet.KeyFrame(), packet.Dts())
	}

	for _, stream := range s.transStreams {
		stream.Input(packet)
	}
}

func (s *SourceImpl) OnDeMuxDone() {
}
