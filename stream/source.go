package stream

import (
	"fmt"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/collections"
	"github.com/lkmio/lkm/log"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lkmio/avformat/stream"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/transcode"
)

// Source 对推流源的封装, 处理除解析流以外的所有事情
type Source interface {
	// GetID 返回SourceID
	GetID() string

	SetID(id string)

	// Input 输入推流数据
	Input(data []byte) error

	// GetType 返回推流类型
	GetType() SourceType

	SetType(sourceType SourceType)

	// OriginStreams 返回推流的原始Streams
	OriginStreams() []utils.AVStream

	// TranscodeStreams 返回转码的Streams
	TranscodeStreams() []utils.AVStream

	// AddSink 添加Sink, 在此之前请确保Sink已经握手、授权通过. 如果Source还未WriteHeader，先将Sink添加到等待队列.
	// 匹配拉流期望的编码器, 创建TransStream或向已经存在TransStream添加Sink
	AddSink(sink Sink)

	// RemoveSink 删除Sink
	RemoveSink(sink Sink)

	RemoveSinkWithID(id SinkID)

	FindSink(id SinkID) Sink

	SetState(state SessionState)

	// Close 关闭Source
	// 关闭推流网络链路, 停止一切封装和转发流以及转码工作
	// 将Sink添加到等待队列
	Close()

	DoClose()

	// IsCompleted 所有推流track是否解析完毕
	IsCompleted() bool

	// FindOrCreatePacketBuffer 查找或者创建AVPacket的内存池
	FindOrCreatePacketBuffer(index int, mediaType utils.AVMediaType) collections.MemoryPool

	// OnDiscardPacket GOP缓存溢出回调, 释放AVPacket
	OnDiscardPacket(pkt utils.AVPacket)

	// OnDeMuxStream 解析出AVStream回调
	OnDeMuxStream(stream utils.AVStream)

	// OnDeMuxStreamDone 所有track解析完毕回调, 后续的OnDeMuxStream回调不再处理
	OnDeMuxStreamDone()

	// OnDeMuxPacket 解析出AvPacket回调
	OnDeMuxPacket(packet utils.AVPacket)

	// OnDeMuxDone 所有流解析完毕回调
	OnDeMuxDone()

	Init(receiveQueueSize int)

	RemoteAddr() string

	String() string

	State() SessionState

	// UrlValues 返回推流url参数
	UrlValues() url.Values

	// SetUrlValues 设置推流url参数
	SetUrlValues(values url.Values)

	// PostEvent 切换到主协程执行当前函数
	PostEvent(cb func())

	// LastPacketTime 返回最近收流时间戳
	LastPacketTime() time.Time

	SetLastPacketTime(time2 time.Time)

	// SinkCount 返回拉流计数
	SinkCount() int

	// LastStreamEndTime 返回最近结束拉流时间戳
	LastStreamEndTime() time.Time

	IsClosed() bool

	StreamPipe() chan []byte

	MainContextEvents() chan func()

	CreateTime() time.Time

	SetCreateTime(time time.Time)

	Sinks() []Sink

	GetBitrateStatistics() *BitrateStatistics
}

type PublishSource struct {
	ID    string
	Type  SourceType
	state SessionState
	Conn  net.Conn

	TransDeMuxer     stream.DeMuxer            // 负责从推流协议中解析出AVStream和AVPacket
	recordSink       Sink                      // 每个Source的录制流
	recordFilePath   string                    // 录制流文件路径
	hlsStream        TransStream               // HLS传输流, 如果开启, 在@see writeHeader 函数中直接创建, 如果等拉流时再创建, 会进一步加大HLS延迟.
	audioTranscoders []transcode.Transcoder    // 音频解码器
	videoTranscoders []transcode.Transcoder    // 视频解码器
	originStreams    StreamManager             // 推流的音视频Streams
	allStreams       StreamManager             // 推流Streams+转码器获得的Stream
	pktBuffers       [8]collections.MemoryPool // 推流每路的AVPacket缓存, AVPacket的data从该内存池中分配. 在GOP缓存溢出时,释放池中内存.
	gopBuffer        GOPBuffer                 // GOP缓存, 音频和视频混合使用, 以视频关键帧为界, 缓存第二个视频关键帧时, 释放前一组gop. 如果不存在视频流, 不缓存音频

	closed     atomic.Bool // source是否已经关闭
	completed  bool        // 所有推流track是否解析完毕, @see writeHeader 函数中赋值为true
	existVideo bool        // 是否存在视频

	probeTimer *time.Timer // track解析超时计时器, 触发时执行@see writeHeader

	TransStreams     map[TransStreamID]TransStream     // 所有的输出流, 持有Sink
	sinks            map[SinkID]Sink                   // 保存所有Sink
	TransStreamSinks map[TransStreamID]map[SinkID]Sink // 输出流对应的Sink

	streamPipe        chan []byte // 推流数据管道
	mainContextEvents chan func() // 切换到主协程执行函数的事件管道

	lastPacketTime    time.Time          // 最近收到推流包的时间
	lastStreamEndTime time.Time          // 最近拉流端结束拉流的时间
	sinkCount         int                // 拉流端计数
	urlValues         url.Values         // 推流url携带的参数
	createTime        time.Time          // source创建时间
	statistics        *BitrateStatistics // 码流统计
}

