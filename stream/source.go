package stream

import (
	"fmt"
	"github.com/yangjiechina/lkm/log"
	"net"
	"net/url"
	"time"

	"github.com/yangjiechina/avformat/stream"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/transcode"
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

	SourceEventPlay     = SourceEvent(1)
	SourceEventPlayDone = SourceEvent(2)
	SourceEventInput    = SourceEvent(3)
	SourceEventClose    = SourceEvent(4)
)

const (
	SessionStateCreate           = SessionState(1) //新建状态
	SessionStateHandshaking      = SessionState(2) //握手中
	SessionStateHandshakeFailure = SessionState(3) //握手失败
	SessionStateHandshakeDone    = SessionState(4) //握手完成
	SessionStateWait             = SessionState(5) //位于等待队列中
	SessionStateTransferring     = SessionState(6) //推拉流中
	SessionStateClose            = SessionState(7) //关闭状态
)

// Source 父类Source负责, 除解析流以外的所有事情
type Source interface {
	// Id Source的唯一ID/**
	Id() string

	SetId(id string)

	// Input 输入推流数据
	//@Return bool fatal error.释放Source
	Input(data []byte) error

	// Type 推流类型
	Type() SourceType

	SetType(sourceType SourceType)

	// OriginStreams 返回推流的原始Streams
	OriginStreams() []utils.AVStream

	// TranscodeStreams 返回转码的Streams
	TranscodeStreams() []utils.AVStream

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，先将Sink添加到等待队列.
	// 匹配拉流的编码器, 创建TransStream或向存在TransStream添加Sink
	AddSink(sink Sink) bool

	// RemoveSink 删除Sink/**
	RemoveSink(sink Sink) bool

	AddEvent(event SourceEvent, data interface{})

	SetState(state SessionState)

	// Close 关闭Source
	// 停止一切封装和转发流以及转码工作
	// 将Sink添加到等待队列
	Close()

	// FindOrCreatePacketBuffer 查找或者创建AVPacket的内存池
	FindOrCreatePacketBuffer(index int, mediaType utils.AVMediaType) MemoryPool

	// OnDiscardPacket GOP缓存溢出回调, 释放AVPacket
	OnDiscardPacket(pkt utils.AVPacket)

	// OnDeMuxStream 解析出AVStream回调
	OnDeMuxStream(stream utils.AVStream)

	// IsCompleted 是否已经WireHeader
	IsCompleted() bool

	// OnDeMuxStreamDone 所有track解析完毕, 后续的OnDeMuxStream回调不再处理
	OnDeMuxStreamDone()

	// OnDeMuxPacket 解析出AvPacket回调
	OnDeMuxPacket(packet utils.AVPacket)

	// OnDeMuxDone 所有流解析完毕回调
	OnDeMuxDone()

	Init(inputCB func(data []byte) error, closeCB func(), receiveQueueSize int)

	LoopEvent()

	RemoteAddr() string

	PrintInfo() string

	// StartReceiveDataTimer 启动收流超时计时器
	StartReceiveDataTimer()

	// StartIdleTimer 启动拉流空闲计时器
	StartIdleTimer()

	State() SessionState

	SetInputCb(func(data []byte) error)

	UrlValues() url.Values

	SetUrlValues(values url.Values)
}

type PublishSource struct {
	Id_   string
	Type_ SourceType
	state SessionState
	Conn  net.Conn

	TransDeMuxer     stream.DeMuxer         //负责从推流协议中解析出AVStream和AVPacket
	recordSink       Sink                   //每个Source的录制流
	hlsStream        TransStream            //如果开开启HLS传输流, 不等拉流时, 创建直接生成
	audioTranscoders []transcode.Transcoder //音频解码器
	videoTranscoders []transcode.Transcoder //视频解码器
	originStreams    StreamManager          //推流的音视频Streams
	allStreams       StreamManager          //推流Streams+转码器获得的Stream
	pktBuffers       [8]MemoryPool          //推流每路的AVPacket缓存, AVPacket的data从该内存池中分配. 在GOP缓存溢出时,释放池中内存.
	gopBuffer        GOPBuffer              //GOP缓存, 音频和视频混合使用, 以视频关键帧为界, 缓存第二个视频关键帧时, 释放前一组gop. 如果不存在视频流, 不缓存音频

	existVideo bool //是否存在视频
	completed  bool
	probeTimer *time.Timer

	inputCB func(data []byte) error //子类Input回调
	closeCB func()                  //子类Close回调

	//所有的输出协议, 持有Sink
	transStreams map[TransStreamId]TransStream

	//sink的拉流和断开拉流事件，都通过管道交给Source处理. 意味着Source内部解析流、封装流、传输流都可以做到无锁操作
	//golang的管道是有锁的(https://github.com/golang/go/blob/d38f1d13fa413436d38d86fe86d6a146be44bb84/src/runtime/chan.go#L202), 后面使用cas队列传输事件, 并且可以做到一次读取多个事件
	inputDataEvent        chan []byte
	closedEvent           chan byte
	playingEventQueue     chan Sink
	playingDoneEventQueue chan Sink
	probeTimoutEvent      chan bool

	lastPacketTime   time.Time
	removeSinkTime   time.Time
	receiveDataTimer *time.Timer
	idleTimer        *time.Timer
	sinkCount        int
	closed           bool
	urlValues        url.Values
}

