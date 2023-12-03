package stream

import (
	"fmt"
	"sync"
	"time"

	"github.com/yangjiechina/avformat/stream"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/transcode"
)

// SourceType Source 推流类型
type SourceType byte

// Protocol 输出协议
type Protocol uint32

type SourceEvent byte

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

	SourceEventPlay     = SourceEvent(1)
	SourceEventPlayDone = SourceEvent(2)
	SourceEventInput    = SourceEvent(3)
	SourceEventClose    = SourceEvent(4)
)

// SessionState 推拉流Session状态
// 包含, 握手阶段、Hook授权.
type SessionState uint32

const (
	SessionStateCreate           = SessionState(1)
	SessionStateHandshaking      = SessionState(2)
	SessionStateHandshakeFailure = SessionState(3)
	SessionStateHandshakeDone    = SessionState(4)
	SessionStateWait             = SessionState(5)
	SessionStateTransferring     = SessionState(6)
	SessionStateClose            = SessionState(7)
)

type ISource interface {
	// Id Source的唯一ID/**
	Id() string

	// Input 输入推流数据
	Input(data []byte)

	// OriginStreams 返回推流的原始Streams
	OriginStreams() []utils.AVStream

	// TranscodeStreams 返回转码的Streams
	TranscodeStreams() []utils.AVStream

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，将Sink添加到等待队列.
	// 匹配拉流的编码器, 创建TransMuxer或向存在TransMuxer添加Sink
	AddSink(sink ISink) bool

	// RemoveSink 删除Sink/**
	RemoveSink(sink ISink) bool

	AddEvent(event SourceEvent, data interface{})

	SetState(state SessionState)

	// Close 关闭Source
	// 停止一切封装和转发流以及转码工作
	// 将Sink添加到等待队列
	Close()
}

type CreateSource func(id string, type_ SourceType, handler stream.OnDeMuxerHandler)

var TranscoderFactory func(src utils.AVStream, dst utils.AVStream) transcode.ITranscoder

type SourceImpl struct {
	Id_   string
	Type_ SourceType
	state SessionState

	TransDeMuxer     stream.DeMuxer          //负责从推流协议中解析出AVStream和AVPacket
	recordSink       ISink                   //每个Source唯一的一个录制流
	audioTranscoders []transcode.ITranscoder //音频解码器
	videoTranscoders []transcode.ITranscoder //视频解码器
	originStreams    StreamManager           //推流的音视频Streams
	allStreams       StreamManager           //推流Streams+转码器获得的Streams
	buffers          []StreamBuffer

	Input_ func(data []byte) //解决无法多态传递给子类的问题

	completed  bool
	mutex      sync.Mutex //只用作AddStream期间
	probeTimer *time.Timer

	//所有的输出协议, 持有Sink
	transStreams map[TransStreamId]ITransStream

	//sink的拉流和断开拉流事件，都通过管道交给Source处理. 意味着Source内部解析流、封装流、传输流都可以做到无锁操作
	//golang的管道是有锁的(https://github.com/golang/go/blob/d38f1d13fa413436d38d86fe86d6a146be44bb84/src/runtime/chan.go#L202), 后面使用cas队列传输事件, 并且可以做到一次读取多个事件
	inputEvent            chan []byte
	responseEvent         chan byte //解析完input的数据后，才能继续从网络io中读取流
	closeEvent            chan byte
	playingEventQueue     chan ISink
	playingDoneEventQueue chan ISink
}

func (s *SourceImpl) Id() string {
	return s.Id_
}

func (s *SourceImpl) Init() {
	//初始化事件接收缓冲区
	s.SetState(SessionStateTransferring)
	//收流和网络断开的chan都阻塞执行
	s.inputEvent = make(chan []byte)
	s.responseEvent = make(chan byte)
	s.closeEvent = make(chan byte)
	s.playingEventQueue = make(chan ISink, 128)
	s.playingDoneEventQueue = make(chan ISink, 128)
}

