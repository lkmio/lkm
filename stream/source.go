package stream

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/yangjiechina/avformat/stream"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/transcode"
)

// SourceType 推流类型
type SourceType byte

// Protocol 输出的流协议
type Protocol uint32

type SourceEvent byte

// SessionState 推拉流Session的状态
// 包含握手和Hook授权阶段
type SessionState uint32

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

	// TransMuxerHeaderMaxSize 传输流协议头的最大长度
	// 在解析流分配AVPacket的Data时, 如果没有开启合并写, 提前预留指定长度的字节数量.
	// 在封装传输流时, 直接在预留头中添加对应传输流的协议头，减少或免内存拷贝. 在传输flv以及转换AVCC和AnnexB格式时有显著提升.
	TransMuxerHeaderMaxSize = 30
)

const (
	SessionStateCreate           = SessionState(1)
	SessionStateHandshaking      = SessionState(2)
	SessionStateHandshakeFailure = SessionState(3)
	SessionStateHandshakeDone    = SessionState(4)
	SessionStateWait             = SessionState(5)
	SessionStateTransferring     = SessionState(6)
	SessionStateClose            = SessionState(7)
)

func sourceTypeToStr(sourceType SourceType) string {
	if SourceTypeRtmp == sourceType {
		return "rtmp"
	} else if SourceType28181 == sourceType {
		return "28181"
	} else if SourceType1078 == sourceType {
		return "1078"
	}

	return ""
}

func streamTypeToStr(protocol Protocol) string {
	if ProtocolRtmp == protocol {
		return "rtmp"
	} else if ProtocolFlv == protocol {
		return "flv"
	} else if ProtocolRtsp == protocol {
		return "rtsp"
	} else if ProtocolHls == protocol {
		return "hls"
	} else if ProtocolRtc == protocol {
		return "rtc"
	}

	return ""
}

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

	Type() SourceType
}

type CreateSource func(id string, type_ SourceType, handler stream.OnDeMuxerHandler)

var TranscoderFactory func(src utils.AVStream, dst utils.AVStream) transcode.ITranscoder

type SourceImpl struct {
	hookSessionImpl

	Id_   string
	Type_ SourceType
	state SessionState
	Conn  net.Conn

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

	testTransStream ITransStream
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

	if s.transStreams == nil {
		s.transStreams = make(map[TransStreamId]ITransStream, 10)
	}
	//测试传输流
	s.testTransStream = TransStreamFactory(s, ProtocolHls, nil)
	s.transStreams[0x100] = s.testTransStream
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

func (s *SourceImpl) Input(data []byte) {

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

// 分发每路Stream的Buffer给传输流
// 按照时间戳升序发送
func (s *SourceImpl) dispatchStreamBuffer(transStream ITransStream, streams []utils.AVStream) {
	size := len(streams)
	indexs := make([]int, size)

	for {
		min := int64(0xFFFFFFFF)

		//找出最小的时间戳
		for index, stream := range streams[:size] {
			if s.buffers[stream.Index()].Size() == indexs[index] {
				continue
			}

			pkt := s.buffers[stream.Index()].Peek(indexs[index]).(utils.AVPacket)
			v := pkt.Dts()
			if min == 0xFFFFFFFF {
				min = v
			} else if v < min {
				min = v
			}
		}

		if min == 0xFFFFFFFF {
			break
		}

		for index, stream := range streams[:size] {
			buffer := s.buffers[stream.Index()]
			if buffer.Size() == indexs[index] {
				continue
			}

			for i := indexs[index]; i < buffer.Size(); i++ {
				packet := buffer.Peek(i).(utils.AVPacket)
				if packet.Dts() > min {
					break
				}

				transStream.Input(packet)
				indexs[index]++
			}
		}
	}
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
		if s.transStreams == nil {
			s.transStreams = make(map[TransStreamId]ITransStream, 10)
		}
		//创建一个新的传输流
		transStream = TransStreamFactory(s, sink.Protocol(), streams[:size])
		s.transStreams[transStreamId] = transStream

		for i := 0; i < size; i++ {
			transStream.AddTrack(streams[i])
		}

		_ = transStream.WriteHeader()

		if AppConfig.GOPCache {
			s.dispatchStreamBuffer(transStream, streams[:size])
		}
	}

	sink.SetTransStreamId(transStreamId)
	//add sink 放在dispatchStreamBuffer后面，不然GOPCache没用
	transStream.AddSink(sink)

	state := sink.SetState(SessionStateTransferring)
	if !state {
		transStream.RemoveSink(sink.Id())
		return false
	}

	return true
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
		transStream.PopAllSink(func(sink ISink) {
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
	if AppConfig.GOPCache {
		buffer := NewStreamBuffer(200)
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

	if s.testTransStream != nil {
		for _, stream_ := range s.originStreams.All() {
			s.testTransStream.AddTrack(stream_)
		}

		s.testTransStream.WriteHeader()
	}
}

func (s *SourceImpl) OnDeMuxStreamDone() {
	s.writeHeader()
}

func (s *SourceImpl) OnDeMuxPacket(packet utils.AVPacket) {
	if AppConfig.GOPCache {
		buffer := s.buffers[packet.Index()]
		buffer.AddPacket(packet, packet.KeyFrame(), packet.Dts())
	}

	//分发给各个传输流
	for _, stream := range s.transStreams {
		stream.Input(packet)
	}
}

func (s *SourceImpl) OnDeMuxDone() {

}

func (s *SourceImpl) Publish(source ISource, success func(), failure func(state utils.HookState)) {
	//streamId 已经被占用
	if source_ := SourceManager.Find(source.Id()); source_ != nil {
		fmt.Printf("推流已经占用 Source:%s", source.Id())
		failure(utils.HookStateOccupy)
	}

	if !AppConfig.Hook.EnableOnPublish() {
		if err := SourceManager.Add(source); err == nil {
			success()
		} else {
			fmt.Printf("添加失败 Source:%s", source.Id())
			failure(utils.HookStateOccupy)
		}

		return
	}

	err := s.Hook(HookEventPublish, NewHookEventInfo(source.Id(), sourceTypeToStr(source.Type()), ""),
		func(response *http.Response) {
			if err := SourceManager.Add(source); err == nil {
				success()
			} else {
				failure(utils.HookStateOccupy)
			}
		}, func(response *http.Response, err error) {
			failure(utils.HookStateFailure)
		})

	//hook地址连接失败
	if err != nil {
		failure(utils.HookStateFailure)
		return
	}
}

func (s *SourceImpl) PublishDone(source ISource, success func(), failure func(state utils.HookState)) {

}

func (s *SourceImpl) Type() SourceType {
	return s.Type_
}