func (s *PublishSource) Id() string {
	return s.Id_
}

func (s *PublishSource) SetId(id string) {
	s.Id_ = id
}

func (s *PublishSource) Init(inputCB func(data []byte) error, closeCB func(), receiveQueueSize int) {
	s.inputCB = inputCB
	s.closeCB = closeCB

	s.SetState(SessionStateHandshakeDone)
	//初始化事件接收缓冲区
	//收流和网络断开的chan都阻塞执行
	//-1是为了保证从管道取到流, 到解析流是安全的, 不会被覆盖
	s.inputDataEvent = make(chan []byte, receiveQueueSize-1)
	s.closedEvent = make(chan byte)
	s.playingEventQueue = make(chan Sink, 128)
	s.playingDoneEventQueue = make(chan Sink, 128)
	s.probeTimoutEvent = make(chan bool)

	if s.transStreams == nil {
		s.transStreams = make(map[TransStreamId]TransStream, 10)
	}

	//创建录制流
	if AppConfig.Record.Enable {

	}

	//创建HLS输出流
	if AppConfig.Hls.Enable {
		hlsStream, err := CreateTransStream(s, ProtocolHls, nil)
		if err != nil {
			panic(err)
		}

		s.hlsStream = hlsStream
		s.transStreams[0x100] = s.hlsStream
	}
}

// FindOrCreatePacketBuffer 查找或者创建AVPacket的内存池
func (s *PublishSource) FindOrCreatePacketBuffer(index int, mediaType utils.AVMediaType) MemoryPool {
	if index >= cap(s.pktBuffers) {
		panic("流路数过多...")
	}

	if s.pktBuffers[index] == nil {
		if utils.AVMediaTypeAudio == mediaType {
			s.pktBuffers[index] = NewRbMemoryPool(48000 * 64)
		} else if AppConfig.GOPCache {
			//开启GOP缓存
			s.pktBuffers[index] = NewRbMemoryPool(AppConfig.GOPBufferSize)
		} else {
			//未开启GOP缓存
			//1M缓存大小, 单帧绰绰有余
			s.pktBuffers[index] = NewRbMemoryPool(1024 * 1000)
		}
	}

	return s.pktBuffers[index]
}

func (s *PublishSource) LoopEvent() {
	for {
		select {
		case data := <-s.inputDataEvent:
			if s.closed {
				break
			}

			if AppConfig.ReceiveTimeout > 0 {
				s.lastPacketTime = time.Now()
			}

			if err := s.inputCB(data); err != nil {
				log.Sugar.Errorf("处理输入流失败 释放source:%s err:%s", s.Id_, err.Error())
				s.doClose()
			}
			break
		case sink := <-s.playingEventQueue:
			if !s.completed {
				AddSinkToWaitingQueue(sink.SourceId(), sink)
			} else {
				if !s.AddSink(sink) {
					sink.Close()
				}
			}
			break
		case sink := <-s.playingDoneEventQueue:
			s.RemoveSink(sink)
			break
		case _ = <-s.closedEvent:
			s.doClose()
			return
		case _ = <-s.probeTimoutEvent:
			s.writeHeader()
			break
		}
	}
}

func (s *PublishSource) Input(data []byte) error {
	s.AddEvent(SourceEventInput, data)
	return nil
}

func (s *PublishSource) OriginStreams() []utils.AVStream {
	return s.originStreams.All()
}

func (s *PublishSource) TranscodeStreams() []utils.AVStream {
	return s.allStreams.All()
}

func IsSupportMux(protocol Protocol, audioCodecId, videoCodecId utils.AVCodecID) bool {
	if ProtocolRtmp == protocol || ProtocolFlv == protocol {

	}

	return true
}