func (s *PublishSource) SetLastPacketTime(time2 time.Time) {
	s.lastPacketTime = time2
}

func (s *PublishSource) IsClosed() bool {
	return s.closed.Load()
}

func (s *PublishSource) StreamPipe() chan []byte {
	return s.streamPipe
}

func (s *PublishSource) MainContextEvents() chan func() {
	return s.mainContextEvents
}

func (s *PublishSource) LastStreamEndTime() time.Time {
	return s.lastStreamEndTime
}

func (s *PublishSource) LastPacketTime() time.Time {
	return s.lastPacketTime
}

func (s *PublishSource) SinkCount() int {
	return s.sinkCount
}

func (s *PublishSource) GetID() string {
	return s.ID
}

func (s *PublishSource) SetID(id string) {
	s.ID = id
}

func (s *PublishSource) Init(receiveQueueSize int) {
	s.SetState(SessionStateHandshakeSuccess)

	// 初始化事件接收管道
	// -2是为了保证从管道取到流, 到处理完流整个过程安全的, 不会被覆盖
	s.streamPipe = make(chan []byte, receiveQueueSize-2)
	s.mainContextEvents = make(chan func(), 128)

	s.TransStreams = make(map[TransStreamID]TransStream, 10)
	s.sinks = make(map[SinkID]Sink, 128)
	s.TransStreamSinks = make(map[TransStreamID]map[SinkID]Sink, len(transStreamFactories)+1)
	s.statistics = NewBitrateStatistics()
}

func (s *PublishSource) CreateDefaultOutStreams() {
	if s.TransStreams == nil {
		s.TransStreams = make(map[TransStreamID]TransStream, 10)
	}

	// 创建录制流
	if AppConfig.Record.Enable {
		sink, path, err := CreateRecordStream(s.ID)
		if err != nil {
			log.Sugar.Errorf("创建录制sink失败 source:%s err:%s", s.ID, err.Error())
		} else {
			s.recordSink = sink
			s.recordFilePath = path
		}
	}

	// 创建HLS输出流
	if AppConfig.Hls.Enable {
		streams := s.OriginStreams()
		utils.Assert(len(streams) > 0)

		id := GenerateTransStreamID(TransStreamHls, streams...)
		hlsStream, err := s.CreateTransStream(id, TransStreamHls, streams)
		if err != nil {
			panic(err)
		}

		s.DispatchGOPBuffer(hlsStream)
		s.hlsStream = hlsStream
		s.TransStreams[id] = s.hlsStream
	}
}

// FindOrCreatePacketBuffer 查找或者创建AVPacket的内存池
func (s *PublishSource) FindOrCreatePacketBuffer(index int, mediaType utils.AVMediaType) collections.MemoryPool {
	if index >= cap(s.pktBuffers) {
		panic("流路数过多...")
	}

	if s.pktBuffers[index] == nil {
		if utils.AVMediaTypeAudio == mediaType {
			s.pktBuffers[index] = collections.NewRbMemoryPool(48000 * 12)
		} else if AppConfig.GOPCache {
			// 开启GOP缓存
			s.pktBuffers[index] = collections.NewRbMemoryPool(AppConfig.GOPBufferSize)
		} else {
			// 未开启GOP缓存
			// 1M缓存大小, 单帧绰绰有余
			s.pktBuffers[index] = collections.NewRbMemoryPool(1024 * 1000)
		}
	}

	return s.pktBuffers[index]
}

