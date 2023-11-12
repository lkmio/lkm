package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/transcode"
)

type TransStreamId uint32

// SourceType Source 推流类型
type SourceType uint32

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

	// AddSink 添加Sink, 在此之前Sink已经握手、授权通过. 如果Source还未WriteHeader，将Sink添加到等待队列.
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
	transcodeStreams []utils.AVStream        //从音视频解码器中获得的AVStream

	//所有的输出协议, 持有Sink
	transStreams map[TransStreamId]TransStream
}

func (s *SourceImpl) Id() string {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) Input(data []byte) {
	//TODO implement me
	panic("implement me")
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
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) TranscodeStreams() []utils.AVStream {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) AddSink(sink ISink) bool {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) RemoveSink(tid TransStreamId, sinkId string) bool {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) Close() {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) OnDeMuxStream(stream utils.AVStream) {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) OnDeMuxStreamDone() {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) OnDeMuxPacket(index int, packet *utils.AVPacket2) {
	//TODO implement me
	panic("implement me")
}

func (s *SourceImpl) OnDeMuxDone() {
	//TODO implement me
	panic("implement me")
}