func (s *PublishSource) AddSink(sink Sink) bool {
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

	for _, stream_ := range s.originStreams.All() {
		if disableVideo && stream_.Type() == utils.AVMediaTypeVideo {
			continue
		}

		streams[size] = stream_
		size++
	}

	transStreamId := GenerateTransStreamId(sink.Protocol(), streams[:size]...)
	transStream, ok := s.transStreams[transStreamId]
	if !ok {
		if s.transStreams == nil {
			s.transStreams = make(map[TransStreamId]TransStream, 10)
		}
		//创建一个新的传输流
		log.Sugar.Debugf("创建%s-stream source:%s", sink.Protocol().ToString(), s.Id_)

		var err error
		transStream, err = CreateTransStream(s, sink.Protocol(), streams[:size])
		if err != nil {
			log.Sugar.Errorf("创建传输流失败 err:%s source:%s", err.Error(), s.Id_)
			return false
		}

		s.transStreams[transStreamId] = transStream

		for i := 0; i < size; i++ {
			transStream.AddTrack(streams[i])
		}

		transStream.Init()
		_ = transStream.WriteHeader()
	}

	sink.SetTransStreamId(transStreamId)

	{
		sink.Lock()
		defer sink.UnLock()

		if SessionStateClose == sink.State() {
			log.Sugar.Warnf("AddSink失败, sink已经断开链接 %s", sink.PrintInfo())
		} else {
			transStream.AddSink(sink)
		}
		sink.SetState(SessionStateTransferring)
	}

	s.sinkCount++

	//新的传输流，发送缓存的音视频帧
	if !ok && AppConfig.GOPCache && s.existVideo {
		s.gopBuffer.PeekAll(func(packet utils.AVPacket) {
			transStream.Input(packet)
		})
	}

	return true
}

func (s *PublishSource) RemoveSink(sink Sink) bool {
	id := sink.TransStreamId()
	if id > 0 {
		transStream := s.transStreams[id]
		//如果从传输流没能删除sink, 再从等待队列删除
		_, b := transStream.RemoveSink(sink.Id())
		if b {
			s.sinkCount--
			s.removeSinkTime = time.Now()
			HookPlayDoneEvent(sink)
			return true
		}
	}

	_, b := RemoveSinkFromWaitingQueue(sink.SourceId(), sink.Id())
	return b
}

func (s *PublishSource) AddEvent(event SourceEvent, data interface{}) {
	if SourceEventInput == event {
		s.inputDataEvent <- data.([]byte)
	} else if SourceEventPlay == event {
		s.playingEventQueue <- data.(Sink)
	} else if SourceEventPlayDone == event {
		s.playingDoneEventQueue <- data.(Sink)
	} else if SourceEventClose == event {
		s.closedEvent <- 0
	}
}

func (s *PublishSource) SetState(state SessionState) {
	s.state = state
}

func (s *PublishSource) doClose() {
	if s.closed {
		return
	}

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	if s.TransDeMuxer != nil {
		s.TransDeMuxer.Close()
		s.TransDeMuxer = nil
	}

	//清空未写完的buffer
	for _, buffer := range s.pktBuffers {
		if buffer != nil {
			buffer.Reset()
		}
	}

	//释放GOP缓存
	if s.gopBuffer != nil {
		s.gopBuffer.Clear()
		s.gopBuffer.Close()
		s.gopBuffer = nil
	}

	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}

	if s.receiveDataTimer != nil {
		s.receiveDataTimer.Stop()
	}

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	//释放解复用器
	//释放转码器
	//释放每路转协议流， 将所有sink添加到等待队列
	_, err := SourceManager.Remove(s.Id_)
	if err != nil {
		log.Sugar.Errorf("删除源失败 source:%s err:%s", s.Id_, err.Error())
	}

	for _, transStream := range s.transStreams {
		transStream.Close()

		transStream.PopAllSink(func(sink Sink) {
			sink.SetTransStreamId(0)
			{
				sink.Lock()
				defer sink.UnLock()

				if SessionStateClose == sink.State() {
					log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开链接 %s", sink.PrintInfo())
				} else {
					sink.SetState(SessionStateWait)
					AddSinkToWaitingQueue(s.Id_, sink)
				}
			}

			if SessionStateClose != sink.State() {
				sink.Flush()
			}
		})
	}

	s.closed = true
	s.transStreams = nil
	go HookPublishDoneEvent(s)
}

func (s *PublishSource) Close() {
	s.AddEvent(SourceEventClose, nil)
}

func (s *PublishSource) OnDiscardPacket(packet utils.AVPacket) {
	s.FindOrCreatePacketBuffer(packet.Index(), packet.MediaType()).FreeHead()
}