func (s *PublishSource) Input(data []byte) error {
	s.streamPipe <- data
	s.statistics.Input(len(data))
	return nil
}

func (s *PublishSource) OriginStreams() []utils.AVStream {
	return s.originStreams.All()
}

func (s *PublishSource) TranscodeStreams() []utils.AVStream {
	return s.allStreams.All()
}

func IsSupportMux(protocol TransStreamProtocol, audioCodecId, videoCodecId utils.AVCodecID) bool {
	if TransStreamRtmp == protocol || TransStreamFlv == protocol {

	}

	return true
}

func (s *PublishSource) CreateTransStream(id TransStreamID, protocol TransStreamProtocol, streams []utils.AVStream) (TransStream, error) {
	log.Sugar.Debugf("创建%s-stream source: %s", protocol.String(), s.ID)

	transStream, err := CreateTransStream(s, protocol, streams)
	if err != nil {
		log.Sugar.Errorf("创建传输流失败 err: %s source: %s", err.Error(), s.ID)
		return nil, err
	}

	for _, track := range streams {
		transStream.AddTrack(track)
	}

	transStream.SetID(id)
	// 创建输出流对应的拉流队列
	s.TransStreamSinks[id] = make(map[SinkID]Sink, 128)
	_ = transStream.WriteHeader()

	return transStream, err
}

func (s *PublishSource) DispatchGOPBuffer(transStream TransStream) {
	s.gopBuffer.PeekAll(func(packet utils.AVPacket) {
		s.DispatchPacket(transStream, packet)
	})
}

// DispatchPacket 分发AVPacket
func (s *PublishSource) DispatchPacket(transStream TransStream, packet utils.AVPacket) {
	data, timestamp, videoKey, err := transStream.Input(packet)
	if err != nil || len(data) < 1 {
		return
	}

	s.DispatchBuffer(transStream, packet.Index(), data, timestamp, videoKey)
}

// DispatchBuffer 分发传输流
func (s *PublishSource) DispatchBuffer(transStream TransStream, index int, data [][]byte, timestamp int64, videoKey bool) {
	sinks := s.TransStreamSinks[transStream.GetID()]
	exist := transStream.IsExistVideo()
	for _, sink := range sinks {

		// 如果存在视频, 确保向sink发送的第一帧是关键帧
		if exist && sink.GetSentPacketCount() < 1 {
			if !videoKey {
				continue
			}

			if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
				s.write(sink, index, extraData, timestamp)
			}
		}

		s.write(sink, index, data, timestamp)
	}
}

// 向sink推流
func (s *PublishSource) write(sink Sink, index int, data [][]byte, timestamp int64) {
	err := sink.Write(index, data, timestamp)
	if err == nil {
		sink.IncreaseSentPacketCount()
		//return
	}

	// 推流超时, 可能是服务器或拉流端带宽不够、拉流端不读取数据等情况造成内核发送缓冲区满, 进而阻塞.
	// 直接关闭连接. 当然也可以将sink先挂起, 后续再继续推流.
	_, ok := err.(*transport.ZeroWindowSizeError)
	if ok {
		log.Sugar.Errorf("向sink推流超时,关闭连接. sink: %s", sink.GetID())
		sink.Close()
	}
}