func (s *SourceImpl) LoopEvent() {
	for {
		select {
		case data := <-s.inputEvent:
			s.Input_(data)
			s.responseEvent <- 0
			break
		case sink := <-s.playingEventQueue:
			s.AddSink(sink)
			break
		case sink := <-s.playingDoneEventQueue:
			s.RemoveSink(sink)
			break
		case _ = <-s.closeEvent:
			s.Close()
			return
		}
	}
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
	var size int

	for _, stream := range s.originStreams.All() {
		if disableVideo && stream.Type() == utils.AVMediaTypeVideo {
			continue
		}

		streams[size] = stream
		size++
	}

	transStreamId := GenerateTransStreamId(sink.Protocol(), streams[:size]...)
	transStream, ok := s.transStreams[transStreamId]
	if !ok {
		//创建一个新的传输流
		transStream = TransStreamFactory(sink.Protocol(), streams[:size])
		if s.transStreams == nil {
			s.transStreams = make(map[TransStreamId]ITransStream, 10)
		}
		s.transStreams[transStreamId] = transStream

		for i := 0; i < size; i++ {
			transStream.AddTrack(streams[i])
		}

		_ = transStream.WriteHeader()
	}

	sink.SetTransStreamId(transStreamId)
	transStream.AddSink(sink)

	state := sink.SetState(SessionStateTransferring)
	if !state {
		transStream.RemoveSink(sink.Id())
		return false
	}

	if AppConfig.GOPCache > 0 && !ok {
		indexs := make([]int, size)

		for {
			min := int64(0xFFFFFFFF)

			for index, stream := range streams[:size] {
				size := s.buffers[stream.Index()].Size()
				if size == indexs[index] {
					continue
				}

				pkt := s.buffers[stream.Index()].Peek(indexs[index]).(utils.AVPacket)
				v := pkt.Dts()
				if min == 0xFFFFFFFF {
					min = v
				} else if v < min {
					v = min
				}
			}

			if min == 0xFFFFFFFF {
				break
			}

			for index, stream := range streams[:size] {
				buffer := s.buffers[stream.Index()]
				size := buffer.Size()
				if size == indexs[index] {
					continue
				}

				for i := indexs[index]; i < buffer.Size(); i++ {
					packet := buffer.Peek(i).(utils.AVPacket)
					transStream.Input(packet)
					indexs[index]++
				}
			}
		}
	}

	return false
}

func (s *SourceImpl) RemoveSink(sink ISink) bool {
	id := sink.TransStreamId()
	if id > 0 {
		transStream := s.transStreams[id]
		//如果从传输流没能删除sink, 再从等待队列删除
		_, b := transStream.RemoveSink(sink.Id())
		if b {
			return true
		}
	}

	_, b := RemoveSinkFromWaitingQueue(sink.SourceId(), sink.Id())
	return b
}

func (s *SourceImpl) AddEvent(event SourceEvent, data interface{}) {
	if SourceEventInput == event {
		s.inputEvent <- data.([]byte)
		<-s.responseEvent
	} else if SourceEventPlay == event {
		s.playingEventQueue <- data.(ISink)
	} else if SourceEventPlayDone == event {
		s.playingDoneEventQueue <- data.(ISink)
	} else if SourceEventClose == event {
		s.closeEvent <- 0
	}
}

func (s *SourceImpl) SetState(state SessionState) {
	s.state = state
}

func (s *SourceImpl) Close() {
	//释放解复用器
	//释放转码器
	//释放每路转协议流， 将所有sink添加到等待队列
	_, _ = SourceManager.Remove(s.Id_)
	for _, transStream := range s.transStreams {
		transStream.PopAllSinks(func(sink ISink) {
			sink.SetTransStreamId(0)
			state := sink.SetState(SessionStateWait)
			if state {
				AddSinkToWaitingQueue(s.Id_, sink)
			}
		})
	}

	s.transStreams = nil
}

func (s *SourceImpl) OnDeMuxStream(stream utils.AVStream) (bool, StreamBuffer) {
	//整块都受保护 确保Add的Stream 都能WriteHeader
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.completed {
		fmt.Printf("添加Stream失败 Source: %s已经WriteHeader", s.Id_)
		return false, nil
	}

	s.originStreams.Add(stream)
	s.allStreams.Add(stream)

	//启动探测超时计时器
	if len(s.originStreams.All()) == 1 && AppConfig.ProbeTimeout > 100 {
		s.probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, s.writeHeader)
	}

	//为每个Stream创建对应的Buffer
	if AppConfig.GOPCache > 0 {
		buffer := NewStreamBuffer(int64(AppConfig.GOPCache * 1000))
		//OnDeMuxStream的调用顺序，就是AVStream和AVPacket的Index的递增顺序
		s.buffers = append(s.buffers, buffer)
		return true, buffer
	}

	return true, nil
}

// 从DeMuxer解析完Stream后, 处理等待Sinks
func (s *SourceImpl) writeHeader() {
	{
		s.mutex.Lock()
		defer s.mutex.Unlock()
		if s.completed {
			return
		}
		s.completed = true
	}

	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}

	sinks := PopWaitingSinks(s.Id_)
	for _, sink := range sinks {
		s.AddSink(sink)
	}
}

func (s *SourceImpl) OnDeMuxStreamDone() {
	s.writeHeader()
}

func (s *SourceImpl) OnDeMuxPacket(packet utils.AVPacket) {
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