func (s *PublishSource) OnDeMuxStream(stream utils.AVStream) {
	if s.completed {
		log.Sugar.Warnf("添加Stream失败 Source: %s已经WriteHeader", s.Id_)
		return
	}

	s.originStreams.Add(stream)
	s.allStreams.Add(stream)

	//启动探测超时计时器
	if len(s.originStreams.All()) == 1 {
		if AppConfig.ProbeTimeout == 0 {
			AppConfig.ProbeTimeout = 2000
		}

		s.probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, func() {
			s.probeTimoutEvent <- true
		})
	}

	if utils.AVMediaTypeVideo == stream.Type() {
		s.existVideo = true
	}

	//为每个Stream创建对应的Buffer
	if AppConfig.GOPCache && s.existVideo {
		s.gopBuffer = NewStreamBuffer()
		//设置GOP缓存溢出回调
		s.gopBuffer.SetDiscardHandler(s.OnDiscardPacket)
	}
}

// 从DeMuxer解析完Stream后, 处理等待Sinks
func (s *PublishSource) writeHeader() {
	if s.completed {
		fmt.Printf("添加Stream失败 Source: %s已经WriteHeader", s.Id_)
		return
	}

	s.completed = true

	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}

	sinks := PopWaitingSinks(s.Id_)
	for _, sink := range sinks {
		if !s.AddSink(sink) {
			sink.Close()
		}
	}

	if s.hlsStream != nil {
		for _, stream_ := range s.originStreams.All() {
			s.hlsStream.AddTrack(stream_)
		}

		s.hlsStream.WriteHeader()
	}
}

func (s *PublishSource) IsCompleted() bool {
	return s.completed
}

func (s *PublishSource) OnDeMuxStreamDone() {
	s.writeHeader()
}

func (s *PublishSource) OnDeMuxPacket(packet utils.AVPacket) {
	if AppConfig.GOPCache && s.existVideo {
		s.gopBuffer.AddPacket(packet)
	}

	//分发给各个传输流
	for _, stream_ := range s.transStreams {
		stream_.Input(packet)
	}

	//未开启GOP缓存或只存在音频流, 释放掉内存
	if !AppConfig.GOPCache || !s.existVideo {
		s.FindOrCreatePacketBuffer(packet.Index(), packet.MediaType()).FreeTail()
	}
}

func (s *PublishSource) OnDeMuxDone() {

}

func (s *PublishSource) Type() SourceType {
	return s.Type_
}

func (s *PublishSource) SetType(sourceType SourceType) {
	s.Type_ = sourceType
}

func (s *PublishSource) RemoteAddr() string {
	if s.Conn == nil {
		return ""
	}

	return s.Conn.RemoteAddr().String()
}

func (s *PublishSource) PrintInfo() string {
	return fmt.Sprintf("id:%s type:%s conn:%s ", s.Id_, s.Type_.ToString(), s.RemoteAddr())
}

func (s *PublishSource) StartReceiveDataTimer() {
	utils.Assert(s.receiveDataTimer == nil)
	utils.Assert(AppConfig.ReceiveTimeout > 0)

	s.lastPacketTime = time.Now()
	s.receiveDataTimer = time.AfterFunc(time.Duration(AppConfig.ReceiveTimeout), func() {
		dis := time.Now().Sub(s.lastPacketTime)

		//如果开启Hook通知, 根据响应决定是否关闭Source
		//如果通知失败, 或者非200应答, 释放Source
		//如果没有开启Hook通知, 直接删除
		if dis >= time.Duration(AppConfig.ReceiveTimeout) {
			log.Sugar.Errorf("收流超时 source:%s", s.Id_)
			response, state := HookReceiveTimeoutEvent(s)
			if utils.HookStateOK != state || response == nil {
				s.closeCB()
				return
			}
		}

		//对精度没要求
		s.receiveDataTimer.Reset(time.Duration(AppConfig.ReceiveTimeout))
	})
}

func (s *PublishSource) StartIdleTimer() {
	utils.Assert(s.idleTimer == nil)
	utils.Assert(AppConfig.IdleTimeout > 0)

	s.removeSinkTime = time.Now()
	s.idleTimer = time.AfterFunc(time.Duration(AppConfig.IdleTimeout), func() {
		dis := time.Now().Sub(s.removeSinkTime)

		if s.sinkCount < 1 && dis >= time.Duration(AppConfig.IdleTimeout) {
			log.Sugar.Errorf("空闲超时 source:%s", s.Id_)
			response, state := HookIdleTimeoutEvent(s)
			if utils.HookStateOK != state || response == nil {
				s.closeCB()
				return
			}
		}

		s.idleTimer.Reset(time.Duration(AppConfig.IdleTimeout))
	})
}

func (s *PublishSource) State() SessionState {
	return s.state
}

func (s *PublishSource) SetInputCb(cb func(data []byte) error) {
	s.inputCB = cb
}

func (s *PublishSource) UrlValues() url.Values {
	return s.urlValues
}
func (s *PublishSource) SetUrlValues(values url.Values) {
	s.urlValues = values
}