// 创建sink需要的输出流
func (s *PublishSource) doAddSink(sink Sink) bool {
	// 暂时不考虑多路视频流，意味着只能1路视频流和多路音频流，同理originStreams和allStreams里面的Stream互斥. 同时多路音频流的Codec必须一致
	audioCodecId, videoCodecId := sink.DesiredAudioCodecId(), sink.DesiredVideoCodecId()
	audioStream := s.originStreams.FindStreamWithType(utils.AVMediaTypeAudio)
	videoStream := s.originStreams.FindStreamWithType(utils.AVMediaTypeVideo)

	disableAudio := audioStream == nil
	disableVideo := videoStream == nil || !sink.EnableVideo()
	if disableAudio && disableVideo {
		return false
	}

	// 不支持对期望编码的流封装. 降级
	if (utils.AVCodecIdNONE != audioCodecId || utils.AVCodecIdNONE != videoCodecId) && !IsSupportMux(sink.GetProtocol(), audioCodecId, videoCodecId) {
		audioCodecId = utils.AVCodecIdNONE
		videoCodecId = utils.AVCodecIdNONE
	}

	if !disableAudio && utils.AVCodecIdNONE == audioCodecId {
		audioCodecId = audioStream.CodecId()
	}
	if !disableVideo && utils.AVCodecIdNONE == videoCodecId {
		videoCodecId = videoStream.CodecId()
	}

	// 创建音频转码器
	if !disableAudio && audioCodecId != audioStream.CodecId() {
		utils.Assert(false)
	}

	// 创建视频转码器
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

	transStreamId := GenerateTransStreamID(sink.GetProtocol(), streams[:size]...)
	transStream, exist := s.TransStreams[transStreamId]
	if !exist {
		var err error
		transStream, err = s.CreateTransStream(transStreamId, sink.GetProtocol(), streams[:size])
		if err != nil {
			log.Sugar.Errorf("创建传输流失败 err: %s source: %s", err.Error(), s.ID)
			return false
		}

		s.TransStreams[transStreamId] = transStream
	}

	sink.SetTransStreamID(transStreamId)

	{
		sink.Lock()
		defer sink.UnLock()

		if SessionStateClosed == sink.GetState() {
			log.Sugar.Warnf("AddSink失败, sink已经断开连接 %s", sink.String())
		} else {
			sink.SetState(SessionStateTransferring)
		}
	}

	err := sink.StartStreaming(transStream)
	if err != nil {
		log.Sugar.Errorf("开始推流失败 err: %s", err.Error())
		return false
	}

	// 还没做好准备(rtsp拉流还在协商sdp中), 暂不推流
	if !sink.IsReady() {
		return true
	}

	// TCP拉流开启异步发包, 一旦出现网络不好的链路, 其余正常链路不受影响.
	conn, ok := sink.GetConn().(*transport.Conn)
	if ok && sink.IsTCPStreaming() && transStream.OutStreamBufferCapacity() > 2 {
		conn.EnableAsyncWriteMode(transStream.OutStreamBufferCapacity() - 2)
	}

	// 发送已有的缓存数据
	// 此处发送缓存数据，必须要存在关键帧的输出流才发，否则等DispatchPacket时再发送extra。
	data, timestamp, _ := transStream.ReadKeyFrameBuffer()
	if len(data) > 0 {
		if extraData, _, _ := transStream.ReadExtraData(timestamp); len(extraData) > 0 {
			s.write(sink, 0, extraData, timestamp)
		}

		s.write(sink, 0, data, timestamp)
	}

	// 累加拉流计数
	if s.recordSink != sink {
		s.sinkCount++
		log.Sugar.Infof("sink count: %d source: %s", s.sinkCount, s.ID)
	}

	s.sinks[sink.GetID()] = sink
	s.TransStreamSinks[transStreamId][sink.GetID()] = sink

	// 新建传输流，发送已经缓存的音视频帧
	if !exist && AppConfig.GOPCache && s.existVideo {
		s.DispatchGOPBuffer(transStream)
	}

	return true
}

func (s *PublishSource) AddSink(sink Sink) {
	s.PostEvent(func() {
		if !s.completed {
			AddSinkToWaitingQueue(sink.GetSourceID(), sink)
		} else {
			if !s.doAddSink(sink) {
				sink.Close()
			}
		}
	})
}

func (s *PublishSource) RemoveSink(sink Sink) {
	s.PostEvent(func() {
		s.doRemoveSink(sink)
	})
}

func (s *PublishSource) RemoveSinkWithID(id SinkID) {
	s.PostEvent(func() {
		sink, ok := s.sinks[id]
		if ok {
			s.doRemoveSink(sink)
		}
	})
}

func (s *PublishSource) FindSink(id SinkID) Sink {
	var result Sink
	group := sync.WaitGroup{}
	group.Add(1)

	s.PostEvent(func() {
		sink, ok := s.sinks[id]
		if ok {
			result = sink
		}

		group.Done()
	})

	group.Wait()
	return result
}

func (s *PublishSource) doRemoveSink(sink Sink) bool {
	transStreamSinks := s.TransStreamSinks[sink.GetTransStreamID()]
	delete(s.sinks, sink.GetID())
	delete(transStreamSinks, sink.GetID())

	s.sinkCount--
	s.lastStreamEndTime = time.Now()

	log.Sugar.Infof("sink count: %d source: %s", s.sinkCount, s.ID)

	if sink.GetProtocol() == TransStreamHls {
		// 从HLS拉流队列删除Sink
		SinkManager.Remove(sink.GetID())
	}

	sink.StopStreaming(s.TransStreams[sink.GetTransStreamID()])
	HookPlayDoneEvent(sink)
	return true
}

func (s *PublishSource) SetState(state SessionState) {
	s.state = state
}

func (s *PublishSource) DoClose() {
	log.Sugar.Debugf("closing the %s source. id: %s. closed flag: %t", s.Type, s.ID, s.closed)

	if s.closed.Load() {
		return
	}

	s.closed.Store(true)

	if s.TransDeMuxer != nil {
		s.TransDeMuxer.Close()
		s.TransDeMuxer = nil
	}

	// 清空未写完的buffer
	for _, buffer := range s.pktBuffers {
		if buffer != nil {
			buffer.Reset()
		}
	}

	// 释放GOP缓存
	if s.gopBuffer != nil {
		s.gopBuffer.Clear()
		s.gopBuffer.Close()
		s.gopBuffer = nil
	}

	// 停止track探测计时器
	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}

	// 关闭录制流
	if s.recordSink != nil {
		s.recordSink.Close()
	}

	// 释放解复用器
	// 释放转码器
	// 释放每路转协议流， 将所有sink添加到等待队列
	_, err := SourceManager.Remove(s.ID)
	if err != nil {
		// source不存在, 在创建source时, 未添加到manager中, 目前只有1078流会出现这种情况(tcp连接到端口, 没有推流或推流数据无效, 无法定位到手机号, 以至于无法执行PreparePublishSource函数), 将不再处理后续事情.
		log.Sugar.Errorf("删除源失败 source:%s err:%s", s.ID, err.Error())
		return
	}

	// 关闭所有输出流
	for _, transStream := range s.TransStreams {
		// 发送剩余包
		data, ts, _ := transStream.Close()
		if len(data) > 0 {
			s.DispatchBuffer(transStream, -1, data, ts, true)
		}
	}

	// 将所有sink添加到等待队列
	for _, sink := range s.sinks {
		transStreamID := sink.GetTransStreamID()
		sink.SetTransStreamID(0)
		if s.recordSink == sink {
			return
		}

		{
			sink.Lock()

			if SessionStateClosed == sink.GetState() {
				log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开连接 %s", sink.String())
			} else {
				sink.SetState(SessionStateWaiting)
				AddSinkToWaitingQueue(s.ID, sink)
			}

			sink.UnLock()
		}

		if SessionStateClosed != sink.GetState() {
			sink.StopStreaming(s.TransStreams[transStreamID])
		}
	}

	s.TransStreams = nil
	s.sinks = nil
	s.TransStreamSinks = nil

	// 异步hook
	go func() {
		if s.Conn != nil {
			s.Conn.Close()
			s.Conn = nil
		}

		HookPublishDoneEvent(s)

		if s.recordSink != nil {
			HookRecordEvent(s, s.recordFilePath)
		}
	}()
}

func (s *PublishSource) Close() {
	if s.closed.Load() {
		return
	}

	// 同步执行, 确保close后, 主协程已经退出, 不会再处理任何推拉流、查询等任何事情.
	group := sync.WaitGroup{}
	group.Add(1)

	s.PostEvent(func() {
		s.DoClose()

		group.Done()
	})

	group.Wait()
}

func (s *PublishSource) OnDiscardPacket(packet utils.AVPacket) {
	s.FindOrCreatePacketBuffer(packet.Index(), packet.MediaType()).FreeHead()
}

func (s *PublishSource) OnDeMuxStream(stream utils.AVStream) {
	if s.completed {
		log.Sugar.Warnf("添加track失败,已经WriteHeader. source: %s", s.ID)
		return
	} else if !s.NotTrackAdded(stream.Index()) {
		log.Sugar.Warnf("添加track失败,已经添加索引为%d的track. source: %s", stream.Index(), s.ID)
		return
	}

	s.originStreams.Add(stream)
	s.allStreams.Add(stream)

	// 启动track解析超时计时器
	if len(s.originStreams.All()) == 1 {
		s.probeTimer = time.AfterFunc(time.Duration(AppConfig.ProbeTimeout)*time.Millisecond, func() {
			s.PostEvent(func() {
				s.writeHeader()
			})
		})
	}

	if utils.AVMediaTypeVideo == stream.Type() {
		s.existVideo = true
	}

	// 创建GOPBuffer
	if AppConfig.GOPCache && s.existVideo && s.gopBuffer == nil {
		s.gopBuffer = NewStreamBuffer()
		// 设置GOP缓存溢出回调
		s.gopBuffer.SetDiscardHandler(s.OnDiscardPacket)
	}
}

// 解析完所有track后, 创建各种输出流
func (s *PublishSource) writeHeader() {
	if s.completed {
		fmt.Printf("添加Stream失败 Source: %s已经WriteHeader", s.ID)
		return
	}

	s.completed = true
	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}

	if len(s.originStreams.All()) == 0 {
		log.Sugar.Errorf("没有一路track, 删除source: %s", s.ID)
		s.DoClose()
		return
	}

	// 创建录制流和HLS
	s.CreateDefaultOutStreams()

	// 将等待队列的sink添加到输出流队列
	sinks := PopWaitingSinks(s.ID)
	if s.recordSink != nil {
		sinks = append(sinks, s.recordSink)
	}

	for _, sink := range sinks {
		if !s.doAddSink(sink) {
			sink.Close()
		}
	}
}

func (s *PublishSource) IsCompleted() bool {
	return s.completed
}

// NotTrackAdded 是否没有添加该index对应的track
func (s *PublishSource) NotTrackAdded(index int) bool {
	for _, avStream := range s.originStreams.All() {
		if avStream.Index() == index {
			return false
		}
	}

	return true
}

func (s *PublishSource) OnDeMuxStreamDone() {
	s.writeHeader()
}

func (s *PublishSource) OnDeMuxPacket(packet utils.AVPacket) {
	// track超时，忽略推流数据
	if s.NotTrackAdded(packet.Index()) {
		s.FindOrCreatePacketBuffer(packet.Index(), packet.MediaType()).FreeTail()
		return
	}

	if AppConfig.GOPCache && s.existVideo {
		s.gopBuffer.AddPacket(packet)
	}

	// 分发给各个传输流
	for _, transStream := range s.TransStreams {
		s.DispatchPacket(transStream, packet)
	}

	// 未开启GOP缓存或只存在音频流, 释放掉内存
	if !AppConfig.GOPCache || !s.existVideo {
		s.FindOrCreatePacketBuffer(packet.Index(), packet.MediaType()).FreeTail()
	}
}

func (s *PublishSource) OnDeMuxDone() {

}

func (s *PublishSource) GetType() SourceType {
	return s.Type
}

func (s *PublishSource) SetType(sourceType SourceType) {
	s.Type = sourceType
}

func (s *PublishSource) RemoteAddr() string {
	if s.Conn == nil {
		return ""
	}

	return s.Conn.RemoteAddr().String()
}

func (s *PublishSource) String() string {
	return fmt.Sprintf("source: %s type: %s conn: %s ", s.ID, s.Type.String(), s.RemoteAddr())
}

func (s *PublishSource) State() SessionState {
	return s.state
}

func (s *PublishSource) UrlValues() url.Values {
	return s.urlValues
}

func (s *PublishSource) SetUrlValues(values url.Values) {
	s.urlValues = values
}

func (s *PublishSource) PostEvent(cb func()) {
	s.mainContextEvents <- cb
}

func (s *PublishSource) CreateTime() time.Time {
	return s.createTime
}

func (s *PublishSource) SetCreateTime(time time.Time) {
	s.createTime = time
}

func (s *PublishSource) Sinks() []Sink {
	var sinks []Sink

	group := sync.WaitGroup{}
	group.Add(1)
	s.PostEvent(func() {
		for _, sink := range s.sinks {
			sinks = append(sinks, sink)
		}

		group.Done()
	})

	group.Wait()
	return sinks
}

func (s *PublishSource) GetBitrateStatistics() *BitrateStatistics {
	return s.statistics
}
